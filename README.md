# llama-swap geocine fork

A lightweight proxy for hot-swapping local AI models, based on upstream [`mostlygeek/llama-swap`](https://github.com/mostlygeek/llama-swap).

This fork keeps llama-swap's core model swapping behavior and adds a hosted-GPU workflow, protected web UI, richer model management, persistent activity captures, and client setup helpers for OpenAI-compatible tools, Claude, and Codex.

## Fork Matrix

| Area | Upstream llama-swap | This fork |
| --- | --- | --- |
| Core proxy | OpenAI/Anthropic-compatible proxy with model hot-swapping | Same core proxy, plus extra client compatibility for Codex and hosted workflows |
| Deployment | General Docker, Homebrew, WinGet, release binaries, source builds | QuickPod-first cloud GPU image, source/prebuilt hosted image flow, remote config URL loading, private Gist config support |
| Models | YAML-defined models, groups, aliases, TTL, hooks | Expanded cloud config with Qwen 3.5/3.6 presets, high-context macros, KV cache macros, optional grammar args |
| Auth | API key support for inference endpoints | API key protected UI, browser login/logout, session cookie, bearer, `x-api-key`, and Basic Auth support |
| Web UI | Models, logs, metrics, playground | Reworked chat UI, persistent conversations, image attachments, reasoning display, context usage, model download progress |
| Model management | Load/unload configured models | Edit session model settings, reload models, import/export YAML, duplicate models, delete runtime-created models |
| Client setup | Standard OpenAI-compatible API surface | Copyable connection snippets for `curl`, Python OpenAI, Claude shortcut, and Codex profile |
| Activity and captures | Metrics and capture inspection | SQLite-backed activity storage, encrypted captures when API keys are configured, DB export, clear activity |
| Download visibility | Upstream logs | `llama-server-progress` wrapper with byte/percent progress surfaced in the UI |
| Codex support | Standard `/v1` compatibility | Codex model catalog shape, Responses API tool normalization, Codex CLI profile helper |
| Branding/UI polish | Upstream UI | Refreshed icons, confirm dialogs, animated login background |
| Tests | Upstream test suite | Added tests for auth, config editing, duplicated models, captures, Codex normalization, and UI helpers |

## Quick Start

### QuickPod / Cloud GPU

QuickPod is the easiest hosted path for this fork, though the same image can run on other GPU cloud providers.

1. Open [console.quickpod.io](https://console.quickpod.io/) and sign in.
2. In the left sidebar, open `Templates`.
3. Switch the template type toggle to `GPU`.
4. Click `Create New Template`.
5. Fill the template form:

| Field | Value |
| --- | --- |
| Template Type | `GPU` |
| Runtime | `Pod` |
| Template Name | `Llama Swap` |
| Docker Image Path | `geocine/llama-swap:latest` |
| Disk Space | `150 GB` |
| Private Docker Repository | Off |
| Docker Options | `-p 8080:8080 -e LS_KEY=your-password` |
| Launch Mode | `Docker Entrypoint` |
| Docker Entrypoint Run Options | Leave blank |
| On Start Script | Leave blank |

6. Click `Create Template`.
7. Go to `Templates` > `My Templates`, then click `Select` on `Llama Swap`.
8. Go back to the GPU pod search page.
9. Set the `Min VRAM` filter to `24 GB` or higher.
10. Choose a GPU and click `Create`.
11. In the pod creation modal, keep runtime as `Pod`, confirm the `Llama Swap` template, keep disk space at `150 GB` or higher, then click `Create Pod`.
12. Open the exposed web port and log in with the `LS_KEY` value you set.
13. In the llama-swap UI, open `Models`.
14. Pick the model you want and click `Load`. The first load may take a while while model weights download.
15. Stay on `Models` and open the `API Connection` panel.
16. Use the `Claude` tab to copy the `cllama` shell shortcut, or the `Codex` tab to copy the `~/.codex.toml` profile and `cdllama` shortcut.
17. For other tools, use the `curl` or `Python` tabs, or copy the base URL and API key directly.

#### Optional configuration

To load a remote config instead of the baked config, add `-e LLAMA_SWAP_CONFIG_URL=https://.../config.yaml` to `Docker Options`.

To build the image yourself instead of using Docker Hub:

```sh
docker build -f Dockerfile.source.quickpod -t llama-swap-geocine .
docker run --rm --gpus all -p 8080:8080 \
  -e LS_KEY=your-api-key \
  llama-swap-geocine
```

### Local binary

```sh
llama-swap --config config.yaml --listen localhost:8080
```

Minimum config:

```yaml
models:
  model1:
    cmd: llama-server --port ${PORT} --model /path/to/model.gguf
```

## UI Highlights

- `Chat`: persistent conversations, image attachments, reasoning output, context usage, compaction, and stop/unload controls.
- `Models`: load/unload models, edit model settings, duplicate models, delete duplicated models, import/export YAML, and view download progress.
- `Activity`: inspect metrics, view request/response captures, clear activity, and download the activity SQLite DB.
- `Logs`: live proxy and upstream logs.
- `API Connection`: copy base URLs, API keys, and client snippets directly from the Models page.

## Notes

- Runtime model edits are session overrides stored in SQLite and can be exported back to YAML.
- Duplicated models are persisted and can be deleted from the UI.
- YAML-defined models are protected from UI deletion.
- Captures are encrypted when API keys are configured.
- If deploying behind nginx, disable response buffering for streaming and SSE routes.
