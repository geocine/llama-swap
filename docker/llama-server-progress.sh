#!/bin/sh

set -eu

SERVER_BIN="${LLAMA_SERVER_BIN:-/app/llama-server}"
PROGRESS_INTERVAL="${LLAMA_DOWNLOAD_PROGRESS_INTERVAL:-15}"

resolve_cache_dir() {
    if [ -n "${LLAMA_CACHE:-}" ]; then
        printf '%s\n' "$LLAMA_CACHE"
        return
    fi

    if [ -n "${XDG_CACHE_HOME:-}" ]; then
        printf '%s/llama.cpp\n' "$XDG_CACHE_HOME"
        return
    fi

    if [ -n "${HOME:-}" ]; then
        printf '%s/.cache/llama.cpp\n' "$HOME"
        return
    fi

    printf '/root/.cache/llama.cpp\n'
}

format_gib() {
    bytes="$1"
    gib_whole=$((bytes / 1073741824))
    gib_frac=$(((bytes % 1073741824) * 100 / 1073741824))
    printf '%d.%02d GiB' "$gib_whole" "$gib_frac"
}

monitor_downloads() {
    cache_dir="$1"
    seen_download=0

    printf 'llama-server-progress: monitoring cache directory %s every %ss\n' "$cache_dir" "$PROGRESS_INTERVAL"

    while kill -0 "$SERVER_PID" 2>/dev/null; do
        active_download=0

        for file in "$cache_dir"/*.downloadInProgress; do
            [ -f "$file" ] || continue

            active_download=1
            seen_download=1

            bytes=$(wc -c < "$file" | tr -d '[:space:]')
            mib=$((bytes / 1048576))
            name=${file##*/}

            printf 'llama-server-progress: downloading %s (%s bytes, %s MiB, %s)\n' \
                "$name" \
                "$bytes" \
                "$mib" \
                "$(format_gib "$bytes")"
        done

        if [ "$active_download" -eq 0 ] && [ "$seen_download" -eq 1 ]; then
            printf 'llama-server-progress: download finished, waiting for model initialization\n'
            seen_download=0
        fi

        sleep "$PROGRESS_INTERVAL"
    done
}

forward_signal() {
    if kill -0 "$SERVER_PID" 2>/dev/null; then
        kill -TERM "$SERVER_PID" 2>/dev/null || true
    fi
}

CACHE_DIR="$(resolve_cache_dir)"

"$SERVER_BIN" "$@" &
SERVER_PID=$!

trap forward_signal INT TERM

monitor_downloads "$CACHE_DIR" &
MONITOR_PID=$!

set +e
wait "$SERVER_PID"
STATUS=$?
set -e

kill "$MONITOR_PID" 2>/dev/null || true
wait "$MONITOR_PID" 2>/dev/null || true

exit "$STATUS"
