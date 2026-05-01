package proxy

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/mostlygeek/llama-swap/proxy/config"
)

type SessionModelSettings struct {
	Source       string `json:"source" yaml:"source"`
	ServerArgs   string `json:"serverArgs" yaml:"serverArgs"`
	KVCacheArgs  string `json:"kvCacheArgs" yaml:"kvCacheArgs"`
	SamplingArgs string `json:"samplingArgs" yaml:"samplingArgs"`
	// GrammarArgs holds llama.cpp grammar-related flags such as
	// --grammar-file, --grammar, --json-schema, and --json-schema-file.
	// Kept in its own bucket so the UI can edit grammar configuration
	// without colliding with normal sampling arguments.
	GrammarArgs string `json:"grammarArgs" yaml:"grammarArgs"`
}

type EditableModelConfig struct {
	ModelID   string                `json:"modelId"`
	State     string                `json:"state"`
	Base      SessionModelSettings  `json:"base"`
	Override  *SessionModelSettings `json:"override,omitempty"`
	Effective SessionModelSettings  `json:"effective"`
	Editable  bool                  `json:"editable"`
	Message   string                `json:"message,omitempty"`
	Command   string                `json:"command"`
}

type configImportResult struct {
	Imported []string `json:"imported"`
	Skipped  []string `json:"skipped"`
}

type sessionModelSettingsStore struct {
	mu sync.RWMutex
	db *sql.DB
}

func newSessionModelSettingsStore(dbPath string) (*sessionModelSettingsStore, error) {
	if strings.TrimSpace(dbPath) == "" {
		dbPath = ":memory:"
	}
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}
	if dbPath != ":memory:" {
		if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
			db.Close()
			return nil, err
		}
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS session_model_settings (
			model_id TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			server_args TEXT NOT NULL,
			kv_cache_args TEXT NOT NULL,
			sampling_args TEXT NOT NULL,
			grammar_args TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL DEFAULT (unixepoch())
		)
	`); err != nil {
		db.Close()
		return nil, err
	}

	// Migrate older databases that pre-date the grammar_args column. SQLite
	// has no native "ADD COLUMN IF NOT EXISTS"; we rely on the duplicate-column
	// error to short-circuit on already-migrated databases.
	if _, err := db.Exec(`ALTER TABLE session_model_settings ADD COLUMN grammar_args TEXT NOT NULL DEFAULT ''`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column name") {
		db.Close()
		return nil, err
	}

	return &sessionModelSettingsStore{db: db}, nil
}

func (s *sessionModelSettingsStore) close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *sessionModelSettingsStore) get(modelID string) (SessionModelSettings, bool, error) {
	if s == nil || s.db == nil {
		return SessionModelSettings{}, false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	var settings SessionModelSettings
	err := s.db.QueryRow(`
		SELECT source, server_args, kv_cache_args, sampling_args, grammar_args
		FROM session_model_settings
		WHERE model_id = ?
	`, modelID).Scan(&settings.Source, &settings.ServerArgs, &settings.KVCacheArgs, &settings.SamplingArgs, &settings.GrammarArgs)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionModelSettings{}, false, nil
	}
	if err != nil {
		return SessionModelSettings{}, false, err
	}
	return settings, true, nil
}

func (s *sessionModelSettingsStore) save(modelID string, settings SessionModelSettings) error {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO session_model_settings
			(model_id, source, server_args, kv_cache_args, sampling_args, grammar_args, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, unixepoch())
		ON CONFLICT(model_id) DO UPDATE SET
			source = excluded.source,
			server_args = excluded.server_args,
			kv_cache_args = excluded.kv_cache_args,
			sampling_args = excluded.sampling_args,
			grammar_args = excluded.grammar_args,
			updated_at = unixepoch()
	`, modelID, settings.Source, settings.ServerArgs, settings.KVCacheArgs, settings.SamplingArgs, settings.GrammarArgs)
	return err
}

func (s *sessionModelSettingsStore) delete(modelID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM session_model_settings WHERE model_id = ?`, modelID)
	return err
}

func (pm *ProxyManager) listEditableModelConfigs() ([]EditableModelConfig, error) {
	modelIDs := make([]string, 0, len(pm.config.Models))
	for modelID := range pm.config.Models {
		modelIDs = append(modelIDs, modelID)
	}
	sort.Strings(modelIDs)

	configs := make([]EditableModelConfig, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		cfg, err := pm.editableModelConfig(modelID)
		if err != nil {
			return nil, err
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}

func (pm *ProxyManager) editableModelConfig(modelID string) (EditableModelConfig, error) {
	modelConfig, ok := pm.config.Models[modelID]
	if !ok {
		return EditableModelConfig{}, fmt.Errorf("model not found")
	}

	base, meta, err := extractSessionModelSettings(modelConfig)
	editable := err == nil
	message := ""
	if err != nil {
		message = err.Error()
	}

	effective := base
	var overridePtr *SessionModelSettings
	if editable && pm.sessionModelSettings != nil {
		override, found, err := pm.sessionModelSettings.get(modelID)
		if err != nil {
			return EditableModelConfig{}, err
		}
		if found {
			overridePtr = &override
			effective = override
		}
	}

	command := ""
	if editable {
		command = buildSessionModelCommand(meta, effective)
	}

	state := "unknown"
	if processGroup := pm.findGroupByModelName(modelID); processGroup != nil {
		if process := processGroup.processes[modelID]; process != nil {
			state = string(process.CurrentState())
		}
	}

	return EditableModelConfig{
		ModelID:   modelID,
		State:     state,
		Base:      base,
		Override:  overridePtr,
		Effective: effective,
		Editable:  editable,
		Message:   message,
		Command:   command,
	}, nil
}

func (pm *ProxyManager) effectiveModelConfig(modelID string) (config.ModelConfig, bool) {
	modelConfig, ok := pm.config.Models[modelID]
	if !ok {
		return config.ModelConfig{}, false
	}

	if pm.sessionModelSettings == nil {
		return modelConfig, true
	}
	override, found, err := pm.sessionModelSettings.get(modelID)
	if err != nil {
		pm.proxyLogger.Warnf("failed to load session model settings for %s: %v", modelID, err)
		return modelConfig, true
	}
	if !found {
		return modelConfig, true
	}

	nextConfig, err := applySessionModelSettings(modelConfig, override)
	if err != nil {
		pm.proxyLogger.Warnf("failed to apply session model settings for %s: %v", modelID, err)
		return modelConfig, true
	}
	return nextConfig, true
}

func (pm *ProxyManager) saveSessionModelSettings(modelID string, settings SessionModelSettings) (EditableModelConfig, error) {
	if _, ok := pm.config.Models[modelID]; !ok {
		return EditableModelConfig{}, fmt.Errorf("model not found")
	}
	modelConfig := pm.config.Models[modelID]
	if _, _, err := extractSessionModelSettings(modelConfig); err != nil {
		return EditableModelConfig{}, err
	}
	if _, err := applySessionModelSettings(modelConfig, settings); err != nil {
		return EditableModelConfig{}, err
	}
	if err := pm.sessionModelSettings.save(modelID, normalizeSessionModelSettings(settings)); err != nil {
		return EditableModelConfig{}, err
	}
	pm.refreshStoppedProcessConfig(modelID)
	return pm.editableModelConfig(modelID)
}

func (pm *ProxyManager) resetSessionModelSettings(modelID string) (EditableModelConfig, error) {
	if _, ok := pm.config.Models[modelID]; !ok {
		return EditableModelConfig{}, fmt.Errorf("model not found")
	}
	if err := pm.sessionModelSettings.delete(modelID); err != nil {
		return EditableModelConfig{}, err
	}
	pm.refreshStoppedProcessConfig(modelID)
	return pm.editableModelConfig(modelID)
}

func (pm *ProxyManager) refreshStoppedProcessConfig(modelID string) {
	processGroup := pm.findGroupByModelName(modelID)
	if processGroup == nil {
		return
	}
	if effective, ok := pm.effectiveModelConfig(modelID); ok {
		processGroup.UpdateProcessConfigIfStopped(modelID, effective)
	}
}

func (pm *ProxyManager) importSessionConfigYAML(data []byte) (configImportResult, error) {
	importedConfig, err := config.LoadConfigFromReader(strings.NewReader(string(data)))
	if err != nil {
		return configImportResult{}, err
	}

	result := configImportResult{}
	modelIDs := make([]string, 0, len(importedConfig.Models))
	for modelID := range importedConfig.Models {
		modelIDs = append(modelIDs, modelID)
	}
	sort.Strings(modelIDs)

	for _, modelID := range modelIDs {
		currentModelConfig, ok := pm.config.Models[modelID]
		if !ok {
			result.Skipped = append(result.Skipped, modelID)
			continue
		}
		if _, _, err := extractSessionModelSettings(currentModelConfig); err != nil {
			result.Skipped = append(result.Skipped, modelID)
			continue
		}
		settings, _, err := extractSessionModelSettings(importedConfig.Models[modelID])
		if err != nil {
			result.Skipped = append(result.Skipped, modelID)
			continue
		}
		if _, err := applySessionModelSettings(currentModelConfig, settings); err != nil {
			result.Skipped = append(result.Skipped, modelID)
			continue
		}
		if err := pm.sessionModelSettings.save(modelID, settings); err != nil {
			return result, err
		}
		pm.refreshStoppedProcessConfig(modelID)
		result.Imported = append(result.Imported, modelID)
	}

	return result, nil
}

type sessionCommandMeta struct {
	Executable string
	PortFlag   string
	PortValue  string
	AliasFlag  string
	AliasValue string
}

func normalizeSessionModelSettings(settings SessionModelSettings) SessionModelSettings {
	return SessionModelSettings{
		Source:       strings.TrimSpace(settings.Source),
		ServerArgs:   strings.TrimSpace(settings.ServerArgs),
		KVCacheArgs:  strings.TrimSpace(settings.KVCacheArgs),
		SamplingArgs: strings.TrimSpace(settings.SamplingArgs),
		GrammarArgs:  strings.TrimSpace(settings.GrammarArgs),
	}
}

func extractSessionModelSettings(modelConfig config.ModelConfig) (SessionModelSettings, sessionCommandMeta, error) {
	args, err := modelConfig.SanitizedCommand()
	if err != nil {
		return SessionModelSettings{}, sessionCommandMeta{}, err
	}
	if len(args) == 0 {
		return SessionModelSettings{}, sessionCommandMeta{}, fmt.Errorf("empty command")
	}

	meta := sessionCommandMeta{Executable: args[0]}
	settings := SessionModelSettings{}
	serverArgs := []string{}
	kvArgs := []string{}
	samplingArgs := []string{}
	grammarArgs := []string{}

	for i := 1; i < len(args); i++ {
		arg := args[i]
		flag, value, hasEqual := strings.Cut(arg, "=")

		switch {
		case isHFSourceFlag(arg):
			if i+1 >= len(args) {
				return SessionModelSettings{}, sessionCommandMeta{}, fmt.Errorf("%s is missing a value", arg)
			}
			settings.Source = args[i+1]
			i++
			continue
		case hasEqual && isHFSourceFlag(flag):
			settings.Source = value
			continue
		case isPortFlag(arg):
			if i+1 < len(args) {
				meta.PortFlag = arg
				meta.PortValue = args[i+1]
				i++
			}
			continue
		case hasEqual && isPortFlag(flag):
			meta.PortFlag = flag
			meta.PortValue = value
			continue
		case isAliasFlag(arg):
			if i+1 < len(args) {
				meta.AliasFlag = arg
				meta.AliasValue = args[i+1]
				i++
			}
			continue
		case hasEqual && isAliasFlag(flag):
			meta.AliasFlag = flag
			meta.AliasValue = value
			continue
		}

		target := &serverArgs
		switch {
		case isKVCacheFlag(flag):
			target = &kvArgs
		case isSamplingFlag(flag):
			target = &samplingArgs
		case isGrammarFlag(flag):
			target = &grammarArgs
		}
		*target = append(*target, arg)
		if !hasEqual && flagTakesValue(arg) && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			*target = append(*target, args[i+1])
			i++
		}
	}

	if strings.TrimSpace(settings.Source) == "" {
		return SessionModelSettings{}, meta, fmt.Errorf("command does not include an editable -hf source")
	}

	settings.ServerArgs = strings.Join(serverArgs, " ")
	settings.KVCacheArgs = strings.Join(kvArgs, " ")
	settings.SamplingArgs = strings.Join(samplingArgs, " ")
	settings.GrammarArgs = strings.Join(grammarArgs, " ")
	return normalizeSessionModelSettings(settings), meta, nil
}

func applySessionModelSettings(modelConfig config.ModelConfig, settings SessionModelSettings) (config.ModelConfig, error) {
	base, meta, err := extractSessionModelSettings(modelConfig)
	if err != nil {
		return config.ModelConfig{}, err
	}
	settings = normalizeSessionModelSettings(settings)
	if settings.Source == "" {
		settings.Source = base.Source
	}
	if strings.ContainsAny(settings.Source, " \t\r\n") {
		return config.ModelConfig{}, fmt.Errorf("source cannot contain whitespace")
	}

	nextConfig := modelConfig
	nextConfig.Cmd = buildSessionModelCommand(meta, settings)
	if _, err := nextConfig.SanitizedCommand(); err != nil {
		return config.ModelConfig{}, fmt.Errorf("invalid generated command: %w", err)
	}
	if _, err := url.Parse(nextConfig.Proxy); err != nil {
		return config.ModelConfig{}, fmt.Errorf("invalid proxy URL: %w", err)
	}
	return nextConfig, nil
}

func buildSessionModelCommand(meta sessionCommandMeta, settings SessionModelSettings) string {
	settings = normalizeSessionModelSettings(settings)
	args := []string{meta.Executable, "-hf", settings.Source}
	if meta.PortFlag != "" && meta.PortValue != "" {
		args = append(args, meta.PortFlag, meta.PortValue)
	}
	if meta.AliasFlag != "" && meta.AliasValue != "" {
		args = append(args, meta.AliasFlag, meta.AliasValue)
	}
	args = append(args, splitCommandSegment(settings.ServerArgs)...)
	args = append(args, splitCommandSegment(settings.KVCacheArgs)...)
	args = append(args, splitCommandSegment(settings.SamplingArgs)...)
	args = append(args, splitCommandSegment(settings.GrammarArgs)...)
	return joinCommandArgs(args)
}

func splitCommandSegment(segment string) []string {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return nil
	}
	args, err := config.SanitizeCommand("x " + segment)
	if err != nil || len(args) <= 1 {
		return strings.Fields(segment)
	}
	return args[1:]
}

func joinCommandArgs(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "" || strings.ContainsAny(arg, " \t\r\n\"'\\") {
			quoted = append(quoted, strconv.Quote(arg))
		} else {
			quoted = append(quoted, arg)
		}
	}
	return strings.Join(quoted, " ")
}

func isHFSourceFlag(flag string) bool {
	return flag == "-hf" || flag == "--hf"
}

func isPortFlag(flag string) bool {
	return flag == "--port" || flag == "-port" || flag == "-p"
}

func isAliasFlag(flag string) bool {
	return flag == "--alias" || flag == "-a"
}

func isKVCacheFlag(flag string) bool {
	return flag == "--cache-type-k" || flag == "--cache-type-v"
}

func isSamplingFlag(flag string) bool {
	switch flag {
	case "--top-k", "--top-p", "--min-p", "--temp", "--repeat-penalty", "--repeat_penalty", "--presence-penalty", "--presence_penalty":
		return true
	default:
		return false
	}
}

func isGrammarFlag(flag string) bool {
	switch flag {
	case "--grammar", "--grammar-file", "-j", "--json-schema", "--json-schema-file":
		return true
	default:
		return false
	}
}

func flagTakesValue(flag string) bool {
	return strings.HasPrefix(flag, "-")
}
