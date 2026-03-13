#!/bin/sh

set -eu

SERVER_BIN="${LLAMA_SERVER_BIN:-/app/llama-server}"
PROGRESS_INTERVAL="${LLAMA_DOWNLOAD_PROGRESS_INTERVAL:-15}"
DOWNLOAD_META_DIR=""
SERVER_LOG_PIPE=""

format_bytes() {
    bytes="$1"
    gb_whole=$((bytes / 1000000000))
    gb_frac=$(((bytes % 1000000000) * 100 / 1000000000))
    printf '%d.%02d GB' "$gb_whole" "$gb_frac"
}

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

safe_meta_name() {
    printf '%s' "$1" | tr '/:' '__'
}

store_download_total_bytes() {
    download_path="$1"
    download_url="$2"
    [ -n "$DOWNLOAD_META_DIR" ] || return

    meta_name="$(safe_meta_name "${download_path##*/}")"
    total_file="$DOWNLOAD_META_DIR/$meta_name.total"
    url_file="$DOWNLOAD_META_DIR/$meta_name.url"

    if [ -f "$total_file" ]; then
        return
    fi

    auth_args=""
    if [ -n "${HF_TOKEN:-}" ]; then
        auth_args="-H Authorization: Bearer ${HF_TOKEN}"
    fi

    headers="$(curl -fsSLI $auth_args "$download_url" 2>/dev/null || true)"
    total_bytes="$(printf '%s\n' "$headers" | tr -d '\r' | awk 'tolower($1) == "content-length:" { value=$2 } END { print value }')"

    if [ -n "$total_bytes" ]; then
        printf '%s\n' "$total_bytes" > "$total_file"
        printf '%s\n' "$download_url" > "$url_file"
        printf 'llama-server-progress: resolved total size for %s (%s)\n' \
            "${download_path##*/}" \
            "$(format_bytes "$total_bytes")"
    fi
}

capture_download_metadata() {
    line="$1"

    case "$line" in
        *"common_download_file_single_online: downloading from "*".downloadInProgress "*)
            download_url="$(printf '%s\n' "$line" | sed -n 's/.*downloading from \(https:\/\/[^ ]*\) to .*/\1/p')"
            download_path="$(printf '%s\n' "$line" | sed -n 's/.* to \([^ ]*\.downloadInProgress\) (.*/\1/p')"
            if [ -n "$download_url" ] && [ -n "$download_path" ]; then
                store_download_total_bytes "$download_path" "$download_url"
            fi
            ;;
    esac
}

relay_server_logs() {
    while IFS= read -r line || [ -n "$line" ]; do
        printf '%s\n' "$line"
        capture_download_metadata "$line"
    done < "$SERVER_LOG_PIPE"
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
            name=${file##*/}
            meta_name="$(safe_meta_name "$name")"
            total_file="$DOWNLOAD_META_DIR/$meta_name.total"

            if [ -f "$total_file" ]; then
                total_bytes="$(cat "$total_file" 2>/dev/null || true)"
            else
                total_bytes=""
            fi

            if [ -n "$total_bytes" ] && [ "$total_bytes" -gt 0 ] 2>/dev/null; then
                pct_tenths=$((bytes * 1000 / total_bytes))
                pct_whole=$((pct_tenths / 10))
                pct_frac=$((pct_tenths % 10))
                printf 'llama-server-progress: downloading %s (%s / %s, %d.%d%%)\n' \
                    "$name" \
                    "$(format_bytes "$bytes")" \
                    "$(format_bytes "$total_bytes")" \
                    "$pct_whole" \
                    "$pct_frac"
            else
                printf 'llama-server-progress: downloading %s (%s)\n' \
                    "$name" \
                    "$(format_bytes "$bytes")"
            fi
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

cleanup() {
    if [ -n "${MONITOR_PID:-}" ]; then
        kill "$MONITOR_PID" 2>/dev/null || true
        wait "$MONITOR_PID" 2>/dev/null || true
    fi

    if [ -n "${LOGGER_PID:-}" ]; then
        kill "$LOGGER_PID" 2>/dev/null || true
        wait "$LOGGER_PID" 2>/dev/null || true
    fi

    if [ -n "$SERVER_LOG_PIPE" ] && [ -p "$SERVER_LOG_PIPE" ]; then
        rm -f "$SERVER_LOG_PIPE"
    fi

    if [ -n "$DOWNLOAD_META_DIR" ] && [ -d "$DOWNLOAD_META_DIR" ]; then
        rm -rf "$DOWNLOAD_META_DIR"
    fi
}

CACHE_DIR="$(resolve_cache_dir)"
DOWNLOAD_META_DIR="$(mktemp -d)"
SERVER_LOG_PIPE="$(mktemp -u)"
mkfifo "$SERVER_LOG_PIPE"

relay_server_logs &
LOGGER_PID=$!

"$SERVER_BIN" "$@" > "$SERVER_LOG_PIPE" 2>&1 &
SERVER_PID=$!

trap 'forward_signal' INT TERM
trap 'cleanup' EXIT

monitor_downloads "$CACHE_DIR" &
MONITOR_PID=$!

set +e
wait "$SERVER_PID"
STATUS=$?
set -e

exit "$STATUS"
