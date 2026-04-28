#!/bin/sh

set -eu

CONFIG_PATH="${LLAMA_SWAP_CONFIG:-/app/config.yaml}"
CONFIG_URL="${LLAMA_SWAP_CONFIG_URL:-}"

if [ -n "$CONFIG_URL" ]; then
    CONFIG_PATH="${LLAMA_SWAP_DOWNLOADED_CONFIG:-/tmp/llama-swap.config.yaml}"

    printf 'quickpod-entrypoint: downloading config from %s\n' "$CONFIG_URL"
    if [ -n "${GITHUB_TOKEN:-}" ]; then
        curl -fsSL -H "Authorization: Bearer ${GITHUB_TOKEN}" "$CONFIG_URL" -o "$CONFIG_PATH"
    else
        curl -fsSL "$CONFIG_URL" -o "$CONFIG_PATH"
    fi
fi

exec /app/llama-swap -config "$CONFIG_PATH" "$@"
