<script lang="ts">
  import {
    listModelConfigSettings,
    resetModelConfigSettings,
    saveModelConfigSettings,
  } from "../stores/api";
  import type { EditableModelConfig, SessionModelSettings } from "../lib/types";
  import { RotateCcw, Save, X } from "lucide-svelte";

  interface Props {
    open: boolean;
    modelId: string;
    onClose: () => void;
  }

  let { open, modelId, onClose }: Props = $props();

  let configs = $state<EditableModelConfig[]>([]);
  let activeConfig = $derived(configs.find((config) => config.modelId === modelId) ?? null);
  let settings = $state<SessionModelSettings>({
    source: "",
    serverArgs: "",
    kvCacheArgs: "",
    samplingArgs: "",
  });
  let loading = $state(false);
  let saving = $state(false);
  let message = $state("");
  let error = $state("");

  $effect(() => {
    if (!open) return;
    void loadConfigs();
  });

  $effect(() => {
    if (!activeConfig) return;
    settings = { ...activeConfig.effective };
  });

  async function loadConfigs(): Promise<void> {
    loading = true;
    message = "";
    error = "";
    try {
      configs = await listModelConfigSettings();
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to load settings";
    } finally {
      loading = false;
    }
  }

  async function save(): Promise<void> {
    if (!activeConfig?.editable) return;
    saving = true;
    message = "";
    error = "";
    try {
      const updated = await saveModelConfigSettings(modelId, settings);
      configs = configs.map((config) => (config.modelId === modelId ? updated : config));
      message = "Saved. Applies on next load.";
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to save settings";
    } finally {
      saving = false;
    }
  }

  async function reset(): Promise<void> {
    saving = true;
    message = "";
    error = "";
    try {
      const updated = await resetModelConfigSettings(modelId);
      configs = configs.map((config) => (config.modelId === modelId ? updated : config));
      settings = { ...updated.effective };
      message = "Reset. Base config applies on next load.";
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to reset settings";
    } finally {
      saving = false;
    }
  }

  function backdropClick(event: MouseEvent): void {
    if (event.target === event.currentTarget) {
      onClose();
    }
  }
</script>

{#if open}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 px-4 py-6" onclick={backdropClick}>
    <div class="flex max-h-full w-full max-w-3xl flex-col overflow-hidden rounded-sm border border-border bg-surface shadow-2xl shadow-black">
      <div class="flex items-center gap-3 border-b border-border px-4 py-3">
        <div class="min-w-0">
          <div class="font-mono text-[10px] uppercase tracking-widest text-txtsecondary">Model config</div>
          <h2 class="truncate text-base font-bold text-txtmain">{modelId}</h2>
        </div>
        <div class="ml-auto flex items-center gap-2">
          <button class="btn p-2" onclick={onClose} title="Close" aria-label="Close">
            <X class="h-4 w-4" />
          </button>
        </div>
      </div>

      <div class="min-h-0 flex-1 overflow-y-auto p-4">
        {#if loading}
          <div class="py-12 text-center font-mono text-[11px] uppercase tracking-widest text-txtsecondary">Loading settings</div>
        {:else if !activeConfig}
          <div class="py-12 text-center font-mono text-[11px] uppercase tracking-widest text-error">Model not found</div>
        {:else if !activeConfig.editable}
          <div class="rounded-sm border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-warning">
            {activeConfig.message || "This model command cannot be edited from the UI."}
          </div>
        {:else}
          <div class="grid gap-4">
            <label class="field">
              <span>HF source</span>
              <input bind:value={settings.source} class="input" spellcheck="false" />
            </label>

            <label class="field">
              <span>Server args</span>
              <textarea bind:value={settings.serverArgs} class="textarea" rows="4" spellcheck="false"></textarea>
            </label>

            <label class="field">
              <span>KV cache args</span>
              <textarea bind:value={settings.kvCacheArgs} class="textarea" rows="3" spellcheck="false"></textarea>
            </label>

            <label class="field">
              <span>Sampling args</span>
              <textarea bind:value={settings.samplingArgs} class="textarea" rows="3" spellcheck="false"></textarea>
            </label>

            <div class="rounded-sm border border-border bg-black/40 px-3 py-2">
              <div class="mb-1 font-mono text-[10px] uppercase tracking-widest text-txtsecondary">Generated command</div>
              <pre class="whitespace-pre-wrap break-words font-mono text-xs text-txtmain">{activeConfig.command}</pre>
            </div>

            {#if activeConfig.override}
              <div class="font-mono text-[10px] uppercase tracking-widest text-txtsecondary">
                Session override active
              </div>
            {/if}
          </div>
        {/if}
      </div>

      <div class="flex flex-wrap items-center gap-2 border-t border-border px-4 py-3">
        {#if error}
          <div class="mr-auto text-xs text-error">{error}</div>
        {:else if message}
          <div class="mr-auto text-xs text-success">{message}</div>
        {:else}
          <div class="mr-auto text-xs text-txtsecondary">Changes apply the next time this model loads.</div>
        {/if}

        <button
          class="btn flex items-center gap-2"
          onclick={reset}
          disabled={!activeConfig?.override || saving}
        >
          <RotateCcw class="h-4 w-4" />
          Reset
        </button>
        <button
          class="btn flex items-center gap-2"
          onclick={save}
          disabled={!activeConfig?.editable || saving}
        >
          <Save class="h-4 w-4" />
          {saving ? "Saving" : "Save"}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .field {
    display: grid;
    gap: 0.5rem;
  }

  .field span {
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--color-txtsecondary);
  }

  .input,
  .textarea {
    width: 100%;
    border: 1px solid var(--color-border);
    border-radius: 2px;
    background: #000000;
    color: var(--color-txtmain);
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.5;
    outline: none;
    padding: 0.625rem 0.75rem;
  }

  .textarea {
    resize: vertical;
  }

  .input:focus,
  .textarea:focus {
    border-color: #ffffff;
  }
</style>
