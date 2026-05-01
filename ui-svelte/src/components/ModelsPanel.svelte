<script lang="ts">
  import { models, downloadProgress, loadModel, unloadAllModels, unloadSingleModel } from "../stores/api";
  import { isNarrow } from "../stores/theme";
  import { persistentStore } from "../stores/persistent";
  import type { Model } from "../lib/types";
  import ModelConfigDialog from "./ModelConfigDialog.svelte";
  import ConfigImportExport from "./config/ConfigImportExport.svelte";
  import { LoaderCircle, Settings } from "lucide-svelte";

  let isUnloading = $state(false);
  let menuOpen = $state(false);
  let configModelId = $state("");
  let configOpen = $state(false);
  let configStatus = $state<{ message: string; kind: "info" | "error" } | null>(null);
  let configStatusTimer: ReturnType<typeof setTimeout> | null = null;

  function setConfigStatus(message: string, kind: "info" | "error") {
    if (configStatusTimer) clearTimeout(configStatusTimer);
    configStatus = { message, kind };
    configStatusTimer = setTimeout(() => {
      configStatus = null;
      configStatusTimer = null;
    }, 5000);
  }

  const showUnlistedStore = persistentStore<boolean>("showUnlisted", true);
  const showIdorNameStore = persistentStore<"id" | "name">("showIdorName", "id");

  let filteredModels = $derived.by(() => {
    const filtered = $models.filter((model) => $showUnlistedStore || !model.unlisted);
    const peerModels = filtered.filter((m) => m.peerID);

    // Group peer models by peerID
    const grouped = peerModels.reduce(
      (acc, model) => {
        const peerId = model.peerID || "unknown";
        if (!acc[peerId]) acc[peerId] = [];
        acc[peerId].push(model);
        return acc;
      },
      {} as Record<string, Model[]>
    );

    return {
      regularModels: filtered.filter((m) => !m.peerID),
      peerModelsByPeerId: grouped,
    };
  });

  // Identify a model that is currently transitioning so we can lock LOAD on
  // every other row until it has either finished starting or fully stopped.
  let transitioningModel = $derived(
    $models.find((m) => m.state === "starting" || m.state === "stopping") ?? null
  );
  let activeDownloadProgress = $derived($downloadProgress?.active ? $downloadProgress : null);
  let activeDownloadPercent = $derived(
    typeof activeDownloadProgress?.percent === "number"
      ? Math.min(100, Math.max(0, activeDownloadProgress.percent))
      : null
  );

  async function handleUnloadAllModels(): Promise<void> {
    isUnloading = true;
    try {
      await unloadAllModels();
    } catch (e) {
      console.error(e);
    } finally {
      setTimeout(() => (isUnloading = false), 1000);
    }
  }

  function toggleIdorName(): void {
    showIdorNameStore.update((prev) => (prev === "name" ? "id" : "name"));
  }

  function toggleShowUnlisted(): void {
    showUnlistedStore.update((prev) => !prev);
  }

  function getModelDisplay(model: Model): string {
    return $showIdorNameStore === "id" ? model.id : (model.name || model.id);
  }

  function canUnloadModel(model: Model): boolean {
    return model.state === "ready" || model.state === "starting";
  }

  function unloadLabel(model: Model): string {
    return model.state === "starting" ? "Stop" : "Unload";
  }

  function showDownloadProgress(model: Model): boolean {
    return model.state === "starting" && activeDownloadProgress !== null;
  }

  function openConfig(modelId: string): void {
    configModelId = modelId;
    configOpen = true;
  }
</script>

<div class="card h-full flex flex-col">
  <div class="shrink-0">
    {#if $isNarrow}
      <div class="flex justify-end items-baseline">
        <div class="relative">
          <button class="btn flex items-center gap-2 py-1" onclick={() => (menuOpen = !menuOpen)} aria-label="Toggle menu">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" class="w-4 h-4">
              <path fill-rule="evenodd" d="M3 6.75A.75.75 0 0 1 3.75 6h16.5a.75.75 0 0 1 0 1.5H3.75A.75.75 0 0 1 3 6.75ZM3 12a.75.75 0 0 1 .75-.75h16.5a.75.75 0 0 1 0 1.5H3.75A.75.75 0 0 1 3 12Zm0 5.25a.75.75 0 0 1 .75-.75h16.5a.75.75 0 0 1 0 1.5H3.75a.75.75 0 0 1-.75-.75Z" clip-rule="evenodd" />
            </svg>
          </button>
          {#if menuOpen}
            <div class="absolute right-0 mt-2 w-48 bg-surface border border-border rounded-sm shadow-2xl z-20">
              <button
                class="w-full text-left px-4 py-2 text-sm text-txtsecondary hover:text-white hover:bg-secondary-hover flex items-center gap-2 transition-colors duration-150"
                onclick={() => { toggleIdorName(); menuOpen = false; }}
              >
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" class="w-4 h-4">
                  <path fill-rule="evenodd" d="M15.97 2.47a.75.75 0 0 1 1.06 0l4.5 4.5a.75.75 0 0 1 0 1.06l-4.5 4.5a.75.75 0 1 1-1.06-1.06l3.22-3.22H7.5a.75.75 0 0 1 0-1.5h11.69l-3.22-3.22a.75.75 0 0 1 0-1.06Zm-7.94 9a.75.75 0 0 1 0 1.06l-3.22 3.22H16.5a.75.75 0 0 1 0 1.5H4.81l3.22 3.22a.75.75 0 1 1-1.06 1.06l-4.5-4.5a.75.75 0 0 1 0-1.06l4.5-4.5a.75.75 0 0 1 1.06 0Z" clip-rule="evenodd" />
                </svg>
                {$showIdorNameStore === "id" ? "Show Name" : "Show ID"}
              </button>
              <button
                class="w-full text-left px-4 py-2 text-sm text-txtsecondary hover:text-white hover:bg-secondary-hover flex items-center gap-2 transition-colors duration-150"
                onclick={() => { toggleShowUnlisted(); menuOpen = false; }}
              >
                {#if $showUnlistedStore}
                  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" class="w-4 h-4">
                    <path d="M3.53 2.47a.75.75 0 0 0-1.06 1.06l18 18a.75.75 0 1 0 1.06-1.06l-18-18ZM22.676 12.553a11.249 11.249 0 0 1-2.631 4.31l-3.099-3.099a5.25 5.25 0 0 0-6.71-6.71L7.759 4.577a11.217 11.217 0 0 1 4.242-.827c4.97 0 9.185 3.223 10.675 7.69.12.362.12.752 0 1.113Z" />
                    <path d="M15.75 12c0 .18-.013.357-.037.53l-4.244-4.243A3.75 3.75 0 0 1 15.75 12ZM12.53 15.713l-4.243-4.244a3.75 3.75 0 0 0 4.244 4.243Z" />
                    <path d="M6.75 12c0-.619.107-1.213.304-1.764l-3.1-3.1a11.25 11.25 0 0 0-2.63 4.31c-.12.362-.12.752 0 1.114 1.489 4.467 5.704 7.69 10.675 7.69 1.5 0 2.933-.294 4.242-.827l-2.477-2.477A5.25 5.25 0 0 1 6.75 12Z" />
                  </svg>
                {:else}
                  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" class="w-4 h-4">
                    <path d="M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z" />
                    <path fill-rule="evenodd" d="M1.323 11.447C2.811 6.976 7.028 3.75 12.001 3.75c4.97 0 9.185 3.223 10.675 7.69.12.362.12.752 0 1.113-1.487 4.471-5.705 7.697-10.677 7.697-4.97 0-9.186-3.223-10.675-7.69a1.762 1.762 0 0 1 0-1.113ZM17.25 12a5.25 5.25 0 1 1-10.5 0 5.25 5.25 0 0 1 10.5 0Z" clip-rule="evenodd" />
                  </svg>
                {/if}
                {$showUnlistedStore ? "Hide Unlisted" : "Show Unlisted"}
              </button>
              <button
                class="w-full text-left px-4 py-2 text-sm text-txtsecondary hover:text-white hover:bg-secondary-hover flex items-center gap-2 transition-colors duration-150"
                onclick={() => { handleUnloadAllModels(); menuOpen = false; }}
                disabled={isUnloading}
              >
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" class="w-5 h-5">
                  <path fill-rule="evenodd" d="M12 2.25c-5.385 0-9.75 4.365-9.75 9.75s4.365 9.75 9.75 9.75 9.75-4.365 9.75-9.75S17.385 2.25 12 2.25Zm.53 5.47a.75.75 0 0 0-1.06 0l-3 3a.75.75 0 1 0 1.06 1.06l1.72-1.72v5.69a.75.75 0 0 0 1.5 0v-5.69l1.72 1.72a.75.75 0 1 0 1.06-1.06l-3-3Z" clip-rule="evenodd" />
                </svg>
                {isUnloading ? "Unloading..." : "Unload All"}
              </button>
              <div class="flex items-center justify-around gap-1 border-t border-border px-2 py-2">
                <ConfigImportExport
                  buttonClass="btn flex-1 flex items-center justify-center gap-2 py-1.5"
                  onstatus={setConfigStatus}
                />
              </div>
            </div>
          {/if}
        </div>
      </div>
    {/if}
    {#if !$isNarrow}
      <div class="flex justify-between">
        <div class="flex gap-2">
          <button class="btn flex items-center gap-2" onclick={toggleIdorName} style="line-height: 1.2">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" class="w-4 h-4">
              <path fill-rule="evenodd" d="M15.97 2.47a.75.75 0 0 1 1.06 0l4.5 4.5a.75.75 0 0 1 0 1.06l-4.5 4.5a.75.75 0 1 1-1.06-1.06l3.22-3.22H7.5a.75.75 0 0 1 0-1.5h11.69l-3.22-3.22a.75.75 0 0 1 0-1.06Zm-7.94 9a.75.75 0 0 1 0 1.06l-3.22 3.22H16.5a.75.75 0 0 1 0 1.5H4.81l3.22 3.22a.75.75 0 1 1-1.06 1.06l-4.5-4.5a.75.75 0 0 1 0-1.06l4.5-4.5a.75.75 0 0 1 1.06 0Z" clip-rule="evenodd" />
            </svg>
            {$showIdorNameStore === "id" ? "ID" : "Name"}
          </button>

          <button class="btn flex items-center gap-2" onclick={toggleShowUnlisted} style="line-height: 1.2">
            {#if $showUnlistedStore}
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" class="w-4 h-4">
                <path d="M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z" />
                <path fill-rule="evenodd" d="M1.323 11.447C2.811 6.976 7.028 3.75 12.001 3.75c4.97 0 9.185 3.223 10.675 7.69.12.362.12.752 0 1.113-1.487 4.471-5.705 7.697-10.677 7.697-4.97 0-9.186-3.223-10.675-7.69a1.762 1.762 0 0 1 0-1.113ZM17.25 12a5.25 5.25 0 1 1-10.5 0 5.25 5.25 0 0 1 10.5 0Z" clip-rule="evenodd" />
              </svg>
            {:else}
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" class="w-4 h-4">
                <path d="M3.53 2.47a.75.75 0 0 0-1.06 1.06l18 18a.75.75 0 1 0 1.06-1.06l-18-18ZM22.676 12.553a11.249 11.249 0 0 1-2.631 4.31l-3.099-3.099a5.25 5.25 0 0 0-6.71-6.71L7.759 4.577a11.217 11.217 0 0 1 4.242-.827c4.97 0 9.185 3.223 10.675 7.69.12.362.12.752 0 1.113Z" />
                <path d="M15.75 12c0 .18-.013.357-.037.53l-4.244-4.243A3.75 3.75 0 0 1 15.75 12ZM12.53 15.713l-4.243-4.244a3.75 3.75 0 0 0 4.244 4.243Z" />
                <path d="M6.75 12c0-.619.107-1.213.304-1.764l-3.1-3.1a11.25 11.25 0 0 0-2.63 4.31c-.12.362-.12.752 0 1.114 1.489 4.467 5.704 7.69 10.675 7.69 1.5 0 2.933-.294 4.242-.827l-2.477-2.477A5.25 5.25 0 0 1 6.75 12Z" />
              </svg>
            {/if}
            unlisted
          </button>
        </div>
        <div class="flex items-center gap-2">
          <ConfigImportExport
            buttonClass="btn p-2"
            onstatus={setConfigStatus}
          />
          <button class="btn flex items-center gap-2" onclick={handleUnloadAllModels} disabled={isUnloading}>
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" class="w-5 h-5">
              <path fill-rule="evenodd" d="M12 2.25c-5.385 0-9.75 4.365-9.75 9.75s4.365 9.75 9.75 9.75 9.75-4.365 9.75-9.75S17.385 2.25 12 2.25Zm.53 5.47a.75.75 0 0 0-1.06 0l-3 3a.75.75 0 1 0 1.06 1.06l1.72-1.72v5.69a.75.75 0 0 0 1.5 0v-5.69l1.72 1.72a.75.75 0 1 0 1.06-1.06l-3-3Z" clip-rule="evenodd" />
            </svg>
            {isUnloading ? "Unloading..." : "Unload All"}
          </button>
        </div>
      </div>
    {/if}
  </div>

  {#if configStatus}
    <div
      class="mt-2 shrink-0 rounded-sm border px-3 py-2 text-xs {configStatus.kind === 'error'
        ? 'border-error/40 bg-error/10 text-error'
        : 'border-border bg-surface text-txtsecondary'}"
    >
      {configStatus.message}
    </div>
  {/if}

  <div class="flex-1 overflow-y-auto">
    <table class="w-full table-fixed">
      <thead class="sticky top-0 z-10">
        <tr class="border-b border-border bg-surface">
          <th class="text-left">{$showIdorNameStore === "id" ? "Model ID" : "Name"}</th>
          <th class="w-56 text-right pr-2">State</th>
        </tr>
      </thead>
      <tbody>
        {#each filteredModels.regularModels as model (model.id)}
          <tr class="border-b border-border hover:bg-secondary transition-colors duration-150">
            <td class={model.unlisted ? "text-txtsecondary" : ""}>
              <a href="/upstream/{model.id}/" class="font-semibold text-txtmain hover:text-white transition-colors duration-150" target="_blank">
                {getModelDisplay(model)}
              </a>
              {#if model.description}
                <p class="text-txtsecondary text-xs"><em>{model.description}</em></p>
              {/if}
              {#if model.aliases && model.aliases.length > 0}
                <p class="text-xs text-txtsecondary font-mono">Aliases: {model.aliases.join(", ")}</p>
              {/if}
              {#if showDownloadProgress(model)}
                <div class="mt-2 max-w-xl rounded-sm border border-border bg-black/40 px-2.5 py-2">
                  <div class="mb-1 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[10px] uppercase tracking-wider">
                    <span class="font-bold text-txtmain">{activeDownloadProgress?.message}</span>
                    <span class="text-txtsecondary">
                      {activeDownloadProgress?.downloadedBytes ?? "0 B"}{#if activeDownloadProgress?.totalBytes} / {activeDownloadProgress.totalBytes}{/if}
                    </span>
                    {#if activeDownloadPercent !== null}
                      <span class="text-txtmain">{activeDownloadPercent.toFixed(1)}%</span>
                    {/if}
                  </div>
                  <div class="h-1 overflow-hidden rounded-full bg-zinc-800">
                    {#if activeDownloadPercent !== null}
                      <div class="h-full bg-white transition-[width] duration-300" style={`width: ${activeDownloadPercent}%`}></div>
                    {:else}
                      <div class="loading-progress h-full w-1/2 bg-white"></div>
                    {/if}
                  </div>
                  {#if activeDownloadProgress?.filename}
                    <div class="mt-1 truncate font-mono text-[10px] text-txtmuted" title={activeDownloadProgress.filename}>
                      {activeDownloadProgress.filename}
                    </div>
                  {/if}
                </div>
              {/if}
            </td>
            <td class="w-56">
              <!-- Action + state are twin chips: identical height, text size, and gap on every row -->
              <div class="flex items-center justify-end gap-2">
                <button
                  class="status w-8 border-border text-txtsecondary transition-colors duration-150 hover:border-border-hover hover:text-white cursor-pointer"
                  onclick={() => openConfig(model.id)}
                  title="Configure model"
                  aria-label={`Configure ${model.id}`}
                >
                  <Settings class="h-3.5 w-3.5" />
                </button>
                {#if model.state === "starting"}
                  <button
                    class="status w-16 border-warning/40 text-warning transition-colors duration-150 hover:border-warning hover:text-warning cursor-pointer"
                    onclick={() => unloadSingleModel(model.id)}
                  >
                    Stop
                  </button>
                {:else if model.state === "stopping"}
                  <span class="status w-16 border-warning/40 text-warning" aria-label="Stopping">
                    <LoaderCircle class="h-3 w-3 animate-spin" />
                  </span>
                {:else if model.state === "stopped"}
                  <button
                    class="status w-16 border-border text-txtmain transition-colors duration-150 hover:border-border-hover hover:text-white cursor-pointer disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:border-border disabled:hover:text-txtmain"
                    onclick={() => loadModel(model.id)}
                    disabled={transitioningModel !== null && transitioningModel.id !== model.id}
                    title={transitioningModel !== null && transitioningModel.id !== model.id
                      ? `Waiting for ${transitioningModel.id} to ${transitioningModel.state === "starting" ? "finish loading" : "finish stopping"}`
                      : "Load model"}
                  >
                    Load
                  </button>
                {:else}
                  <button
                    class="status w-16 border-border text-txtmain transition-colors duration-150 hover:border-border-hover hover:text-white cursor-pointer disabled:cursor-not-allowed disabled:opacity-40"
                    onclick={() => unloadSingleModel(model.id)}
                    disabled={!canUnloadModel(model)}
                  >
                    {unloadLabel(model)}
                  </button>
                {/if}
                <span class="status status--{model.state} w-16">{model.state}</span>
              </div>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>

    {#if Object.keys(filteredModels.peerModelsByPeerId).length > 0}
      <h3 class="mt-8 mb-2 text-xs font-bold uppercase tracking-wide text-txtsecondary">Peer Models</h3>
      {#each Object.entries(filteredModels.peerModelsByPeerId).sort(([a], [b]) => a.localeCompare(b)) as [peerId, peerModels] (peerId)}
        <div class="mb-4">
          <table class="w-full">
            <thead class="sticky top-0 z-10">
              <tr class="text-left border-b border-border bg-surface">
                <th class="font-semibold">{peerId}</th>
              </tr>
            </thead>
            <tbody>
              {#each peerModels as model (model.id)}
                <tr class="border-b border-border hover:bg-secondary transition-colors duration-150">
                  <td class="pl-8 {model.unlisted ? 'text-txtsecondary' : ''}">
                    <span class="font-mono text-sm">{model.id}</span>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/each}
    {/if}
  </div>
</div>

<ModelConfigDialog
  open={configOpen}
  modelId={configModelId}
  onClose={() => (configOpen = false)}
/>

<style>
  .loading-progress {
    animation: loading-progress 1.4s ease-in-out infinite;
  }

  @keyframes loading-progress {
    0% {
      transform: translateX(-100%);
    }
    100% {
      transform: translateX(200%);
    }
  }
</style>
