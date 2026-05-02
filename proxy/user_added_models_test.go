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

// testEditableConfigWithDB returns a config with a deterministic capture DB
// path so tests can re-open the same SQLite file and assert that user-added
// models survive a "restart".
func testEditableConfigWithDB(t *testing.T, dbPath string) config.Config {
	t.Helper()
	cfg, err := config.LoadConfigFromReader(strings.NewReader(`
healthCheckTimeout: 15
logLevel: error
captureDBPath: ignored.sqlite
startPort: 19000
models:
  model1:
    cmd: /app/llama-server-progress -hf org/model:Q4_K_M --port ${PORT} --alias ${MODEL_ID} -ngl 99 -c 4096
`))
	require.NoError(t, err)
	cfg.CaptureDBPath = dbPath
	return cfg
}

// TestProxyManager_DuplicateModel verifies the duplicate happy path:
// auto-generated model id, the new entry is editable, and the rebuilt cmd
// matches the source.
func TestProxyManager_DuplicateModel(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "session.sqlite")
	cfg := testEditableConfigWithDB(t, dbPath)
	proxy := New(cfg)
	defer proxy.Shutdown()

	created, err := proxy.duplicateModel("model1")
	require.NoError(t, err)
	assert.Equal(t, "model1-copy", created.ModelID)
	assert.Equal(t, "model1", created.SourceModelID)
	assert.True(t, created.UserAdded)
	assert.True(t, created.Editable)

	// The base cmd uses `--alias ${MODEL_ID}` (resolved to "model1"), so the
	// duplicate's alias should follow the new id and not stay pinned to the
	// source name.
	assert.Equal(t, "model1-copy", created.Effective.Alias)
	assert.Contains(t, created.Command, "--alias model1-copy")

	// New entry must be visible to the model list APIs.
	configs, err := proxy.listEditableModelConfigs()
	require.NoError(t, err)
	ids := make([]string, 0, len(configs))
	for _, cfg := range configs {
		ids = append(ids, cfg.ModelID)
	}
	assert.Contains(t, ids, "model1")
	assert.Contains(t, ids, "model1-copy")

	// Subsequent duplicate must pick a fresh id, not collide.
	second, err := proxy.duplicateModel("model1")
	require.NoError(t, err)
	assert.Equal(t, "model1-copy-2", second.ModelID)
}

// TestProxyManager_DuplicateModelPersistsAcrossRestart spins the manager down
// and back up against the same SQLite file to confirm duplicates are restored
// on the next process boot.
func TestProxyManager_DuplicateModelPersistsAcrossRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "session.sqlite")

	first := New(testEditableConfigWithDB(t, dbPath))
	created, err := first.duplicateModel("model1")
	require.NoError(t, err)
	require.Equal(t, "model1-copy", created.ModelID)
	first.Shutdown()

	second := New(testEditableConfigWithDB(t, dbPath))
	defer second.Shutdown()

	configs, err := second.listEditableModelConfigs()
	require.NoError(t, err)
	var found *EditableModelConfig
	for i := range configs {
		if configs[i].ModelID == "model1-copy" {
			found = &configs[i]
			break
		}
	}
	require.NotNil(t, found, "user-added model should survive restart")
	assert.True(t, found.UserAdded)
	assert.Equal(t, "model1", found.SourceModelID)

	// And it must be a member of a process group.
	pg := second.findGroupByModelName("model1-copy")
	require.NotNil(t, pg)
	assert.NotNil(t, pg.processes["model1-copy"])
}

// TestProxyManager_DeleteUserAddedModel verifies the delete path tears down
// the entry, removes it from the in-memory config, and refuses to delete a
// YAML-defined model.
func TestProxyManager_DeleteUserAddedModel(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "session.sqlite")
	cfg := testEditableConfigWithDB(t, dbPath)
	proxy := New(cfg)
	defer proxy.Shutdown()

	created, err := proxy.duplicateModel("model1")
	require.NoError(t, err)

	require.NoError(t, proxy.deleteModelEntry(created.ModelID))

	_, ok := proxy.config.Models[created.ModelID]
	assert.False(t, ok, "deleted model must not remain in pm.config.Models")
	assert.Nil(t, proxy.findGroupByModelName(created.ModelID))

	// YAML-defined models must not be deletable through this path.
	err = proxy.deleteModelEntry("model1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not user-added")
}

// TestProxyManager_DuplicateModelHTTPRoute exercises the API layer end-to-end
// via the registered Gin handler so any routing or status-code regressions
// surface quickly.
func TestProxyManager_DuplicateModelHTTPRoute(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "session.sqlite")
	cfg := testEditableConfigWithDB(t, dbPath)
	proxy := New(cfg)
	defer proxy.Shutdown()

	req := httptest.NewRequest("POST", "/api/config/models/model1/duplicate", bytes.NewReader(nil))
	w := CreateTestResponseRecorder()
	proxy.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var response EditableModelConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Equal(t, "model1-copy", response.ModelID)
	assert.True(t, response.UserAdded)

	delReq := httptest.NewRequest("DELETE", "/api/config/models/model1-copy", nil)
	delW := CreateTestResponseRecorder()
	proxy.ServeHTTP(delW, delReq)

	require.Equal(t, http.StatusOK, delW.Code, "body: %s", delW.Body.String())
}
