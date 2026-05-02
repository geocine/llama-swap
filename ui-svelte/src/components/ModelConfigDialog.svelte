<script lang="ts">
  import {
    deleteModelEntry,
    duplicateModelConfig,
    listModelConfigSettings,
    loadModel,
    resetModelConfigSettings,
    saveModelConfigSettings,
    unloadSingleModel,
  } from "../stores/api";
  import { confirmDialog } from "../stores/confirm";
  import type { EditableModelConfig, ModelStatus, SessionModelSettings } from "../lib/types";
  import { Copy, RefreshCw, RotateCcw, Save, Trash2, X } from "lucide-svelte";

  interface Props {
    open: boolean;
    modelId: string;
    onClose: () => void;
    onModelChanged?: (newModelId: string) => void;
  }

  let { open, modelId, onClose, onModelChanged }: Props = $props();

  let configs = $state<EditableModelConfig[]>([]);
  let activeConfig = $derived(configs.find((config) => config.modelId === modelId) ?? null);
  let settings = $state<SessionModelSettings>({
    alias: "",
    source: "",
    serverArgs: "",
    kvCacheArgs: "",
    samplingArgs: "",
    grammarArgs: "",
  });
  let loading = $state(false);
  let saving = $state(false);
  let reloading = $state(false);
  let needsReload = $state(false);
  let message = $state("");
  let error = $state("");

  function isLiveState(state: ModelStatus | undefined): boolean {
    return state === "ready" || state === "starting";
  }
  const defaultGrammarArgs = "--grammar-file /app/think.gbnf";
  const defaultKvCacheArgs = "-ctk q8_0 -ctv q8_0";

  // Rewrites the value of any --cache-type-k/-v or -ctk/-ctv flag to the
  // requested quant (e.g. q8_0). If no flag is present we fall back to a
  // sensible default so the button always produces a usable command.
  function applyKvCacheQuant(quant: string): void {
    const current = settings.kvCacheArgs ?? "";
    const flagPattern = /(--cache-type-[kv]|-ct[kv])(\s+|=)(\S+)/g;
    if (flagPattern.test(current)) {
      flagPattern.lastIndex = 0;
      settings.kvCacheArgs = current.replace(flagPattern, (_m, flag, sep) => `${flag}${sep}${quant}`);
      return;
    }
    settings.kvCacheArgs = `-ctk ${quant} -ctv ${quant}`;
  }

  $effect(() => {
    if (!open) return;
    needsReload = false;
    void loadConfigs();
  });

  $effect(() => {
    void modelId;
    needsReload = false;
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
    const wasLive = isLiveState(activeConfig?.state);
    saving = true;
    message = "";
    error = "";
    try {
      const updated = await saveModelConfigSettings(modelId, settings);
      configs = configs.map((config) => (config.modelId === modelId ? updated : config));
      if (wasLive || isLiveState(updated.state)) {
        needsReload = true;
        message = "";
      } else {
        needsReload = false;
        message = "Saved. Applies on next load.";
      }
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to save settings";
    } finally {
      saving = false;
    }
  }

  async function reset(): Promise<void> {
    const wasLive = isLiveState(activeConfig?.state);
    saving = true;
    message = "";
    error = "";
    try {
      const updated = await resetModelConfigSettings(modelId);
      configs = configs.map((config) => (config.modelId === modelId ? updated : config));
      settings = { ...updated.effective };
      if (wasLive || isLiveState(updated.state)) {
        needsReload = true;
        message = "";
      } else {
        needsReload = false;
        message = "Reset. Base config applies on next load.";
      }
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to reset settings";
    } finally {
      saving = false;
    }
  }

  // Cycle the model so the freshly-saved settings take effect immediately.
  // Stop first, then start; the load endpoint is idempotent if the model is
  // already running, so the explicit unload guarantees a fresh process.
  async function reloadModelNow(): Promise<void> {
    reloading = true;
    message = "";
    error = "";
    try {
      if (isLiveState(activeConfig?.state)) {
        await unloadSingleModel(modelId);
      }
      await loadModel(modelId);
      needsReload = false;
      message = "Model reloaded with the latest settings.";
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to reload model";
    } finally {
      reloading = false;
    }
  }

  // Duplicate clones the active model into a freshly named entry on the
  // backend and immediately switches the dialog to the new id so the user
  // can tweak it (alias, source, etc.) without re-opening anything.
  async function duplicate(): Promise<void> {
    if (!activeConfig) return;
    saving = true;
    message = "";
    error = "";
    try {
      const created = await duplicateModelConfig(modelId);
      configs = [...configs.filter((config) => config.modelId !== created.modelId), created];
      message = `Duplicated as "${created.modelId}".`;
      onModelChanged?.(created.modelId);
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to duplicate model";
    } finally {
      saving = false;
    }
  }

  // Delete is only available for user-added (duplicated) models. We confirm
  // first because the operation also tears down any running process for the
  // entry and removes its session overrides.
  async function deleteEntry(): Promise<void> {
    if (!activeConfig?.userAdded) return;
    const ok = await confirmDialog({
      title: "Delete model",
      message: `Delete "${modelId}"? This stops any running process for the model and removes its session settings. This cannot be undone.`,
      confirmLabel: "Delete",
      cancelLabel: "Cancel",
      danger: true,
    });
    if (!ok) return;

    saving = true;
    message = "";
    error = "";
    try {
      await deleteModelEntry(modelId);
      configs = configs.filter((config) => config.modelId !== modelId);
      onModelChanged?.("");
      onClose();
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to delete model";
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
          <div class="font-mono text-[10px] uppercase tracking-widest text-txtsecondary">
            Model config{#if activeConfig?.userAdded}
              {" \u00B7 "}<span class="text-success">duplicated{activeConfig.sourceModelId
                ? ` from ${activeConfig.sourceModelId}`
                : ""}</span>
            {/if}
          </div>
          <h2 class="truncate text-base font-bold text-txtmain">
            {settings.alias.trim() || modelId}
          </h2>
        </div>
        <div class="ml-auto flex items-center gap-2">
          <button
            class="btn p-2"
            onclick={duplicate}
            disabled={!activeConfig?.editable || saving}
            title="Duplicate this model"
            aria-label="Duplicate this model"
          >
            <Copy class="h-4 w-4" />
          </button>
          {#if activeConfig?.userAdded}
            <button
              class="btn p-2 text-error"
              onclick={deleteEntry}
              disabled={saving}
              title="Delete this duplicated model"
              aria-label="Delete this duplicated model"
            >
              <Trash2 class="h-4 w-4" />
            </button>
          {/if}
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
            {#if needsReload}
              <div class="flex flex-wrap items-start gap-3 rounded-sm border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-warning">
                <div class="flex-1 min-w-[12rem]">
                  <div class="font-mono text-[10px] font-bold uppercase tracking-widest">Reload required</div>
                  <p class="mt-0.5 text-xs leading-relaxed">
                    This model is currently loaded. Reload it now so your saved settings take effect on the next request.
                  </p>
                </div>
                <button
                  type="button"
                  class="btn flex items-center gap-2 border-warning/50 text-warning hover:border-warning hover:text-white disabled:cursor-not-allowed disabled:opacity-40"
                  onclick={reloadModelNow}
                  disabled={reloading}
                >
                  <RefreshCw class="h-4 w-4 {reloading ? 'animate-spin' : ''}" />
                  {reloading ? "Reloading" : "Reload now"}
                </button>
              </div>
            {/if}

            <label class="field">
              <span>Alias</span>
              <input
                bind:value={settings.alias}
                class="input"
                spellcheck="false"
                placeholder={modelId}
              />
              <span class="hint">
                Name reported by the upstream server (passed via <code>--alias</code>).
                Leave blank to keep the base value.
              </span>
            </label>

            <label class="field">
              <span>HF source</span>
              <input bind:value={settings.source} class="input" spellcheck="false" />
            </label>

            <label class="field">
              <span>Server args</span>
              <textarea bind:value={settings.serverArgs} class="textarea" rows="4" spellcheck="false"></textarea>
            </label>

            <div class="field">
              <div class="flex items-center justify-between gap-2">
                <label for="kvCacheArgs" class="field-label">KV cache args</label>
                <div class="flex items-center gap-1">
                  <button
                    type="button"
                    class="cursor-pointer rounded-[2px] border border-border bg-black px-2 py-1 font-mono text-[10px] font-bold uppercase tracking-widest text-txtsecondary transition-colors duration-150 hover:border-border-hover hover:text-white"
                    onclick={() => applyKvCacheQuant("q4_0")}
                    title="Set cache quant to q4_0"
                  >
                    Q4
                  </button>
                  <button
                    type="button"
                    class="cursor-pointer rounded-[2px] border border-border bg-black px-2 py-1 font-mono text-[10px] font-bold uppercase tracking-widest text-txtsecondary transition-colors duration-150 hover:border-border-hover hover:text-white"
                    onclick={() => applyKvCacheQuant("q8_0")}
                    title="Set cache quant to q8_0"
                  >
                    Q8
                  </button>
                </div>
              </div>
              <textarea
                id="kvCacheArgs"
                bind:value={settings.kvCacheArgs}
                class="textarea"
                rows="3"
                spellcheck="false"
                placeholder={`e.g. ${defaultKvCacheArgs}`}
              ></textarea>
            </div>

            <label class="field">
              <span>Sampling args</span>
              <textarea bind:value={settings.samplingArgs} class="textarea" rows="3" spellcheck="false"></textarea>
            </label>

            <div class="field">
              <div class="flex items-center justify-between gap-2">
                <label for="grammarArgs" class="field-label">Grammar args</label>
                <button
                  type="button"
                  class="cursor-pointer rounded-[2px] border border-border bg-black px-2 py-1 font-mono text-[10px] font-bold uppercase tracking-widest text-txtsecondary transition-colors duration-150 hover:border-border-hover hover:text-white"
                  onclick={() => (settings.grammarArgs = defaultGrammarArgs)}
                >
                  default
                </button>
              </div>
              <textarea
                id="grammarArgs"
                bind:value={settings.grammarArgs}
                class="textarea"
                rows="2"
                spellcheck="false"
                placeholder="e.g. --grammar-file /app/think.gbnf"
              ></textarea>
            </div>

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

  .field > span:first-of-type,
  .field-label {
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--color-txtsecondary);
  }

  .field .hint {
    font-family: var(--font-sans);
    font-size: 11px;
    font-weight: 400;
    letter-spacing: normal;
    text-transform: none;
    color: var(--color-txtsecondary);
    line-height: 1.5;
  }

  .field .hint code {
    font-family: var(--font-mono);
    font-size: 10px;
    background: #000000;
    border: 1px solid var(--color-border);
    border-radius: 2px;
    padding: 0 4px;
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
