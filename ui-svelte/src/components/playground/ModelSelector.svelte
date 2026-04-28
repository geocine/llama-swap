<script lang="ts">
  import { models } from "../../stores/api";
  import { groupModels } from "../../lib/modelUtils";
  import { resolveSelectedModel } from "../../lib/modelLoading";
  import type { ModelStatus } from "../../lib/types";
  import { ChevronDown, Check } from "lucide-svelte";

  interface Props {
    value: string;
    placeholder?: string;
    disabled?: boolean;
  }

  let { value = $bindable(), placeholder = "Select a model...", disabled = false }: Props = $props();

  let grouped = $derived(groupModels($models));
  let peerEntries = $derived(
    Object.entries(grouped.peersByProvider).sort(([a], [b]) => a.localeCompare(b))
  );
  let hasModels = $derived(grouped.local.length > 0 || peerEntries.length > 0);
  let hasPeers = $derived(peerEntries.length > 0);
  let selectedModel = $derived(value ? resolveSelectedModel($models, value) : undefined);

  let isOpen = $state(false);
  let rootEl: HTMLDivElement | undefined = $state();

  function toggle() {
    if (disabled) return;
    isOpen = !isOpen;
  }

  function close() {
    isOpen = false;
  }

  function select(modelId: string) {
    value = modelId;
    close();
  }

  // Map model state to a status dot color and short label for the trigger/list
  function dotClass(state: ModelStatus): string {
    switch (state) {
      case "ready":
        return "bg-success";
      case "starting":
      case "stopping":
        return "bg-warning animate-pulse";
      case "stopped":
        return "bg-zinc-600";
      case "shutdown":
      case "unknown":
      default:
        return "bg-error";
    }
  }

  function stateLabel(state: ModelStatus): string {
    switch (state) {
      case "ready":
        return "Ready";
      case "starting":
        return "Loading";
      case "stopping":
        return "Stopping";
      case "stopped":
        return "Idle";
      case "shutdown":
        return "Shutdown";
      case "unknown":
      default:
        return "Unknown";
    }
  }

  function handleDocumentClick(event: MouseEvent) {
    if (!rootEl) return;
    const target = event.target as Node | null;
    if (target && !rootEl.contains(target)) {
      close();
    }
  }

  function handleKeyDown(event: KeyboardEvent) {
    if (event.key === "Escape" && isOpen) {
      event.preventDefault();
      close();
    }
  }

  $effect(() => {
    if (!isOpen) return;
    document.addEventListener("click", handleDocumentClick, true);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("click", handleDocumentClick, true);
      document.removeEventListener("keydown", handleKeyDown);
    };
  });
</script>

{#if hasModels}
  <div class="relative min-w-0 flex-1 basis-48" bind:this={rootEl}>
    <!-- Trigger -->
    <button
      type="button"
      onclick={toggle}
      {disabled}
      aria-haspopup="listbox"
      aria-expanded={isOpen}
      class="flex w-full items-center gap-2 rounded-sm border border-border bg-surface px-3 py-2 text-left text-xs font-bold uppercase tracking-wide text-txtmain transition-colors duration-150 hover:border-border-hover focus:border-white focus:outline-none disabled:cursor-not-allowed disabled:opacity-50"
      class:open={isOpen}
    >
      {#if selectedModel}
        <span
          class="h-1.5 w-1.5 shrink-0 rounded-full {dotClass(selectedModel.state)}"
          title={stateLabel(selectedModel.state)}
        ></span>
      {/if}
      <span class="min-w-0 flex-1 truncate {value ? 'text-txtmain' : 'text-txtsecondary'}">
        {value || placeholder}
      </span>
      {#if selectedModel}
        <span class="hidden font-mono text-[10px] tracking-widest text-txtmuted sm:inline">
          {stateLabel(selectedModel.state)}
        </span>
      {/if}
      <ChevronDown
        class="h-4 w-4 shrink-0 text-txtsecondary transition-transform duration-150 {isOpen ? 'rotate-180' : ''}"
      />
    </button>

    <!-- Panel -->
    {#if isOpen}
      <div
        class="absolute left-0 right-0 top-full z-30 mt-1 max-h-80 overflow-y-auto rounded-sm border border-border bg-surface-elevated shadow-2xl shadow-black/40"
        role="listbox"
      >
        {#if grouped.local.length > 0}
          {#if hasPeers}
            <div class="px-3 pb-1 pt-2 font-mono text-[10px] uppercase tracking-widest text-txtmuted">
              Local
            </div>
          {/if}
          {#each grouped.local as model (model.id)}
            <button
              type="button"
              role="option"
              aria-selected={value === model.id}
              onclick={() => select(model.id)}
              class="flex w-full items-center gap-2 px-3 py-2 text-left text-xs font-bold uppercase tracking-wide transition-colors duration-150 hover:bg-secondary {value === model.id ? 'text-white' : 'text-txtmain'}"
            >
              <Check class="h-3.5 w-3.5 shrink-0 {value === model.id ? 'text-white' : 'text-transparent'}" />
              <span class="h-1.5 w-1.5 shrink-0 rounded-full {dotClass(model.state)}"></span>
              <span class="min-w-0 flex-1 truncate">{model.id}</span>
              <span class="font-mono text-[10px] tracking-widest text-txtmuted">
                {stateLabel(model.state)}
              </span>
            </button>
            {#if model.aliases}
              {#each model.aliases as alias (alias)}
                <button
                  type="button"
                  role="option"
                  aria-selected={value === alias}
                  onclick={() => select(alias)}
                  class="flex w-full items-center gap-2 px-3 py-2 pl-8 text-left text-xs font-bold uppercase tracking-wide transition-colors duration-150 hover:bg-secondary {value === alias ? 'text-white' : 'text-txtsecondary'}"
                >
                  <Check class="h-3.5 w-3.5 shrink-0 {value === alias ? 'text-white' : 'text-transparent'}" />
                  <span class="min-w-0 flex-1 truncate font-mono normal-case tracking-normal">↳ {alias}</span>
                </button>
              {/each}
            {/if}
          {/each}
        {/if}

        {#each peerEntries as [peerId, peerModels] (peerId)}
          <div class="border-t border-border px-3 pb-1 pt-2 font-mono text-[10px] uppercase tracking-widest text-txtmuted first:border-t-0">
            Peer · {peerId}
          </div>
          {#each peerModels as model (model.id)}
            <button
              type="button"
              role="option"
              aria-selected={value === model.id}
              onclick={() => select(model.id)}
              class="flex w-full items-center gap-2 px-3 py-2 text-left text-xs font-bold uppercase tracking-wide transition-colors duration-150 hover:bg-secondary {value === model.id ? 'text-white' : 'text-txtmain'}"
            >
              <Check class="h-3.5 w-3.5 shrink-0 {value === model.id ? 'text-white' : 'text-transparent'}" />
              <span class="h-1.5 w-1.5 shrink-0 rounded-full {dotClass(model.state)}"></span>
              <span class="min-w-0 flex-1 truncate">{model.id}</span>
              <span class="font-mono text-[10px] tracking-widest text-txtmuted">
                {stateLabel(model.state)}
              </span>
            </button>
          {/each}
        {/each}
      </div>
    {/if}
  </div>
{/if}

<style>
  .open {
    border-color: var(--color-border-hover);
  }
</style>
