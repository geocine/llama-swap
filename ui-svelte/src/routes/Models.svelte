<script lang="ts">
  import { isNarrow } from "../stores/theme";
  import { models, upstreamLogs } from "../stores/api";
  import { persistentStore } from "../stores/persistent";
  import ModelsPanel from "../components/ModelsPanel.svelte";
  import StatsPanel from "../components/StatsPanel.svelte";
  import LogPanel from "../components/LogPanel.svelte";
  import ResizablePanels from "../components/ResizablePanels.svelte";
  import ApiInfoPanel from "../components/api-info/ApiInfoPanel.svelte";

  type ModelsTab = "models" | "connect";

  let direction = $derived<"horizontal" | "vertical">($isNarrow ? "vertical" : "horizontal");
  const activeTabStore = persistentStore<ModelsTab>("models-active-tab", "models");

  let activeModel = $derived($models.find((m) => m.state === "ready"));
  let loadingModel = $derived($models.find((m) => m.state === "starting"));
</script>

<ResizablePanels {direction} storageKey="models-panel-group">
  {#snippet leftPanel()}
    <div class="flex h-full min-h-0 flex-col">
      <div class="mb-2 flex shrink-0 items-center gap-0 border-b border-border">
        <button
          class="tab-btn"
          class:tab-btn-active={$activeTabStore === "models"}
          onclick={() => activeTabStore.set("models")}
        >
          Models
        </button>
        <button
          class="tab-btn"
          class:tab-btn-active={$activeTabStore === "connect"}
          onclick={() => activeTabStore.set("connect")}
        >
          Connect
        </button>

        <div class="ml-auto flex min-w-0 items-center gap-2 px-2 font-mono text-[10px] uppercase tracking-widest">
          {#if activeModel}
            <span class="status-dot status-dot--ready" aria-hidden="true"></span>
            <span class="text-txtmuted">Active</span>
            <span class="truncate text-txtmain" title={activeModel.id}>{activeModel.id}</span>
          {:else if loadingModel}
            <span class="status-dot status-dot--starting" aria-hidden="true"></span>
            <span class="text-txtmuted">Loading</span>
            <span class="truncate text-txtmain" title={loadingModel.id}>{loadingModel.id}</span>
          {:else}
            <span class="status-dot status-dot--idle" aria-hidden="true"></span>
            <span class="text-txtmuted">No active model</span>
          {/if}
        </div>
      </div>

      <div class="min-h-0 flex-1">
        {#if $activeTabStore === "models"}
          <ModelsPanel />
        {:else}
          <div class="card h-full overflow-y-auto">
            <ApiInfoPanel />
          </div>
        {/if}
      </div>
    </div>
  {/snippet}
  {#snippet rightPanel()}
    <div class="flex flex-col h-full space-y-4">
      {#if direction === "horizontal"}
        <StatsPanel />
      {/if}
      <div class="flex-1 min-h-0">
        <LogPanel id="modelsupstream" title="Upstream Logs" logData={$upstreamLogs} />
      </div>
    </div>
  {/snippet}
</ResizablePanels>

<style>
  .tab-btn {
    padding: 0.5rem 0.875rem;
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--color-txtsecondary);
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    margin-bottom: -1px;
    cursor: pointer;
    transition: color 150ms ease, border-color 150ms ease;
  }
  .tab-btn:hover {
    color: var(--color-txtmain);
  }
  .tab-btn-active {
    color: #ffffff;
    border-bottom-color: #ffffff;
  }

  .status-dot {
    display: inline-block;
    width: 6px;
    height: 6px;
    border-radius: 9999px;
    flex-shrink: 0;
  }
  .status-dot--ready {
    background: var(--color-success);
    box-shadow: 0 0 6px color-mix(in srgb, var(--color-success) 60%, transparent);
  }
  .status-dot--starting {
    background: var(--color-warning);
    animation: dot-pulse 1.4s ease-in-out infinite;
  }
  .status-dot--idle {
    background: var(--color-txtmuted);
  }

  @keyframes dot-pulse {
    0%, 100% {
      opacity: 1;
    }
    50% {
      opacity: 0.4;
    }
  }
</style>
