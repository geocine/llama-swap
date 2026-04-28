<script lang="ts">
  import { renderMarkdown, escapeHtml, renderStreamingMarkdown, createStreamingCache } from "../../lib/markdown";
  import type { RenderedBlock } from "../../lib/markdown";
  import { Copy, Check, Pencil, X, Save, RefreshCw, ChevronDown, ChevronRight, Brain, Code, LoaderCircle } from "lucide-svelte";
  import { getTextContent, getImageUrls } from "../../lib/types";
  import type { ContentPart } from "../../lib/types";
  import { formatElapsed } from "../../lib/modelLoading";
  import type { ChatModelLoadingState } from "../../lib/modelLoading";

  interface Props {
    role: "user" | "assistant" | "system";
    content: string | ContentPart[];
    reasoning_content?: string;
    reasoningTimeMs?: number;
    isStreaming?: boolean;
    isReasoning?: boolean;
    loadingState?: ChatModelLoadingState | null;
    onEdit?: (newContent: string) => void;
    onRegenerate?: () => void;
  }

  let {
    role,
    content,
    reasoning_content = "",
    reasoningTimeMs = 0,
    isStreaming = false,
    isReasoning = false,
    loadingState = null,
    onEdit,
    onRegenerate,
  }: Props = $props();

  let textContent = $derived(getTextContent(content));
  let imageUrls = $derived(getImageUrls(content));
  let hasImages = $derived(imageUrls.length > 0);
  let canEdit = $derived(onEdit !== undefined && !hasImages);

  let streamingCache = createStreamingCache();
  let renderedParts = $derived.by(() => {
    if (role !== "assistant") {
      return { blocks: [{ id: -1, html: escapeHtml(textContent).replace(/\n/g, '<br>') }] as RenderedBlock[], pendingHtml: "" };
    }
    if (!isStreaming) {
      streamingCache = createStreamingCache();
      return { blocks: [{ id: -1, html: renderMarkdown(textContent) }] as RenderedBlock[], pendingHtml: "" };
    }
    return renderStreamingMarkdown(textContent, streamingCache);
  });
  let copied = $state(false);
  let showRaw = $state(false);
  let isEditing = $state(false);
  let editContent = $state("");
  let showReasoning = $state(false);
  let modalImageUrl = $state<string | null>(null);

  function formatDuration(ms: number): string {
    if (ms < 1000) {
      return `${ms.toFixed(0)}ms`;
    }
    return `${(ms / 1000).toFixed(1)}s`;
  }

  async function copyToClipboard() {
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(textContent);
      } else {
        // Fallback for non-secure contexts (HTTP)
        const textarea = document.createElement("textarea");
        textarea.value = textContent;
        textarea.style.position = "fixed";
        textarea.style.left = "-9999px";
        document.body.appendChild(textarea);
        textarea.select();
        document.execCommand("copy");
        document.body.removeChild(textarea);
      }
      copied = true;
      setTimeout(() => (copied = false), 2000);
    } catch (err) {
      console.error("Failed to copy:", err);
    }
  }

  function startEdit() {
    editContent = textContent;
    isEditing = true;
  }

  function cancelEdit() {
    isEditing = false;
    editContent = "";
  }

  function saveEdit() {
    if (onEdit && editContent.trim() !== textContent) {
      onEdit(editContent.trim());
    }
    isEditing = false;
    editContent = "";
  }

  function openModal(imageUrl: string) {
    modalImageUrl = imageUrl;
    document.body.style.overflow = "hidden";
  }

  function closeModal(event?: MouseEvent) {
    // Only close if clicking the background, not the image
    if (event && event.target !== event.currentTarget) {
      return;
    }
    modalImageUrl = null;
    document.body.style.overflow = "";
  }

  function handleModalKeyDown(event: KeyboardEvent) {
    if (event.key === "Escape") {
      closeModal();
    }
  }

  function handleKeyDown(event: KeyboardEvent) {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      saveEdit();
    } else if (event.key === "Escape") {
      cancelEdit();
    }
  }

  const COPY_SVG = `<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/></svg>`;
  const CHECK_SVG = `<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>`;

  function codeBlockCopy(node: HTMLElement) {
    function attachButtons() {
      node.querySelectorAll<HTMLPreElement>('pre:not([data-copy-btn])').forEach(pre => {
        pre.setAttribute('data-copy-btn', 'true');
        const btn = document.createElement('button');
        btn.className = 'code-copy-btn';
        btn.title = 'Copy code';
        btn.innerHTML = COPY_SVG;
        btn.addEventListener('click', async () => {
          const text = pre.querySelector('code')?.textContent ?? pre.textContent ?? '';
          try {
            if (navigator.clipboard && window.isSecureContext) {
              await navigator.clipboard.writeText(text);
            } else {
              const ta = document.createElement('textarea');
              ta.value = text;
              ta.style.cssText = 'position:fixed;left:-9999px';
              document.body.appendChild(ta);
              ta.select();
              document.execCommand('copy');
              document.body.removeChild(ta);
            }
            btn.innerHTML = CHECK_SVG;
            btn.classList.add('copied');
            setTimeout(() => { btn.innerHTML = COPY_SVG; btn.classList.remove('copied'); }, 2000);
          } catch (e) {
            console.error('copy failed', e);
          }
        });
        pre.appendChild(btn);
      });
    }
    attachButtons();
    const mo = new MutationObserver(attachButtons);
    mo.observe(node, { childList: true, subtree: true });
    return { destroy: () => mo.disconnect() };
  }
</script>

<div class="group flex flex-col {role === 'user' ? 'items-end' : 'items-start'} gap-3 py-6">
  {#if role === "assistant"}
    <!-- Role label -->
    <div class="flex items-center gap-2 font-mono text-[10px] uppercase tracking-widest text-txtsecondary">
      <span class="h-1 w-1 rounded-full {loadingState ? 'animate-pulse bg-warning' : 'bg-success'}"></span>
      Assistant
    </div>

    {#if loadingState}
      <div class="w-full overflow-hidden rounded-sm border border-border bg-surface">
        <div class="flex items-center gap-3 px-4 py-3">
          <LoaderCircle class="h-4 w-4 animate-spin text-white" />
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
              <span class="text-xs font-bold uppercase tracking-widest text-txtmain">
                {loadingState.label}
              </span>
              <span class="font-mono text-[11px] uppercase tracking-widest text-txtsecondary">
                {loadingState.modelId}
              </span>
            </div>
            <div class="mt-1 font-mono text-[11px] uppercase tracking-widest text-txtmuted">
              {loadingState.state} · {formatElapsed(loadingState.elapsedMs)}
            </div>
          </div>
        </div>
        <div class="h-1 overflow-hidden bg-zinc-800">
          <div class="loading-progress h-full w-1/2 bg-white"></div>
        </div>
      </div>
    {:else}
    <!-- Reasoning collapsible -->
    {#if reasoning_content || isReasoning}
      <div class="w-full overflow-hidden rounded-sm border border-border bg-surface">
        <button
          class="flex w-full items-center gap-2 px-3 py-2 text-sm transition-colors duration-150 hover:bg-secondary-hover"
          onclick={() => (showReasoning = !showReasoning)}
        >
          {#if showReasoning}
            <ChevronDown class="h-3.5 w-3.5 text-txtsecondary" />
          {:else}
            <ChevronRight class="h-3.5 w-3.5 text-txtsecondary" />
          {/if}
          <Brain class="h-3.5 w-3.5 text-txtsecondary" />
          <span class="text-[10px] font-bold uppercase tracking-widest text-txtmain">Reasoning</span>
          <span class="font-mono text-[11px] text-txtmuted">
            {reasoning_content.length} chars{#if !isReasoning && reasoningTimeMs > 0} · {formatDuration(reasoningTimeMs)}{/if}
          </span>
          {#if isReasoning}
            <span class="ml-auto flex items-center gap-1.5 font-mono text-[10px] uppercase tracking-widest text-txtsecondary">
              <span class="h-1.5 w-1.5 animate-pulse rounded-full bg-white"></span>
              thinking
            </span>
          {/if}
        </button>
        {#if showReasoning}
          <div class="whitespace-pre-wrap border-t border-border bg-surface-elevated px-4 py-3 font-mono text-[13px] leading-6 text-txtsecondary">
            {reasoning_content}{#if isReasoning}<span class="ml-0.5 inline-block h-4 w-1.5 animate-pulse bg-current"></span>{/if}
          </div>
        {/if}
      </div>
    {/if}

    {#if hasImages}
      <div class="flex flex-wrap gap-2">
        {#each imageUrls as imageUrl, idx (idx)}
          <button
            onclick={() => openModal(imageUrl)}
            class="cursor-pointer rounded-sm border border-border transition-opacity duration-150 hover:opacity-80"
          >
            <img
              src={imageUrl}
              alt="Image {idx + 1}"
              class="max-h-64 rounded-sm"
            />
          </button>
        {/each}
      </div>
    {/if}

    {#if showRaw}
      <div class="w-full whitespace-pre-wrap rounded-sm border border-border bg-surface-elevated p-3 font-mono text-sm text-txtmain">
        {textContent}
      </div>
    {:else}
      <div class="prose prose-sm prose-invert w-full max-w-none leading-7 text-txtmain" use:codeBlockCopy>
        {#each renderedParts.blocks as block (block.id)}
          {@html block.html}
        {/each}
        {@html renderedParts.pendingHtml}
        {#if isStreaming && !isReasoning}
          <span class="ml-0.5 inline-block h-4 w-2 animate-pulse bg-current"></span>
        {/if}
      </div>
    {/if}

    {#if !isStreaming}
      <div
        class="flex gap-1 text-txtsecondary opacity-0 transition-opacity duration-150 group-hover:opacity-100 focus-within:opacity-100"
      >
        {#if onRegenerate}
          <button
            class="rounded-sm p-1.5 text-txtsecondary transition-colors duration-150 hover:bg-secondary hover:text-white"
            onclick={onRegenerate}
            title="Regenerate response"
            aria-label="Regenerate response"
          >
            <RefreshCw class="h-3.5 w-3.5" />
          </button>
        {/if}
        <button
          class="rounded-sm p-1.5 text-txtsecondary transition-colors duration-150 hover:bg-secondary hover:text-white"
          onclick={copyToClipboard}
          title={copied ? "Copied!" : "Copy to clipboard"}
          aria-label="Copy to clipboard"
        >
          {#if copied}
            <Check class="h-3.5 w-3.5 text-success" />
          {:else}
            <Copy class="h-3.5 w-3.5" />
          {/if}
        </button>
        <button
          class="rounded-sm p-1.5 transition-colors duration-150 hover:bg-secondary hover:text-white {showRaw ? 'text-white' : 'text-txtsecondary'}"
          onclick={() => (showRaw = !showRaw)}
          title={showRaw ? "Show rendered" : "Show raw"}
          aria-label={showRaw ? "Show rendered" : "Show raw"}
        >
          <Code class="h-3.5 w-3.5" />
        </button>
      </div>
    {/if}
    {/if}
  {:else}
    <!-- User message -->
    {#if isEditing}
      <div class="flex w-full max-w-[85%] flex-col gap-2">
        <textarea
          class="w-full resize-none rounded-sm border border-border bg-black px-3 py-2 text-sm text-txtmain outline-none transition-colors duration-150 focus:border-white"
          rows="3"
          bind:value={editContent}
          onkeydown={handleKeyDown}
        ></textarea>
        <div class="flex justify-end gap-1">
          <button
            class="btn btn--sm flex items-center gap-1.5"
            onclick={cancelEdit}
            title="Cancel"
          >
            <X class="h-3.5 w-3.5" />
            Cancel
          </button>
          <button
            class="btn btn-primary btn--sm flex items-center gap-1.5"
            onclick={saveEdit}
            title="Save and regenerate"
          >
            <Save class="h-3.5 w-3.5" />
            Save
          </button>
        </div>
      </div>
    {:else}
      {#if hasImages}
        <div class="flex flex-wrap justify-end gap-2">
          {#each imageUrls as imageUrl, idx (idx)}
            <button
              onclick={() => openModal(imageUrl)}
              class="cursor-pointer rounded-sm border border-border transition-opacity duration-150 hover:opacity-80"
            >
              <img
                src={imageUrl}
                alt="Image {idx + 1}"
                class="max-h-48 max-w-[200px] rounded-sm"
              />
            </button>
          {/each}
        </div>
      {/if}
      {#if textContent.trim()}
        <div class="max-w-[80%]">
          <div class="whitespace-pre-wrap rounded-sm border border-border bg-secondary px-4 py-2.5 text-sm leading-6 text-txtmain">{textContent}</div>
        </div>
      {/if}
      <div
        class="flex gap-1 text-txtsecondary opacity-0 transition-opacity duration-150 group-hover:opacity-100 focus-within:opacity-100"
      >
        <button
          class="rounded-sm p-1.5 text-txtsecondary transition-colors duration-150 hover:bg-secondary hover:text-white"
          onclick={copyToClipboard}
          title={copied ? "Copied!" : "Copy to clipboard"}
          aria-label="Copy to clipboard"
        >
          {#if copied}
            <Check class="h-3.5 w-3.5 text-success" />
          {:else}
            <Copy class="h-3.5 w-3.5" />
          {/if}
        </button>
        {#if canEdit}
          <button
            class="rounded-sm p-1.5 text-txtsecondary transition-colors duration-150 hover:bg-secondary hover:text-white"
            onclick={startEdit}
            title="Edit message"
            aria-label="Edit message"
          >
            <Pencil class="h-3.5 w-3.5" />
          </button>
        {/if}
      </div>
    {/if}
  {/if}
</div>

<!-- Full-size image modal -->
{#if modalImageUrl}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/90 p-4"
    onclick={(e) => closeModal(e)}
    onkeydown={handleModalKeyDown}
    role="button"
    tabindex="-1"
  >
    <button
      class="absolute right-4 top-4 rounded-sm border border-border bg-surface p-2 text-txtsecondary transition-colors duration-150 hover:bg-secondary hover:text-white"
      onclick={() => closeModal()}
      title="Close"
      aria-label="Close"
    >
      <X class="h-5 w-5" />
    </button>
    <img
      src={modalImageUrl}
      alt=""
      class="pointer-events-none max-h-full max-w-full rounded-sm"
    />
  </div>
{/if}

<style>
  .prose :global(pre) {
    position: relative;
    background-color: #09090b;
    border: 1px solid #27272a;
    border-radius: 2px;
    padding: 0.75rem;
    padding-right: 2.5rem;
    overflow-x: auto;
    margin: 0.5rem 0;
  }

  .prose :global(.code-copy-btn) {
    position: absolute;
    top: 0.375rem;
    right: 0.375rem;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0.25rem;
    border-radius: 2px;
    border: 1px solid #3f3f46;
    background: #18181b;
    color: #71717a;
    cursor: pointer;
    transition: all 150ms ease;
    line-height: 0;
  }

  .prose :global(.code-copy-btn:hover) {
    background: #27272a;
    color: #e4e4e7;
  }

  .prose :global(.code-copy-btn.copied) {
    color: #22c55e;
    opacity: 1;
  }

  .prose :global(code) {
    font-family: 'JetBrains Mono', 'Fira Code', 'Consolas', monospace;
    font-size: 0.875em;
  }

  .prose :global(pre code) {
    background: none;
    padding: 0;
  }

  .prose :global(code:not(pre code)) {
    background-color: #09090b;
    padding: 0.125rem 0.25rem;
    border-radius: 2px;
    border: 1px solid #27272a;
  }

  .prose :global(p) {
    margin: 0.5rem 0;
  }

  .prose :global(p:first-child) {
    margin-top: 0;
  }

  .prose :global(p:last-child) {
    margin-bottom: 0;
  }

  .prose :global(ul),
  .prose :global(ol) {
    margin: 0.5rem 0;
    padding-left: 1.5rem;
  }

  .prose :global(li) {
    margin: 0.25rem 0;
  }

  .prose :global(h1),
  .prose :global(h2),
  .prose :global(h3),
  .prose :global(h4) {
    margin: 1rem 0 0.5rem 0;
    font-weight: 600;
  }

  .prose :global(h1:first-child),
  .prose :global(h2:first-child),
  .prose :global(h3:first-child),
  .prose :global(h4:first-child) {
    margin-top: 0;
  }

  .prose :global(blockquote) {
    border-left: 3px solid #3f3f46;
    padding-left: 1rem;
    margin: 0.5rem 0;
    font-style: italic;
    color: #a1a1aa;
  }

  .prose :global(a) {
    color: #a1a1aa;
    text-decoration: underline;
  }

  .prose :global(a:hover) {
    color: #e4e4e7;
  }

  .prose :global(table) {
    width: 100%;
    border-collapse: collapse;
    margin: 0.5rem 0;
  }

  .prose :global(th),
  .prose :global(td) {
    border: 1px solid #27272a;
    padding: 0.5rem;
    text-align: left;
  }

  .prose :global(th) {
    background-color: #18181b;
    font-weight: 600;
  }

  .prose :global(.hljs) {
    background: transparent;
  }

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
