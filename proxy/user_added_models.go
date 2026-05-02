package proxy

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mostlygeek/llama-swap/event"
	"github.com/mostlygeek/llama-swap/proxy/config"
	"gopkg.in/yaml.v3"
)

// userAddedModelRecord is the persisted representation of a model that was
// created at runtime via the Duplicate UI. The full ModelConfig is stored as
// YAML so we don't have to track every individual field, and so any future
// fields added to ModelConfig round-trip without a migration.
type userAddedModelRecord struct {
	ModelID       string
	SourceModelID string
	GroupID       string
	Config        config.ModelConfig
}

// listUserAddedModels returns every persisted user-added model. Used at
// startup to merge runtime-added models into the in-memory config.
func (s *sessionModelSettingsStore) listUserAddedModels() ([]userAddedModelRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT model_id, source_model_id, group_id, config_yaml
		FROM user_added_models
		ORDER BY model_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []userAddedModelRecord
	for rows.Next() {
		var (
			modelID, sourceID, groupID, configYAML string
		)
		if err := rows.Scan(&modelID, &sourceID, &groupID, &configYAML); err != nil {
			return nil, err
		}
		var modelConfig config.ModelConfig
		if err := yaml.Unmarshal([]byte(configYAML), &modelConfig); err != nil {
			return nil, fmt.Errorf("user-added model %q: %w", modelID, err)
		}
		records = append(records, userAddedModelRecord{
			ModelID:       modelID,
			SourceModelID: sourceID,
			GroupID:       groupID,
			Config:        modelConfig,
		})
	}
	return records, rows.Err()
}

// getUserAddedModel returns the record for a single user-added model, or
// (_, false, nil) when the model id is not user-added.
func (s *sessionModelSettingsStore) getUserAddedModel(modelID string) (userAddedModelRecord, bool, error) {
	if s == nil || s.db == nil {
		return userAddedModelRecord{}, false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	var record userAddedModelRecord
	var configYAML string
	err := s.db.QueryRow(`
		SELECT model_id, source_model_id, group_id, config_yaml
		FROM user_added_models
		WHERE model_id = ?
	`, modelID).Scan(&record.ModelID, &record.SourceModelID, &record.GroupID, &configYAML)
	if errors.Is(err, sql.ErrNoRows) {
		return userAddedModelRecord{}, false, nil
	}
	if err != nil {
		return userAddedModelRecord{}, false, err
	}
	if err := yaml.Unmarshal([]byte(configYAML), &record.Config); err != nil {
		return userAddedModelRecord{}, false, fmt.Errorf("user-added model %q: %w", modelID, err)
	}
	return record, true, nil
}

// saveUserAddedModel inserts or updates a runtime-created model.
func (s *sessionModelSettingsStore) saveUserAddedModel(record userAddedModelRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("user-added model storage unavailable")
	}
	configYAML, err := yaml.Marshal(record.Config)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.db.Exec(`
		INSERT INTO user_added_models
			(model_id, source_model_id, group_id, config_yaml, created_at)
		VALUES (?, ?, ?, ?, unixepoch())
		ON CONFLICT(model_id) DO UPDATE SET
			source_model_id = excluded.source_model_id,
			group_id = excluded.group_id,
			config_yaml = excluded.config_yaml
	`, record.ModelID, record.SourceModelID, record.GroupID, string(configYAML))
	return err
}

// deleteUserAddedModel removes the user-added entry for `modelID`. It is a
// no-op when the row does not exist.
func (s *sessionModelSettingsStore) deleteUserAddedModel(modelID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM user_added_models WHERE model_id = ?`, modelID)
	return err
}

// mergeUserAddedModels folds every persisted user-added model into the given
// Config. Each row becomes a regular Models entry and is added as a member of
// its recorded group (or the default group when the recorded group no longer
// exists). Missing CaptureDB or migration state simply produces a no-op.
//
// Called from New() before the process groups are constructed so user-added
// models become first-class members of their group with normal lifecycle.
func mergeUserAddedModels(cfg *config.Config, store *sessionModelSettingsStore, logger *LogMonitor) {
	if store == nil {
		return
	}
	records, err := store.listUserAddedModels()
	if err != nil {
		if logger != nil {
			logger.Warnf("failed to load user-added models: %v", err)
		}
		return
	}
	for _, record := range records {
		// Skip if the YAML config already defines this model id (collision
		// with a freshly-edited config); the YAML wins.
		if _, exists := cfg.Models[record.ModelID]; exists {
			continue
		}

		groupID := record.GroupID
		if _, ok := cfg.Groups[groupID]; !ok {
			groupID = config.DEFAULT_GROUP_ID
		}

		cfg.Models[record.ModelID] = record.Config
		group := cfg.Groups[groupID]
		if !slices.Contains(group.Members, record.ModelID) {
			group.Members = append(group.Members, record.ModelID)
			cfg.Groups[groupID] = group
		}
	}
}

// duplicateModel creates a copy of an existing model under a freshly-generated
// model id, persists it via the user_added_models table, and inserts it into
// the appropriate process group so it is immediately usable.
func (pm *ProxyManager) duplicateModel(sourceModelID string) (EditableModelConfig, error) {
	pm.Lock()
	defer pm.Unlock()

	if pm.sessionModelSettings == nil {
		return EditableModelConfig{}, fmt.Errorf("user-added model storage unavailable")
	}

	sourceCfg, ok := pm.config.Models[sourceModelID]
	if !ok {
		return EditableModelConfig{}, fmt.Errorf("source model %q not found", sourceModelID)
	}

	groupID := pm.findGroupIDForModel(sourceModelID)
	if groupID == "" {
		groupID = config.DEFAULT_GROUP_ID
	}
	if _, ok := pm.config.Groups[groupID]; !ok {
		groupID = config.DEFAULT_GROUP_ID
	}

	newID := generateUniqueModelID(sourceModelID, pm.config.Models)

	// ModelConfig is a value type; struct copy is sufficient for the fields
	// that matter at runtime (cmd, proxy, env). Slice/map fields are read-only
	// after this point.
	newCfg := sourceCfg

	// When the source's alias matches its model id (the common
	// `--alias ${MODEL_ID}` pattern), retarget the duplicate's alias to the
	// new id so the dialog title and the alias stay in sync. Custom aliases
	// are preserved verbatim. Failure here is non-fatal: the duplicate just
	// keeps the source's resolved cmd as-is.
	if base, meta, err := extractSessionModelSettings(sourceCfg); err == nil && base.Alias == sourceModelID {
		retargeted := base
		retargeted.Alias = newID
		newCfg.Cmd = buildSessionModelCommand(meta, retargeted)
	}

	if err := pm.sessionModelSettings.saveUserAddedModel(userAddedModelRecord{
		ModelID:       newID,
		SourceModelID: sourceModelID,
		GroupID:       groupID,
		Config:        newCfg,
	}); err != nil {
		return EditableModelConfig{}, err
	}

	pm.config.Models[newID] = newCfg
	group := pm.config.Groups[groupID]
	if !slices.Contains(group.Members, newID) {
		group.Members = append(group.Members, newID)
		pm.config.Groups[groupID] = group
	}

	if pg, ok := pm.processGroups[groupID]; ok && pg != nil {
		pg.AddMember(newID, newCfg)
	}

	event.Emit(ConfigFileChangedEvent{})

	return pm.editableModelConfig(newID)
}

// deleteModelEntry deletes a user-added model entirely: stops any running
// process, removes it from the in-memory config and process group, and
// drops both the user_added_models row and any session_model_settings
// override that may have been recorded against the same id.
//
// Returns an error when the model id is not user-added so callers cannot
// accidentally delete YAML-defined entries.
func (pm *ProxyManager) deleteModelEntry(modelID string) error {
	pm.Lock()
	defer pm.Unlock()

	if pm.sessionModelSettings == nil {
		return fmt.Errorf("user-added model storage unavailable")
	}

	_, found, err := pm.sessionModelSettings.getUserAddedModel(modelID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("model %q is not user-added; cannot delete", modelID)
	}

	if pg := pm.findGroupByModelName(modelID); pg != nil {
		pg.RemoveMember(modelID)
	}

	delete(pm.config.Models, modelID)
	for gid, group := range pm.config.Groups {
		if !slices.Contains(group.Members, modelID) {
			continue
		}
		group.Members = slices.DeleteFunc(group.Members, func(m string) bool { return m == modelID })
		pm.config.Groups[gid] = group
	}

	if err := pm.sessionModelSettings.deleteUserAddedModel(modelID); err != nil {
		return err
	}
	if err := pm.sessionModelSettings.delete(modelID); err != nil {
		return err
	}

	event.Emit(ConfigFileChangedEvent{})
	return nil
}

// findGroupIDForModel returns the id of the group that contains modelID, or
// "" when no group lists it.
func (pm *ProxyManager) findGroupIDForModel(modelID string) string {
	for groupID, group := range pm.config.Groups {
		if slices.Contains(group.Members, modelID) {
			return groupID
		}
	}
	return ""
}

// generateUniqueModelID picks a fresh model id of the form `<source>-copy`,
// `<source>-copy-2`, … so duplicates don't collide with each other or any
// existing entry. Callers are expected to hold the proxy lock so the
// existence check stays consistent with the subsequent insert.
func generateUniqueModelID(source string, existing map[string]config.ModelConfig) string {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "model"
	}
	base := source + "-copy"
	if _, taken := existing[base]; !taken {
		return base
	}
	for i := 2; i < 10000; i++ {
		candidate := fmt.Sprintf("%s-copy-%d", source, i)
		if _, taken := existing[candidate]; !taken {
			return candidate
		}
	}
	// Extremely unlikely fallback so we never return a colliding id.
	return fmt.Sprintf("%s-copy-%d", source, len(existing)+1)
}
