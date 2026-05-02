package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mostlygeek/llama-swap/proxy/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testEditableConfig(t *testing.T) config.Config {
	t.Helper()

	cfg, err := config.LoadConfigFromReader(strings.NewReader(`
healthCheckTimeout: 15
logLevel: error
captureDBPath: ignored.sqlite
startPort: 19000
models:
  model1:
    cmd: /app/llama-server-progress -hf org/model:Q4_K_M --port ${PORT} --alias ${MODEL_ID} -ngl 99 -c 4096 --cache-type-k q4_0 --cache-type-v q4_0 --temp 0.7 --top-p 0.8 --grammar-file /app/think.gbnf
`))
	require.NoError(t, err)
	cfg.CaptureDBPath = filepath.Join(t.TempDir(), "session.sqlite")
	return cfg
}

func TestProxyManager_SessionModelSettingsExtractsEditableCommandParts(t *testing.T) {
	cfg := testEditableConfig(t)
	settings, meta, err := extractSessionModelSettings(cfg.Models["model1"])
	require.NoError(t, err)

	assert.Equal(t, "org/model:Q4_K_M", settings.Source)
	assert.Equal(t, "model1", settings.Alias)
	assert.Equal(t, "-ngl 99 -c 4096", settings.ServerArgs)
	assert.Equal(t, "--cache-type-k q4_0 --cache-type-v q4_0", settings.KVCacheArgs)
	assert.Equal(t, "--temp 0.7 --top-p 0.8", settings.SamplingArgs)
	assert.Equal(t, "--grammar-file /app/think.gbnf", settings.GrammarArgs)
	assert.Equal(t, "--port", meta.PortFlag)
	assert.Equal(t, "19000", meta.PortValue)
	assert.Equal(t, "--alias", meta.AliasFlag)
	assert.Equal(t, "model1", meta.AliasValue)
}

// TestProxyManager_SessionModelSettingsAlias verifies that the alias is
// surfaced as an editable field, that overrides round-trip through the rebuilt
// cmd, and that an empty alias falls back to the base value.
func TestProxyManager_SessionModelSettingsAlias(t *testing.T) {
	cfg := testEditableConfig(t)
	proxy := New(cfg)
	defer proxy.Shutdown()

	updated, err := proxy.saveSessionModelSettings("model1", SessionModelSettings{
		Alias:        "qwen-fast",
		Source:       "org/model:Q4_K_M",
		ServerArgs:   "-ngl 99 -c 4096",
		KVCacheArgs:  "--cache-type-k q4_0 --cache-type-v q4_0",
		SamplingArgs: "--temp 0.7 --top-p 0.8",
		GrammarArgs:  "--grammar-file /app/think.gbnf",
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Override)
	assert.Equal(t, "qwen-fast", updated.Effective.Alias)
	assert.Contains(t, updated.Command, "--alias qwen-fast")

	effective, ok := proxy.effectiveModelConfig("model1")
	require.True(t, ok)
	assert.Contains(t, effective.Cmd, "--alias qwen-fast")

	// An empty alias on save should resolve back to the base value (here
	// equal to the model id).
	cleared, err := proxy.saveSessionModelSettings("model1", SessionModelSettings{
		Alias:        "",
		Source:       "org/model:Q4_K_M",
		ServerArgs:   "-ngl 99 -c 4096",
		KVCacheArgs:  "--cache-type-k q4_0 --cache-type-v q4_0",
		SamplingArgs: "--temp 0.7 --top-p 0.8",
		GrammarArgs:  "--grammar-file /app/think.gbnf",
	})
	require.NoError(t, err)
	assert.Equal(t, "model1", cleared.Effective.Alias)
	assert.Contains(t, cleared.Command, "--alias model1")
}

func TestProxyManager_SessionModelSettingsAPI(t *testing.T) {
	cfg := testEditableConfig(t)
	proxy := New(cfg)
	defer proxy.Shutdown()

	body := bytes.NewBufferString(`{
		"source":"org/other:Q8_0",
		"serverArgs":"-ngl 80 -c 8192",
		"kvCacheArgs":"--cache-type-k q8_0 --cache-type-v q8_0",
		"samplingArgs":"--temp 0.6 --top-p 0.95"
	}`)
	req := httptest.NewRequest("PUT", "/api/config/models/model1/settings", body)
	req.Header.Set("Content-Type", "application/json")
	w := CreateTestResponseRecorder()
	proxy.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response EditableModelConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Equal(t, "model1", response.ModelID)
	assert.Equal(t, "org/other:Q8_0", response.Effective.Source)
	assert.NotNil(t, response.Override)

	effective, ok := proxy.effectiveModelConfig("model1")
	require.True(t, ok)
	assert.Contains(t, effective.Cmd, "-hf org/other:Q8_0")
	assert.Contains(t, effective.Cmd, "-ngl 80 -c 8192")
	assert.Contains(t, effective.Cmd, "--cache-type-k q8_0 --cache-type-v q8_0")
	assert.Contains(t, effective.Cmd, "--temp 0.6 --top-p 0.95")

	req = httptest.NewRequest("DELETE", "/api/config/models/model1/settings", nil)
	w = CreateTestResponseRecorder()
	proxy.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	response = EditableModelConfig{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Nil(t, response.Override)
	assert.Equal(t, "org/model:Q4_K_M", response.Effective.Source)
}

func TestProxyManager_SessionModelSettingsExportImport(t *testing.T) {
	cfg := testEditableConfig(t)
	proxy := New(cfg)
	defer proxy.Shutdown()

	_, err := proxy.saveSessionModelSettings("model1", SessionModelSettings{
		Source:       "org/exported:Q4_K_M",
		ServerArgs:   "-ngl 50 -c 16384",
		KVCacheArgs:  "--cache-type-k q4_0 --cache-type-v q4_0",
		SamplingArgs: "--temp 0.4 --top-p 0.9",
	})
	require.NoError(t, err)

	data, err := proxy.exportSessionConfigYAML()
	require.NoError(t, err)
	assert.Contains(t, string(data), "org/exported:Q4_K_M")

	nextCfg := testEditableConfig(t)
	nextProxy := New(nextCfg)
	defer nextProxy.Shutdown()

	result, err := nextProxy.importSessionConfigYAML(data)
	require.NoError(t, err)
	assert.Equal(t, []string{"model1"}, result.Imported)

	effective, ok := nextProxy.effectiveModelConfig("model1")
	require.True(t, ok)
	assert.Contains(t, effective.Cmd, "org/exported:Q4_K_M")
	assert.Contains(t, effective.Cmd, "-ngl 50 -c 16384")
}

// TestProxyManager_SessionModelSettingsGrammarArgs verifies that grammar flags
// are routed to the GrammarArgs bucket on extraction and re-emitted in the
// rebuilt cmd when applied.
func TestProxyManager_SessionModelSettingsGrammarArgs(t *testing.T) {
	cfg := testEditableConfig(t)
	proxy := New(cfg)
	defer proxy.Shutdown()

	updated, err := proxy.saveSessionModelSettings("model1", SessionModelSettings{
		Source:       "org/grammar:Q4_K_M",
		ServerArgs:   "-ngl 99 -c 4096",
		KVCacheArgs:  "--cache-type-k q4_0 --cache-type-v q4_0",
		SamplingArgs: "--temp 0.6 --top-p 0.95",
		GrammarArgs:  "--grammar-file /app/think.gbnf",
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Override)
	assert.Equal(t, "--grammar-file /app/think.gbnf", updated.Effective.GrammarArgs)
	assert.Contains(t, updated.Command, "--grammar-file /app/think.gbnf")

	effective, ok := proxy.effectiveModelConfig("model1")
	require.True(t, ok)
	assert.Contains(t, effective.Cmd, "--grammar-file /app/think.gbnf")

	// Clearing the field on save should drop the flag from the rebuilt cmd.
	cleared, err := proxy.saveSessionModelSettings("model1", SessionModelSettings{
		Source:       "org/grammar:Q4_K_M",
		ServerArgs:   "-ngl 99 -c 4096",
		KVCacheArgs:  "--cache-type-k q4_0 --cache-type-v q4_0",
		SamplingArgs: "--temp 0.6 --top-p 0.95",
		GrammarArgs:  "",
	})
	require.NoError(t, err)
	assert.Equal(t, "", cleared.Effective.GrammarArgs)
	assert.NotContains(t, cleared.Command, "--grammar-file")
}

// TestProxyManager_SessionConfigExportPreservesSource verifies that the export
// round-trips through the original YAML bytes: every model in the document is
// included, macros and comments are preserved, and only the cmd of an
// overridden model is rewritten.
func TestProxyManager_SessionConfigExportPreservesSource(t *testing.T) {
	source := `
healthCheckTimeout: 15
logLevel: error
captureDBPath: ignored.sqlite
startPort: 19000

# global macros shared across models
macros:
  LLAMA_SERVER: /app/llama-server-progress
  SERVER_ARGS: -ngl 99 -c 4096
  KV_CACHE: --cache-type-k q4_0 --cache-type-v q4_0
  SAMPLING: --temp 0.7 --top-p 0.8

models:
  model1:
    cmd: >
      ${LLAMA_SERVER}
      -hf org/model1:Q4_K_M
      --port ${PORT}
      --alias ${MODEL_ID}
      ${SERVER_ARGS}
      ${KV_CACHE}
      ${SAMPLING}
  model2:
    cmd: >
      ${LLAMA_SERVER}
      -hf org/model2:Q4_K_M
      --port ${PORT}
      --alias ${MODEL_ID}
      ${SERVER_ARGS}
      ${KV_CACHE}
      ${SAMPLING}
`
	cfg, err := config.LoadConfigFromReader(strings.NewReader(source))
	require.NoError(t, err)
	cfg.CaptureDBPath = filepath.Join(t.TempDir(), "session.sqlite")

	proxy := New(cfg)
	defer proxy.Shutdown()

	_, err = proxy.saveSessionModelSettings("model1", SessionModelSettings{
		Source:       "org/model1-override:Q8_0",
		ServerArgs:   "-ngl 80 -c 8192",
		KVCacheArgs:  "--cache-type-k q8_0 --cache-type-v q8_0",
		SamplingArgs: "--temp 0.4 --top-p 0.95",
	})
	require.NoError(t, err)

	data, err := proxy.exportSessionConfigYAML()
	require.NoError(t, err)
	out := string(data)

	// Macros block from the original document is preserved verbatim.
	assert.Contains(t, out, "LLAMA_SERVER: /app/llama-server-progress")
	assert.Contains(t, out, "SERVER_ARGS:")
	assert.Contains(t, out, "KV_CACHE:")

	// Both models are present in the export.
	assert.Contains(t, out, "model1:")
	assert.Contains(t, out, "model2:")

	// model1's cmd is rewritten with the override values.
	assert.Contains(t, out, "org/model1-override:Q8_0")
	assert.Contains(t, out, "-ngl 80 -c 8192")

	// model2 has no override, so its cmd retains the original macros.
	assert.Contains(t, out, "${LLAMA_SERVER}")
	assert.Contains(t, out, "${SERVER_ARGS}")
	assert.Contains(t, out, "org/model2:Q4_K_M")
}
