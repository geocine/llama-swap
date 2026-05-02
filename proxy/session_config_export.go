package proxy

import (
	"sort"

	"github.com/mostlygeek/llama-swap/proxy/config"
	"gopkg.in/yaml.v3"
)

// exportSessionConfigYAML emits a YAML document describing the running config
// with any session overrides folded back into the relevant model commands.
//
// When the proxy was loaded from a YAML source (the usual case) the exporter
// round-trips through the original bytes via yaml.Node so that macros,
// comments, key ordering and the overall document shape are preserved. Only
// the `cmd` field of models that have a session override is rewritten, which
// keeps the export visually close to the user's hand-written config.
//
// When the proxy was constructed programmatically (e.g. from tests) the
// original bytes are unavailable and the exporter falls back to marshaling
// the in-memory Config — which still contains every model, just rendered
// from the parsed structures.
func (pm *ProxyManager) exportSessionConfigYAML() ([]byte, error) {
	if data, ok, err := pm.exportSessionConfigFromSource(); err != nil {
		return nil, err
	} else if ok {
		return data, nil
	}
	return pm.exportSessionConfigFromMemory()
}

// exportSessionConfigFromSource attempts to produce the export by patching
// the original YAML document. It returns (nil, false, nil) when no source
// bytes are available so callers can fall back to the in-memory marshaler.
func (pm *ProxyManager) exportSessionConfigFromSource() ([]byte, bool, error) {
	raw := pm.config.RawSource()
	if len(raw) == 0 {
		return nil, false, nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		// Source bytes that fail to parse should not block export; let the
		// caller render a best-effort document from the in-memory config.
		return nil, false, nil
	}

	if err := pm.applySessionOverridesToDoc(&doc); err != nil {
		return nil, false, err
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// exportSessionConfigFromMemory marshals the in-memory Config with effective
// (override-applied) model configs. Used when the original source bytes are
// not available.
func (pm *ProxyManager) exportSessionConfigFromMemory() ([]byte, error) {
	exportConfig := pm.config

	modelIDs := make([]string, 0, len(pm.config.Models))
	for modelID := range pm.config.Models {
		modelIDs = append(modelIDs, modelID)
	}
	sort.Strings(modelIDs)

	exportConfig.Models = make(map[string]config.ModelConfig, len(modelIDs))
	for _, modelID := range modelIDs {
		base := pm.config.Models[modelID]
		if effective, ok := pm.effectiveModelConfig(modelID); ok {
			exportConfig.Models[modelID] = effective
		} else {
			exportConfig.Models[modelID] = base
		}
	}

	return yaml.Marshal(exportConfig)
}

// applySessionOverridesToDoc walks the parsed YAML document and rewrites the
// `cmd` field of every model that has a stored session override. Only that
// field is touched; the rest of the document — macros, comments, ordering,
// other keys — is left untouched.
func (pm *ProxyManager) applySessionOverridesToDoc(doc *yaml.Node) error {
	if pm.sessionModelSettings == nil {
		return nil
	}
	if doc == nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}

	modelsNode := mappingValue(root, "models")
	if modelsNode == nil || modelsNode.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(modelsNode.Content); i += 2 {
		keyNode := modelsNode.Content[i]
		valueNode := modelsNode.Content[i+1]
		if valueNode.Kind != yaml.MappingNode {
			continue
		}
		modelID := keyNode.Value
		baseCfg, ok := pm.config.Models[modelID]
		if !ok {
			continue
		}
		override, found, err := pm.sessionModelSettings.get(modelID)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		cmd, err := renderExportCmd(baseCfg, override)
		if err != nil {
			// A stored override may have become incompatible with a base
			// config that has since changed shape. Skip it instead of
			// failing the whole export.
			continue
		}
		setCmdField(valueNode, cmd)
	}
	return nil
}

// renderExportCmd builds the cmd string for an overridden model in a form
// suitable for writing back to a config document. The model's resolved port
// is restored to ${PORT}, and an unchanged alias is restored to ${MODEL_ID},
// so the exported YAML stays portable and re-importable across hosts. A
// custom alias is emitted verbatim so it round-trips correctly.
func renderExportCmd(baseCfg config.ModelConfig, override SessionModelSettings) (string, error) {
	base, meta, err := extractSessionModelSettings(baseCfg)
	if err != nil {
		return "", err
	}
	if meta.PortValue != "" {
		meta.PortValue = "${PORT}"
	}

	override = normalizeSessionModelSettings(override)
	if override.Alias == "" || override.Alias == base.Alias {
		// The override doesn't change the alias, so keep the placeholder.
		override.Alias = ""
		if meta.AliasValue != "" {
			meta.AliasValue = "${MODEL_ID}"
		}
	}
	return buildSessionModelCommand(meta, override), nil
}

// mappingValue returns the value node for `key` inside a mapping node, or nil
// when the key is missing or the parent is not a mapping.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// setCmdField updates (or appends) the `cmd` entry on a model mapping node.
// The value is written without an explicit style so yaml.v3 picks the most
// readable representation for the rendered command string.
func setCmdField(modelNode *yaml.Node, cmd string) {
	if modelNode == nil || modelNode.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(modelNode.Content); i += 2 {
		if modelNode.Content[i].Value == "cmd" {
			valueNode := modelNode.Content[i+1]
			valueNode.Kind = yaml.ScalarNode
			valueNode.Tag = ""
			valueNode.Style = 0
			valueNode.Value = cmd
			valueNode.Content = nil
			return
		}
	}
	modelNode.Content = append(modelNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "cmd"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: cmd},
	)
}
