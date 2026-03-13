# Custom Image Guide

This file describes what this fork adds on top of upstream `llama-swap` for GPU rental services like QuickPod, RunPod, and Vast.ai where:

- you provide a Docker image
- the host runs that image directly
- configuration is mainly through environment variables
- Docker-in-Docker is not available

## What this fork changes

This fork adds a deployment flow aimed at hosted GPU containers:

- a fork-specific image build using `Dockerfile.quickpod`
- a source-built fork image using `Dockerfile.source.quickpod`
- a baked multi-model config in `quickpod.config.yaml`
- API protection through the `LS_KEY` environment variable
- a lightweight download-progress wrapper for `llama-server`
- docs for local Windows testing and hosted deployment

These changes are fork-specific and are not describing upstream `llama-swap` in general.

## Files

- `Dockerfile.quickpod`
- `Dockerfile.source.quickpod`
- `quickpod.config.yaml`
- `docker/llama-server-progress.sh`

## Which Dockerfile to use

### `Dockerfile.quickpod`

Use this when you want:

- the upstream `llama-swap` binary from `ghcr.io/mostlygeek/llama-swap:cuda`
- only fork-level config and wrapper script changes
- the fastest build

This Dockerfile does **not** compile Go source from this fork.

### `Dockerfile.source.quickpod`

Use this when you want:

- Go source changes from this fork included in the image
- UI assets built from this fork
- a container image that truly reflects local backend code changes from this fork

This is the Dockerfile to use when the fork changes application code, not just configuration.

## How `Dockerfile.quickpod` is built

This image does **not** compile `llama-swap` from source.

It starts from the published CUDA image:

```dockerfile
FROM ghcr.io/mostlygeek/llama-swap:cuda
```

That base image already contains:

- `/app/llama-swap`
- `/app/llama-server`
- the runtime libraries needed for CUDA inference

Then this build only adds two files:

1. `docker/llama-server-progress.sh` copied to `/app/llama-server-progress`
   - this wraps `llama-server`
   - it logs download progress by watching `.downloadInProgress` files in the llama.cpp cache
2. `quickpod.config.yaml` copied to `/app/config.yaml`
   - this becomes the active `llama-swap` config used at container startup

The base image already has its entrypoint set to start:

```shell
/app/llama-swap -config /app/config.yaml
```

So after the two `COPY` steps, the container is ready. There is no extra compile step in `Dockerfile.quickpod`.

## How `Dockerfile.source.quickpod` is built

This Dockerfile uses three stages:

1. a Node stage builds `ui-svelte` into `proxy/ui_dist`
2. a Go stage compiles `llama-swap` from this fork's source tree
3. a final runtime stage follows the upstream `docker/llama-swap.Containerfile` layout and starts from `ghcr.io/ggml-org/llama.cpp:server-cuda`

That means:

- `/app/llama-server` comes directly from the upstream `llama.cpp` CUDA image
- `/app/llama-swap` comes from this fork
- `/app/config.yaml` comes from `quickpod.config.yaml`
- `/app/llama-server-progress` comes from this fork
- the final runtime image recreates the upstream `/app` user, working directory, `PATH`, healthcheck, and entrypoint pattern

Use this Dockerfile if you need forked backend changes to actually ship in the image.

## Exact build command

Local build using upstream binary:

```shell
docker build -f Dockerfile.quickpod -t geocine/llama-swap:qwen35-upstream .
```

Local build from this fork's source:

```shell
docker build -f Dockerfile.source.quickpod -t geocine/llama-swap:qwen35 .
```

Build and push in one step from this fork's source:

```shell
docker buildx build --platform linux/amd64 \
  -f Dockerfile.source.quickpod \
  -t geocine/llama-swap:qwen35 \
  --push .
```

## Push command

```shell
docker login
docker push geocine/llama-swap:qwen35
```

## QuickPod settings for this fork

- Template Type: `GPU`
- Docker Image Path: `geocine/llama-swap:qwen35`
- Docker Options: `-p 8675:8080`
- Disk Space: `150 GB` is a reasonable starting point

## Environment variables used by this fork

- `LS_KEY` required
  - protects API routes and the UI's API calls
- `HF_TOKEN` optional
  - needed only if a Hugging Face model requires auth
- `CUDA_VISIBLE_DEVICES=0` optional
  - pins the container to a specific GPU
- `LLAMA_CACHE=/cache/llama` optional
  - overrides the llama.cpp cache path inside the container
- `LLAMA_DOWNLOAD_PROGRESS_INTERVAL=15` optional
  - controls progress log frequency in seconds

## Local Windows run

Use **PowerShell**, not Git Bash, because Git Bash can rewrite container paths like `/cache/llama`.

```powershell
docker run --rm --gpus all -p 8080:8080 `
  -e LS_KEY=replace-me `
  -e LLAMA_CACHE=/cache/llama `
  -v "$env:USERPROFILE\AppData\Local\llama.cpp:/cache/llama" `
  -v "${PWD}\models:/models" `
  geocine/llama-swap:qwen35
```

For the current `quickpod.config.yaml`, the `/cache/llama` mount is the important one because the configured models resolve through the llama.cpp cache. The `/models` mount is still useful for local configs that reference files under `/models`.

## Auth added by this fork

- API routes are protected
- the UI now shows a password form instead of triggering a browser Basic Auth prompt
- the UI password is the same value configured in `LS_KEY`
- successful UI login sets a browser session cookie used by `/api/*`, `/v1/*`, and `/upstream/*`
- API clients can send:
  - `x-api-key: <LS_KEY>`
  - `Authorization: Bearer <LS_KEY>`
  - Basic Auth with password = `LS_KEY`

## Model IDs in this fork

- `qwen35-27b`
- `qwen35-27b-kv8`
- `qwen35-27b-opus46`
- `qwen35-27b-opus46-kv8`
- `qwen35-27b-uncensored`
- `qwen35-27b-fast`
- `qwen35-27b-opus46-fast`
- `qwen35-27b-uncensored-fast`
- `qwen35-35b-a3b-262k`
- `qwen35-35b-a3b-1m`
- `qwen35-35b-a3b-iq4nl`
- `qwen35-35b-a3b-iq4nl-kv8`
- `qwen35-35b-a3b-iq4nl-fast`

## Smoke tests

```shell
curl http://<host>:8675/health
curl http://<host>:8675/v1/models -H "x-api-key: replace-me"
```

OpenAI-style:

```shell
curl http://<host>:8675/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "x-api-key: replace-me" \
  -d '{
    "model": "qwen35-27b",
    "messages": [
      {"role": "user", "content": "Write a haiku about hot-swapping models."}
    ]
  }'
```

Anthropic-style:

```shell
curl http://<host>:8675/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: replace-me" \
  -d '{
    "model": "qwen35-27b-opus46",
    "max_tokens": 256,
    "messages": [
      {"role": "user", "content": "Summarize why llama-swap is useful."}
    ]
  }'
```

## Notes

- `llama-swap` listens on container port `8080`
- the first request for a model triggers load/download
- this fork's custom image is text-only
- the `z-image` example from `docker/config.example.yaml` needs an image that also includes `sd-server`
