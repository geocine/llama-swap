<script lang="ts">
  import { metrics, getCapture, downloadActivityDB, clearActivity } from "../stores/api";
  import { confirmDialog } from "../stores/confirm";
  import Tooltip from "../components/Tooltip.svelte";
  import CaptureDialog from "../components/CaptureDialog.svelte";
  import type { ReqRespCapture } from "../lib/types";

  function formatSpeed(speed: number): string {
    return speed < 0 ? "unknown" : speed.toFixed(2) + " t/s";
  }

  function formatDuration(ms: number): string {
    return (ms / 1000).toFixed(2) + "s";
  }

  function formatRelativeTime(timestamp: string): string {
    const now = new Date();
    const date = new Date(timestamp);
    const diffInSeconds = Math.floor((now.getTime() - date.getTime()) / 1000);

    // Handle future dates by returning "just now"
    if (diffInSeconds < 5) {
      return "now";
    }

    if (diffInSeconds < 60) {
      return `${diffInSeconds}s ago`;
    }

    const diffInMinutes = Math.floor(diffInSeconds / 60);
    if (diffInMinutes < 60) {
      return `${diffInMinutes}m ago`;
    }

    const diffInHours = Math.floor(diffInMinutes / 60);
    if (diffInHours < 24) {
      return `${diffInHours}h ago`;
    }

    return "a while ago";
  }

  let sortedMetrics = $derived([...$metrics].sort((a, b) => b.id - a.id));

  let selectedCapture = $state<ReqRespCapture | null>(null);
  let dialogOpen = $state(false);
  let loadingCaptureId = $state<number | null>(null);
  let missingCaptureIds = $state<Set<number>>(new Set());
  let downloadingDB = $state(false);
  let clearingActivity = $state(false);

  async function viewCapture(id: number) {
    loadingCaptureId = id;
    const capture = await getCapture(id);
    loadingCaptureId = null;
    if (capture) {
      selectedCapture = capture;
      dialogOpen = true;
    } else {
      missingCaptureIds = new Set([...missingCaptureIds, id]);
    }
  }

  function closeDialog() {
    dialogOpen = false;
    selectedCapture = null;
  }

  async function downloadDB() {
    downloadingDB = true;
    try {
      await downloadActivityDB();
    } finally {
      downloadingDB = false;
    }
  }

  async function clearAllActivity() {
    const ok = await confirmDialog({
      title: "Clear activity",
      message: "Clear all activity metrics and captures? This cannot be undone.",
      confirmLabel: "Clear",
      cancelLabel: "Cancel",
      danger: true,
    });
    if (!ok) {
      return;
    }
    clearingActivity = true;
    try {
      await clearActivity();
      missingCaptureIds = new Set();
    } finally {
      clearingActivity = false;
    }
  }
</script>

<div class="p-2">
  <div class="mb-3 flex items-center justify-between gap-3">
    <h1 class="text-sm font-bold uppercase tracking-wide">Activity</h1>
    <div class="flex items-center gap-2">
      <button class="btn btn--sm" onclick={downloadDB} disabled={downloadingDB || $metrics.length === 0}>
        {downloadingDB ? "..." : "Download DB"}
      </button>
      <button class="btn btn--sm" onclick={clearAllActivity} disabled={clearingActivity || $metrics.length === 0}>
        {clearingActivity ? "..." : "Clear"}
      </button>
    </div>
  </div>

  {#if $metrics.length === 0}
    <div class="text-center py-8">
      <p class="text-sm text-txtsecondary">No metrics data available</p>
    </div>
  {:else}
    <div class="card overflow-auto">
      <table class="min-w-full divide-y divide-border">
        <thead>
          <tr class="text-left text-[10px] font-bold uppercase tracking-widest text-txtsecondary">
            <th class="px-4 py-3">ID</th>
            <th class="px-4 py-3">Time</th>
            <th class="px-4 py-3">Model</th>
            <th class="px-4 py-3">
              Cached <Tooltip content="prompt tokens from cache" />
            </th>
            <th class="px-4 py-3">
              Prompt <Tooltip content="new prompt tokens processed" />
            </th>
            <th class="px-4 py-3">Generated</th>
            <th class="px-4 py-3">Prompt Processing</th>
            <th class="px-4 py-3">Generation Speed</th>
            <th class="px-4 py-3">Duration</th>
            <th class="px-4 py-3">Capture</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-border">
          {#each sortedMetrics as metric (metric.id)}
            <tr class="whitespace-nowrap text-sm hover:bg-secondary transition-colors duration-150">
              <td class="px-4 py-3 font-mono text-txtsecondary">{metric.id + 1}</td>
              <td class="px-4 py-3 font-mono text-txtsecondary">{formatRelativeTime(metric.timestamp)}</td>
              <td class="px-4 py-3 font-mono">{metric.model}</td>
              <td class="px-4 py-3 font-mono">{metric.cache_tokens > 0 ? metric.cache_tokens.toLocaleString() : "-"}</td>
              <td class="px-4 py-3 font-mono">{metric.input_tokens.toLocaleString()}</td>
              <td class="px-4 py-3 font-mono">{metric.output_tokens.toLocaleString()}</td>
              <td class="px-4 py-3 font-mono">{formatSpeed(metric.prompt_per_second)}</td>
              <td class="px-4 py-3 font-mono">{formatSpeed(metric.tokens_per_second)}</td>
              <td class="px-4 py-3 font-mono">{formatDuration(metric.duration_ms)}</td>
              <td class="px-4 py-3">
                {#if metric.has_capture && !missingCaptureIds.has(metric.id)}
                  <button
                    onclick={() => viewCapture(metric.id)}
                    disabled={loadingCaptureId === metric.id}
                    class="btn btn--sm"
                  >
                    {loadingCaptureId === metric.id ? "..." : "View"}
                  </button>
                {:else}
                  <span class="text-txtsecondary">-</span>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<CaptureDialog capture={selectedCapture} open={dialogOpen} onclose={closeDialog} />
