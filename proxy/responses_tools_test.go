package proxy

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyManager_ResponsesTools_CustomToolsBecomeFunctions(t *testing.T) {
	body := []byte(`{
		"model": "qwen",
		"tools": [
			{
				"type": "custom",
				"name": "apply_patch",
				"description": "Use apply_patch",
				"format": {
					"type": "grammar",
					"syntax": "lark",
					"definition": "start: /.+/"
				}
			}
		],
		"tool_choice": {
			"type": "custom",
			"name": "apply_patch"
		}
	}`)

	normalized, changed, err := normalizeResponsesRequestForLlamaCpp(body)
	require.NoError(t, err)
	require.True(t, changed)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(normalized, &decoded))

	tools := decoded["tools"].([]any)
	require.Len(t, tools, 1)
	tool := tools[0].(map[string]any)
	assert.Equal(t, "function", tool["type"])
	assert.Equal(t, "apply_patch", tool["name"])
	assert.Equal(t, false, tool["strict"])

	parameters := tool["parameters"].(map[string]any)
	assert.Equal(t, "object", parameters["type"])
	assert.Equal(t, false, parameters["additionalProperties"])
	assert.Equal(t, []any{"input"}, parameters["required"])

	properties := parameters["properties"].(map[string]any)
	input := properties["input"].(map[string]any)
	assert.Equal(t, "string", input["type"])

	toolChoice := decoded["tool_choice"].(map[string]any)
	assert.Equal(t, "function", toolChoice["type"])
	assert.Equal(t, "apply_patch", toolChoice["name"])
}

func TestProxyManager_ResponsesTools_FunctionToolsPassThrough(t *testing.T) {
	body := []byte(`{
		"model": "qwen",
		"tools": [
			{
				"type": "function",
				"name": "shell_command",
				"description": "Run a command",
				"parameters": {
					"type": "object",
					"properties": {
						"command": {
							"type": "string"
						}
					}
				}
			}
		]
	}`)

	normalized, changed, err := normalizeResponsesRequestForLlamaCpp(body)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.JSONEq(t, string(body), string(normalized))
}

func TestProxyManager_ResponsesTools_UnsupportedToolsBecomeFunctions(t *testing.T) {
	body := []byte(`{
		"model": "qwen",
		"tools": [
			{
				"type": "local_shell"
			}
		]
	}`)

	normalized, changed, err := normalizeResponsesRequestForLlamaCpp(body)
	require.NoError(t, err)
	require.True(t, changed)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(normalized, &decoded))

	tools := decoded["tools"].([]any)
	require.Len(t, tools, 1)
	tool := tools[0].(map[string]any)
	assert.Equal(t, "function", tool["type"])
	assert.Equal(t, "local_shell", tool["name"])

	parameters := tool["parameters"].(map[string]any)
	assert.Equal(t, []any{"command"}, parameters["required"])
	properties := parameters["properties"].(map[string]any)
	command := properties["command"].(map[string]any)
	assert.Equal(t, "array", command["type"])
	items := command["items"].(map[string]any)
	assert.Equal(t, "string", items["type"])
}

func TestProxyManager_ResponsesTools_DropsUnsupportedBuiltIns(t *testing.T) {
	body := []byte(`{
		"model": "qwen",
		"tools": [
			{
				"type": "web_search"
			},
			{
				"type": "image_generation",
				"output_format": "png"
			}
		]
	}`)

	normalized, changed, err := normalizeResponsesRequestForLlamaCpp(body)
	require.NoError(t, err)
	require.True(t, changed)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(normalized, &decoded))

	tools := decoded["tools"].([]any)
	assert.Empty(t, tools)
}

func TestProxyManager_ResponsesTools_HoistsSystemAndDeveloperMessages(t *testing.T) {
	body := []byte(`{
		"model": "qwen",
		"instructions": "Base instructions.",
		"input": [
			{
				"type": "message",
				"role": "user",
				"content": [
					{
						"type": "input_text",
						"text": "Hello"
					}
				]
			},
			{
				"type": "message",
				"role": "developer",
				"content": [
					{
						"type": "input_text",
						"text": "Developer instructions."
					}
				]
			},
			{
				"type": "message",
				"role": "system",
				"content": "System update."
			}
		]
	}`)

	normalized, changed, err := normalizeResponsesRequestForLlamaCpp(body)
	require.NoError(t, err)
	require.True(t, changed)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(normalized, &decoded))

	assert.Equal(t, "Base instructions.\n\nDeveloper instructions.\n\nSystem update.", decoded["instructions"])

	input := decoded["input"].([]any)
	require.Len(t, input, 1)
	message := input[0].(map[string]any)
	assert.Equal(t, "message", message["type"])
	assert.Equal(t, "user", message["role"])
}

func TestProxyManager_ResponsesTools_CodexRequestDetection(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/responses", nil)
	assert.False(t, isCodexRequest(req))

	req = httptest.NewRequest("POST", "/v1/responses", nil)
	req.Header.Set("x-codex-window-id", "window")
	assert.True(t, isCodexRequest(req))

	req = httptest.NewRequest("POST", "/v1/responses", nil)
	req.Header.Set("User-Agent", "codex-tui/0.128.0")
	assert.True(t, isCodexRequest(req))

	req = httptest.NewRequest("GET", "/v1/models?client_version=0.128.0", nil)
	assert.True(t, isCodexModelCatalogRequest(req))
}
