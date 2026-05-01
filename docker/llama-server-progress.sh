#!/bin/sh

set -eu

SERVER_BIN="${LLAMA_SERVER_BIN:-/app/llama-server}"
PROGRESS_INTERVAL="${LLAMA_DOWNLOAD_PROGRESS_INTERVAL:-15}"
DOWNLOAD_META_DIR=""
SERVER_LOG_PIPE=""
ACTIVE_DOWNLOADS_FILE=""

format_bytes() {
    bytes="$1"
    gb_whole=$((bytes / 1000000000))
    gb_frac=$(((bytes % 1000000000) * 100 / 1000000000))
    printf '%d.%02d GB' "$gb_whole" "$gb_frac"
}

resolve_cache_dirs() {
    if [ -n "${LLAMA_CACHE:-}" ]; then
        printf '%s\n' "$LLAMA_CACHE"
    fi

    if [ -n "${XDG_CACHE_HOME:-}" ]; then
        printf '%s/llama.cpp\n' "$XDG_CACHE_HOME"
        printf '%s/huggingface/hub\n' "$XDG_CACHE_HOME"
        return
    fi

    if [ -n "${HOME:-}" ]; then
        printf '%s/.cache/llama.cpp\n' "$HOME"
        printf '%s/.cache/huggingface/hub\n' "$HOME"
        return
    fi

    printf '/root/.cache/llama.cpp\n'
    printf '/root/.cache/huggingface/hub\n'
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
    active_file="$DOWNLOAD_META_DIR/$meta_name.active"

    printf '%s\n' "$download_path" > "$active_file"
    printf '%s\n' "$download_path" >> "$ACTIVE_DOWNLOADS_FILE"

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

hf_repo_from_cache_path() {
    path="$1"
    repo_part="$(printf '%s\n' "$path" | sed -n 's#.*models--\([^/]*\)/blobs/.*#\1#p')"
    [ -n "$repo_part" ] || return

    owner="$(printf '%s\n' "$repo_part" | sed 's/--.*//')"
    repo="$(printf '%s\n' "$repo_part" | sed 's/^[^-]*--//')"
    [ -n "$owner" ] && [ -n "$repo" ] || return
    printf '%s/%s\n' "$owner" "$repo"
}

resolve_hf_blob_metadata() {
    download_path="$1"
    meta_name="$2"
    [ -n "$DOWNLOAD_META_DIR" ] || return

    total_file="$DOWNLOAD_META_DIR/$meta_name.total"
    display_file="$DOWNLOAD_META_DIR/$meta_name.display"
    [ -f "$total_file" ] && return

    blob_name="${download_path##*/}"
    blob_sha="${blob_name%.downloadInProgress}"
    repo="$(hf_repo_from_cache_path "$download_path" || true)"
    [ -n "$repo" ] || return

    repo_meta_name="$(safe_meta_name "$repo")"
    repo_meta_file="$DOWNLOAD_META_DIR/$repo_meta_name.json"

    if [ ! -f "$repo_meta_file" ]; then
        auth_args=""
        if [ -n "${HF_TOKEN:-}" ]; then
            auth_args="-H Authorization: Bearer ${HF_TOKEN}"
        fi

        curl -fsSL $auth_args "https://huggingface.co/api/models/$repo?blobs=true" \
            -o "$repo_meta_file" 2>/dev/null || true
    fi

    [ -s "$repo_meta_file" ] || return

    entry="$(
        tr '{' '\n' < "$repo_meta_file" |
            awk -v sha="$blob_sha" '
                index($0, "\"sha256\":\"" sha "\"") {
                    print prev
                    print $0
                    exit
                }
                { prev = $0 }
            '
    )"

    total_bytes="$(printf '%s\n' "$entry" | sed -n 's/.*"rfilename":"[^"]*","blobId":"[^"]*","size":\([0-9][0-9]*\).*/\1/p' | head -n 1)"
    display_name="$(printf '%s\n' "$entry" | sed -n 's/.*"rfilename":"\([^"]*\)".*/\1/p' | head -n 1)"

    if [ -n "$total_bytes" ]; then
        printf '%s\n' "$total_bytes" > "$total_file"
        [ -n "$display_name" ] && printf '%s\n' "$display_name" > "$display_file"
        printf 'llama-server-progress: resolved total size for %s (%s)\n' \
            "${display_name:-$blob_name}" \
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

collect_active_downloads() {
    cache_dirs="$1"
    output_file="$2"
    tmp_file="$DOWNLOAD_META_DIR/active-downloads.current"

    : > "$tmp_file"

    if [ -s "$ACTIVE_DOWNLOADS_FILE" ]; then
        cat "$ACTIVE_DOWNLOADS_FILE" >> "$tmp_file"
    fi

    printf '%s\n' "$cache_dirs" | while IFS= read -r cache_dir; do
        [ -n "$cache_dir" ] || continue
        [ -d "$cache_dir" ] || continue
        find "$cache_dir" -type f -name '*.downloadInProgress' 2>/dev/null >> "$tmp_file" || true
    done

    sort -u "$tmp_file" > "$output_file"
    cp "$output_file" "$ACTIVE_DOWNLOADS_FILE"
}

monitor_downloads() {
    cache_dirs="$1"
    seen_download=0

    printf '%s\n' "$cache_dirs" | while IFS= read -r cache_dir; do
        [ -n "$cache_dir" ] || continue
        printf 'llama-server-progress: monitoring cache directory %s every %ss\n' "$cache_dir" "$PROGRESS_INTERVAL"
    done

    while kill -0 "$SERVER_PID" 2>/dev/null; do
        active_download=0

        active_list="$DOWNLOAD_META_DIR/active-downloads.sorted"
        collect_active_downloads "$cache_dirs" "$active_list"

        if [ ! -s "$active_list" ]; then
            sleep "$PROGRESS_INTERVAL"
            continue
        fi

        total_known_bytes=0
        downloaded_known_bytes=0
        unknown_downloads=0
        active_names=""

        while IFS= read -r file; do
            current_file="$file"
            if [ ! -f "$current_file" ]; then
                case "$current_file" in
                    *.downloadInProgress)
                        completed_file="${current_file%.downloadInProgress}"
                        if [ -f "$completed_file" ]; then
                            current_file="$completed_file"
                        else
                            continue
                        fi
                        ;;
                    *)
                        continue
                        ;;
                esac
            fi

            seen_download=1
            case "$file" in
                *.downloadInProgress)
                    if [ -f "$file" ]; then
                        active_download=1
                    fi
                    ;;
                *)
                    active_download=1
                    ;;
            esac

            bytes=$(wc -c < "$current_file" | tr -d '[:space:]')
            name=${file##*/}
            meta_name="$(safe_meta_name "$name")"
            total_file="$DOWNLOAD_META_DIR/$meta_name.total"
            display_file="$DOWNLOAD_META_DIR/$meta_name.display"

            if [ ! -f "$total_file" ]; then
                resolve_hf_blob_metadata "$file" "$meta_name"
            fi

            if [ -f "$total_file" ]; then
                total_bytes="$(cat "$total_file" 2>/dev/null || true)"
            else
                total_bytes=""
            fi

            if [ -f "$display_file" ]; then
                display_name="$(cat "$display_file" 2>/dev/null || true)"
            else
                display_name="$name"
            fi

            if [ -n "$total_bytes" ] && [ "$total_bytes" -gt 0 ] 2>/dev/null; then
                total_known_bytes=$((total_known_bytes + total_bytes))
                if [ "$bytes" -gt "$total_bytes" ] 2>/dev/null; then
                    downloaded_known_bytes=$((downloaded_known_bytes + total_bytes))
                else
                    downloaded_known_bytes=$((downloaded_known_bytes + bytes))
                fi
                active_names="${active_names}${active_names:+, }$display_name"
                pct_tenths=$((bytes * 1000 / total_bytes))
                pct_whole=$((pct_tenths / 10))
                pct_frac=$((pct_tenths % 10))
                printf 'llama-server-progress: downloading %s (%s / %s, %d.%d%%)\n' \
                    "$display_name" \
                    "$(format_bytes "$bytes")" \
                    "$(format_bytes "$total_bytes")" \
                    "$pct_whole" \
                    "$pct_frac"
            else
                unknown_downloads=$((unknown_downloads + 1))
                printf 'llama-server-progress: downloading %s (%s)\n' \
                    "$display_name" \
                    "$(format_bytes "$bytes")"
            fi
        done < "$active_list"

        if [ "$total_known_bytes" -gt 0 ]; then
            pct_tenths=$((downloaded_known_bytes * 1000 / total_known_bytes))
            pct_whole=$((pct_tenths / 10))
            pct_frac=$((pct_tenths % 10))
            suffix=""
            if [ "$unknown_downloads" -gt 0 ]; then
                suffix=" + $unknown_downloads unknown"
            fi
            printf 'llama-server-progress: model download %s%s (%s / %s, %d.%d%%)\n' \
                "${active_names:-model files}" \
                "$suffix" \
                "$(format_bytes "$downloaded_known_bytes")" \
                "$(format_bytes "$total_known_bytes")" \
                "$pct_whole" \
                "$pct_frac"
        fi

        if [ "$active_download" -eq 0 ] && [ "$seen_download" -eq 1 ]; then
            printf 'llama-server-progress: download finished, waiting for model initialization\n'
            : > "$ACTIVE_DOWNLOADS_FILE"
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

CACHE_DIRS="$(resolve_cache_dirs)"
DOWNLOAD_META_DIR="$(mktemp -d)"
ACTIVE_DOWNLOADS_FILE="$DOWNLOAD_META_DIR/active-downloads"
: > "$ACTIVE_DOWNLOADS_FILE"
SERVER_LOG_PIPE="$(mktemp -u)"
mkfifo "$SERVER_LOG_PIPE"

relay_server_logs &
LOGGER_PID=$!

"$SERVER_BIN" "$@" > "$SERVER_LOG_PIPE" 2>&1 &
SERVER_PID=$!

trap 'forward_signal' INT TERM
trap 'cleanup' EXIT

monitor_downloads "$CACHE_DIRS" &
MONITOR_PID=$!

set +e
wait "$SERVER_PID"
STATUS=$?
set -e

exit "$STATUS"
