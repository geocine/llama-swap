<script lang="ts">
  import {
    downloadProgress,
    models,
    unloadSingleModel,
  } from "../../stores/api";
  import { persistentStore } from "../../stores/persistent";
  import { completeChatCompletion, streamChatCompletion } from "../../lib/chatApi";
  import {
    buildCompactionRequestMessages,
    insertCompactSummary,
    messagesForNextTurn,
    planConversationCompaction,
  } from "../../lib/compaction";
  import { getChatModelLoadingState, resolveSelectedModel } from "../../lib/modelLoading";
  import { playgroundStores } from "../../stores/playgroundActivity";
  import {
    activeConversation,
    chatsReady,
    conversations,
    currentChatId,
    ensureActiveConversation,
    newConversation,
    setConversationPersistencePaused,
    setMessages as setActiveMessages,
  } from "../../stores/chats";
  import { getTextContent } from "../../lib/types";
  import type { ChatMessage, ChatMessagePromptProgress, ChatMessageTimings, ContentPart } from "../../lib/types";
  import ChatMessageComponent from "./ChatMessage.svelte";
  import ModelConfigDialog from "../ModelConfigDialog.svelte";
  import ConfigImportExport from "../config/ConfigImportExport.svelte";
  import ModelSelector from "./ModelSelector.svelte";
  import {
    Archive,
    PanelLeftOpen,
    Plus,
    Send,
    Settings,
    SlidersHorizontal,
    X,
  } from "lucide-svelte";
  import { get } from "svelte/store";
  import { sidebarOpen } from "../../stores/playgroundUI";

  type AttachedImage = {
    url: string;
    name: string;
    size: number;
  };

  const selectedModelStore = persistentStore<string>("playground-selected-model", "");
  const systemPromptStore = persistentStore<string>("playground-system-prompt", "");

  // Ensure there is always an active conversation as soon as this component mounts
  $effect(() => {
    if (!$chatsReady) return;
    ensureActiveConversation();
  });

  // Messages live in the chats store; derive from the active conversation
  let messages = $derived($activeConversation?.messages ?? []);

  // Helper to update a specific conversation's messages — reads the freshest
  // store value via get() so back-to-back synchronous updates during streaming
  // never operate on stale state. Defaults to the active conversation but
  // accepts an explicit id, so streams continue writing to their original
  // conversation even if the user switches chats mid-stream.
  function updateMessages(
    updater: (current: ChatMessage[]) => ChatMessage[],
    targetId?: string
  ): void {
    const id = targetId ?? get(currentChatId) ?? ensureActiveConversation();
    const current = get(conversations).find((c) => c.id === id)?.messages ?? [];
    setActiveMessages(id, updater(current));
  }

  function buildRequestMessages(history: ChatMessage[]): ChatMessage[] {
    const systemParts: string[] = [];
    const systemPrompt = $systemPromptStore.trim();
    if (systemPrompt) {
      systemParts.push(systemPrompt);
    }

    const nonSystemMessages: ChatMessage[] = [];
    for (const message of history) {
      if (message.role === "system") {
        const text = getTextContent(message.content).trim();
        if (text) {
          systemParts.push(text);
        }
      } else {
        nonSystemMessages.push(message);
      }
    }

    if (systemParts.length === 0) {
      return nonSystemMessages;
    }

    return [{ role: "system", content: systemParts.join("\n\n") }, ...nonSystemMessages];
  }
  let userInput = $state("");
  let isStreaming = $state(false);
  let isReasoning = $state(false);
  let reasoningStartTime = $state<number>(0);
  let abortController = $state<AbortController | null>(null);
  let scrollContainer: HTMLDivElement | undefined = $state();
  let textareaEl: HTMLTextAreaElement | undefined = $state();
  let composerWrap: HTMLDivElement | undefined = $state();
  let showSettings = $state(false);
  let attachedImages = $state<AttachedImage[]>([]);
  let fileInput = $state<HTMLInputElement | null>(null);
  let imageError = $state<string | null>(null);
  let requestStartedAt = $state(0);
  let hasReceivedOutput = $state(false);
  let now = $state(Date.now());
  let isCompacting = $state(false);
  let compactError = $state<string | null>(null);
  let showModelConfig = $state(false);
  let configMessage = $state<string | null>(null);

  let hasModels = $derived($models.some((m) => !m.unlisted));
  let userScrolledUp = $state(false);
  let isEmpty = $derived(messages.length === 0);
  let modelLoadingState = $derived(
    getChatModelLoadingState(
      $models,
      $selectedModelStore,
      isStreaming,
      hasReceivedOutput,
      requestStartedAt,
      now,
      $downloadProgress
    )
  );
  let selectedModelInfo = $derived(resolveSelectedModel($models, $selectedModelStore));
  let contextTotal = $derived(selectedModelInfo?.contextSize ?? 0);
  let latestTimings = $derived.by(() => {
    for (let i = messages.length - 1; i >= 0; i -= 1) {
      const message = messages[i];
      if (message.role === "assistant" && message.timings) {
        return message.timings;
      }
    }
    return null;
  });
  let latestPromptProgress = $derived.by(() => {
    for (let i = messages.length - 1; i >= 0; i -= 1) {
      const message = messages[i];
      if (message.role === "assistant" && message.promptProgress) {
        return message.promptProgress;
      }
    }
    return null;
  });
  let contextUsed = $derived(calculateContextUsed(latestTimings, latestPromptProgress));
  let contextAvailable = $derived(Math.max(0, contextTotal - contextUsed));
  let contextPercent = $derived(
    contextTotal > 0 ? Math.min(100, Math.round((contextUsed / contextTotal) * 100)) : 0
  );
  let outputUsed = $derived(latestTimings?.predicted_n ?? 0);
  let liveTokensPerSecond = $derived(calculateLiveTokensPerSecond(latestTimings, latestPromptProgress));
  let canCompact = $derived(
    !isStreaming && !isCompacting && !!$selectedModelStore && messages.length > 7 && planConversationCompaction(messages, contextTotal) !== null
  );

  $effect(() => {
    playgroundStores.chatStreaming.set(isStreaming);
  });

  $effect(() => {
    if (!isStreaming) return;
    now = Date.now();
    const timer = setInterval(() => {
      now = Date.now();
    }, 1000);
    return () => clearInterval(timer);
  });

  // Return focus to the composer once the response (or compaction) finishes.
  // While the textarea is disabled, focus drops to `document.body`, which makes
  // the input feel "stuck" until the user clicks back. We only steal focus when
  // nothing else is focused, so manual interactions elsewhere are respected.
  let lastInputBusy = false;
  $effect(() => {
    const busy = isStreaming || isCompacting;
    if (lastInputBusy && !busy) {
      requestAnimationFrame(() => {
        if (!textareaEl || textareaEl.disabled) return;
        const active = document.activeElement;
        if (active && active !== document.body && active !== textareaEl) return;
        textareaEl.focus();
      });
    }
    lastInputBusy = busy;
  });

  function handleScroll() {
    if (!scrollContainer) return;
    const { scrollTop, scrollHeight, clientHeight } = scrollContainer;
    // Consider "at bottom" if within 40px of the bottom
    userScrolledUp = scrollHeight - scrollTop - clientHeight > 40;
  }

  // Auto-scroll when messages change — skip if user scrolled up
  $effect(() => {
    if (messages.length > 0 && scrollContainer && !userScrolledUp) {
      scrollContainer.scrollTo({
        top: scrollContainer.scrollHeight,
        behavior: isStreaming ? "instant" : "smooth",
      });
    }
  });

  // Auto-grow textarea (capped — overflow scrolls inside the textarea)
  const TEXTAREA_MIN_PX = 48;
  const TEXTAREA_MAX_PX = 200;
  $effect(() => {
    void userInput;
    if (textareaEl) {
      textareaEl.style.height = "auto";
      const next = Math.max(TEXTAREA_MIN_PX, Math.min(textareaEl.scrollHeight, TEXTAREA_MAX_PX));
      textareaEl.style.height = `${next}px`;
      textareaEl.style.overflowY = textareaEl.scrollHeight > TEXTAREA_MAX_PX ? "auto" : "hidden";
    }
  });

  // Keep the message bottom padding in sync with the composer's actual height
  // so the last message is never occluded as the textarea grows or images are added
  $effect(() => {
    if (!composerWrap || !scrollContainer) return;
    const target = scrollContainer;
    const update = () => {
      if (!composerWrap) return;
      target.style.setProperty("--composer-height", `${composerWrap.offsetHeight}px`);
    };
    update();
    requestAnimationFrame(update);
    const ro = new ResizeObserver(update);
    ro.observe(composerWrap);
    return () => ro.disconnect();
  });

  // Persistence is handled inside the chats store (throttled).

  async function sendMessage() {
    const trimmedInput = userInput.trim();
    if (!$chatsReady || (!trimmedInput && attachedImages.length === 0) || !$selectedModelStore || isStreaming || isCompacting) return;

    userScrolledUp = false;

    // Build message content (multimodal if images attached)
    let content: string | ContentPart[];
    if (attachedImages.length > 0) {
      const parts: ContentPart[] = [];
      if (trimmedInput) {
        parts.push({ type: "text", text: trimmedInput });
      }
      for (const image of attachedImages) {
        parts.push({ type: "image_url", image_url: { url: image.url } });
      }
      content = parts;
    } else {
      content = trimmedInput;
    }

    // Add user message
    updateMessages((curr) => [...curr, { role: "user", content }]);
    userInput = "";
    attachedImages = [];
    imageError = null;

    // Generate response from the new user message
    await regenerateFromIndex(messages.length - 1);
  }

  function cancelStreaming() {
    abortController?.abort();
    if (modelLoadingState?.state === "starting" || modelLoadingState?.state === "stopped") {
      void unloadSingleModel(modelLoadingState.modelId).catch((error) => {
        console.error("Failed to stop loading model:", error);
      });
    }
  }

  function newChat() {
    if (!$chatsReady) return;
    if (isStreaming) {
      cancelStreaming();
    }
    // Always create a fresh conversation rather than clearing the active one,
    // so previous chats stay in the sidebar history.
    newConversation();
    isReasoning = false;
    reasoningStartTime = 0;
  }

  async function regenerateFromIndex(idx: number) {
    // Snapshot the conversation id so streaming writes stay anchored even if
    // the user switches chats while the request is in flight.
    const streamChatId = get(currentChatId) ?? ensureActiveConversation();

    // Trim everything after the target message and append an empty assistant slot
    updateMessages((curr) => [...curr.slice(0, idx + 1), { role: "assistant", content: "" }], streamChatId);

    isStreaming = true;
    setConversationPersistencePaused(true);
    isReasoning = false;
    reasoningStartTime = 0;
    requestStartedAt = Date.now();
    now = requestStartedAt;
    hasReceivedOutput = false;
    abortController = new AbortController();

    let pendingContent = "";
    let pendingReasoning = "";
    let pendingReasoningTimeMs: number | undefined;
    let pendingPromptProgress: ChatMessagePromptProgress | undefined;
    let pendingTimings: ChatMessageTimings | undefined;
    let flushTimer: ReturnType<typeof setTimeout> | undefined;

    const flushStreamUpdate = () => {
      if (flushTimer) {
        clearTimeout(flushTimer);
        flushTimer = undefined;
      }
      if (
        !pendingContent &&
        !pendingReasoning &&
        pendingReasoningTimeMs === undefined &&
        !pendingPromptProgress &&
        !pendingTimings
      ) {
        return;
      }

      const contentDelta = pendingContent;
      const reasoningDelta = pendingReasoning;
      const reasoningTimeMs = pendingReasoningTimeMs;
      const promptProgress = pendingPromptProgress;
      const timings = pendingTimings;

      pendingContent = "";
      pendingReasoning = "";
      pendingReasoningTimeMs = undefined;
      pendingPromptProgress = undefined;
      pendingTimings = undefined;

      updateMessages(
        (curr) =>
          curr.map((msg, i) => {
            if (i !== curr.length - 1) return msg;

            const next: ChatMessage = { ...msg };
            if (reasoningDelta) {
              next.reasoning_content = (next.reasoning_content || "") + reasoningDelta;
            }
            if (contentDelta) {
              const currentContent = typeof next.content === "string" ? next.content : getTextContent(next.content);
              next.content = currentContent + contentDelta;
            }
            if (reasoningTimeMs !== undefined) {
              next.reasoningTimeMs = reasoningTimeMs;
            }
            if (promptProgress) {
              next.promptProgress = promptProgress;
            }
            if (timings) {
              next.timings = { ...(next.timings || {}), ...timings };
            }
            return next;
          }),
        streamChatId
      );
    };

    const scheduleStreamFlush = () => {
      if (flushTimer) return;
      flushTimer = setTimeout(flushStreamUpdate, 100);
    };

    try {
      const startMessages =
        get(conversations).find((c) => c.id === streamChatId)?.messages ?? [];
      const apiMessages = buildRequestMessages(messagesForNextTurn(startMessages).slice(0, -1));

      const stream = streamChatCompletion(
        $selectedModelStore,
        apiMessages,
        abortController.signal,
      );

      for await (const chunk of stream) {
        if (chunk.done) break;

        if (chunk.reasoning_content) {
          hasReceivedOutput = true;
          if (!isReasoning) {
            isReasoning = true;
            reasoningStartTime = Date.now();
          }
          pendingReasoning += chunk.reasoning_content;
        }

        if (chunk.content) {
          hasReceivedOutput = true;
          if (isReasoning) {
            pendingReasoningTimeMs = Date.now() - reasoningStartTime;
            isReasoning = false;
          }
          pendingContent += chunk.content;
        }

        if (chunk.prompt_progress) {
          pendingPromptProgress = chunk.prompt_progress;
        }

        if (chunk.timings) {
          pendingTimings = { ...(pendingTimings || {}), ...chunk.timings };
        }

        scheduleStreamFlush();
      }
      flushStreamUpdate();
    } catch (error) {
      if (error instanceof Error && error.name === "AbortError") {
        // User cancelled, keep partial response
        // If we were still reasoning, record the time
        if (isReasoning && reasoningStartTime > 0) {
          pendingReasoningTimeMs = Date.now() - reasoningStartTime;
          flushStreamUpdate();
        }
      } else {
        // Show error in the assistant message
        const errorMessage = error instanceof Error ? error.message : "An error occurred";
        flushStreamUpdate();
        updateMessages(
          (curr) =>
            curr.map((msg, i) =>
              i === curr.length - 1 ? { ...msg, content: msg.content + `\n\n**Error:** ${errorMessage}` } : msg
            ),
          streamChatId
        );
      }
    } finally {
      flushStreamUpdate();
      isStreaming = false;
      setConversationPersistencePaused(false);
      isReasoning = false;
      requestStartedAt = 0;
      hasReceivedOutput = false;
      abortController = null;
    }
  }

  async function editMessage(idx: number, newContent: string) {
    if (isStreaming || !$selectedModelStore) return;

    // Update the user message at the specified index
    updateMessages((curr) => curr.map((msg, i) => (i === idx ? { ...msg, content: newContent } : msg)));

    // Trigger a new chat request with the updated messages
    await regenerateFromIndex(idx);
  }

  function handleKeyDown(event: KeyboardEvent) {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      sendMessage();
    }
  }

  const ACCEPTED_IMAGE_FORMATS = ["image/jpeg", "image/png", "image/gif", "image/webp"];
  const MAX_IMAGE_SIZE = 20 * 1024 * 1024;
  const MAX_IMAGES_PER_MESSAGE = 5;

  function validateImageFile(file: File): string | null {
    if (!ACCEPTED_IMAGE_FORMATS.includes(file.type)) {
      return `Invalid file type: ${file.type}. Accepted formats: JPG, PNG, GIF, WEBP`;
    }
    if (file.size > MAX_IMAGE_SIZE) {
      return `File too large: ${(file.size / 1024 / 1024).toFixed(1)}MB. Maximum size: 20MB`;
    }
    return null;
  }

  function fileToAttachedImage(file: File): Promise<AttachedImage> {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () =>
        resolve({
          url: reader.result as string,
          name: file.name || "image",
          size: file.size,
        });
      reader.onerror = () => reject(new Error("Failed to read file"));
      reader.readAsDataURL(file);
    });
  }

  async function processImageFiles(files: File[]): Promise<void> {
    imageError = null;

    if (attachedImages.length + files.length > MAX_IMAGES_PER_MESSAGE) {
      imageError = `Maximum ${MAX_IMAGES_PER_MESSAGE} images per message`;
      return;
    }

    for (const file of files) {
      const error = validateImageFile(file);
      if (error) {
        imageError = error;
        return;
      }
    }

    try {
      const newImages = await Promise.all(files.map(fileToAttachedImage));
      attachedImages = [...attachedImages, ...newImages];
    } catch (error) {
      imageError = error instanceof Error ? error.message : "Failed to process images";
    }
  }

  function formatFileSize(bytes: number): string {
    if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
  }

  function handleImageSelect(event: Event) {
    const input = event.target as HTMLInputElement;
    if (input.files && input.files.length > 0) {
      processImageFiles(Array.from(input.files));
    }
    // Reset the input so the same file can be selected again
    input.value = "";
  }

  function handlePaste(event: ClipboardEvent) {
    const clipboardData = event.clipboardData;
    if (!clipboardData) return;

    const files: File[] = [];
    for (const item of Array.from(clipboardData.items)) {
      if (item.kind === "file" && item.type.startsWith("image/")) {
        const file = item.getAsFile();
        if (file) {
          files.push(file);
        }
      }
    }

    if (files.length === 0) {
      for (const file of Array.from(clipboardData.files)) {
        if (file.type.startsWith("image/")) {
          files.push(file);
        }
      }
    }

    if (files.length === 0) return;
    event.preventDefault();
    void processImageFiles(files);
  }

  function removeImage(idx: number) {
    attachedImages = attachedImages.filter((_, i) => i !== idx);
    imageError = null;
  }

  async function compactChat() {
    if (!canCompact) return;

    const streamChatId = get(currentChatId) ?? ensureActiveConversation();
    const sourceMessages = get(conversations).find((c) => c.id === streamChatId)?.messages ?? [];
    const plan = planConversationCompaction(sourceMessages, contextTotal);
    if (!plan) return;

    isCompacting = true;
    compactError = null;
    setConversationPersistencePaused(true);
    const controller = new AbortController();

    try {
      const summary = await completeChatCompletion(
        $selectedModelStore,
        buildCompactionRequestMessages(plan),
        controller.signal,
        {
          temperature: 0.2,
          max_tokens: contextTotal > 0 ? Math.min(4096, Math.max(1024, Math.floor(contextTotal * 0.08))) : 2048,
        }
      );
      setActiveMessages(streamChatId, insertCompactSummary(sourceMessages, plan, summary));
    } catch (error) {
      compactError = error instanceof Error ? error.message : "Failed to compact conversation";
    } finally {
      isCompacting = false;
      setConversationPersistencePaused(false);
    }
  }

  function setConfigStatusMessage(message: string): void {
    configMessage = message;
  }

  function formatTokenCount(value: number): string {
    if (value >= 1_000_000) {
      return `${(value / 1_000_000).toFixed(value >= 10_000_000 ? 0 : 1)}M`;
    }
    if (value >= 10_000) {
      return `${Math.round(value / 1000)}K`;
    }
    return value.toLocaleString();
  }

  function calculateContextUsed(timings: ChatMessageTimings | null, promptProgress: ChatMessagePromptProgress | null): number {
    if (timings) {
      return (timings.prompt_n ?? 0) + (timings.cache_n ?? 0) + (timings.predicted_n ?? 0);
    }
    if (promptProgress) {
      return Math.max(0, promptProgress.processed);
    }
    return 0;
  }

  function calculateLiveTokensPerSecond(timings: ChatMessageTimings | null, promptProgress: ChatMessagePromptProgress | null): number {
    if (timings?.predicted_n && timings.predicted_ms && timings.predicted_ms > 0) {
      return (timings.predicted_n / timings.predicted_ms) * 1000;
    }
    if (timings?.predicted_per_second && timings.predicted_per_second > 0) {
      return timings.predicted_per_second;
    }
    if (promptProgress && promptProgress.processed > promptProgress.cache && promptProgress.time_ms > 0) {
      return ((promptProgress.processed - promptProgress.cache) / promptProgress.time_ms) * 1000;
    }
    return 0;
  }

</script>

<div class="relative flex h-full min-h-0 flex-col">
  <!-- Top toolbar: model + actions -->
  <div class="shrink-0 px-1 pb-2 pt-2">
    <div class="mx-auto flex w-full max-w-[64rem] items-center gap-2">
      {#if !$sidebarOpen}
        <button
          class="btn p-2"
          onclick={() => sidebarOpen.set(true)}
          title="Show history"
          aria-label="Show history"
        >
          <PanelLeftOpen class="h-4 w-4" />
        </button>
      {/if}
      <ModelSelector bind:value={$selectedModelStore} placeholder="Select a model..." disabled={isStreaming} />

      <div class="ml-auto flex items-center gap-2">
        <button
          class="btn p-2"
          onclick={() => (showModelConfig = true)}
          disabled={!selectedModelInfo || !!selectedModelInfo.peerID || isStreaming}
          title="Configure model"
          aria-label="Configure model"
        >
          <SlidersHorizontal class="h-4 w-4" />
        </button>
        <ConfigImportExport
          buttonClass="btn p-2"
          onstatus={setConfigStatusMessage}
        />
        <button
          class="btn p-2"
          class:active={showSettings}
          onclick={() => (showSettings = !showSettings)}
          title="Settings"
          aria-label="Settings"
        >
          <Settings class="h-4 w-4" />
        </button>
        <button
          class="btn p-2"
          onclick={compactChat}
          disabled={!canCompact}
          title="Compact context"
          aria-label="Compact context"
        >
          <Archive class="h-4 w-4" />
        </button>
        <button
          class="btn p-2"
          onclick={newChat}
          disabled={messages.length === 0 && !isStreaming}
          title="New chat"
          aria-label="New chat"
        >
          <Plus class="h-4 w-4" />
        </button>
      </div>
    </div>
  </div>

  <!-- Settings drawer -->
  {#if showSettings}
    <div class="mx-auto mb-3 w-full max-w-[64rem] shrink-0 rounded-sm border border-border bg-surface p-4">
      <div>
        <label
          class="mb-2 block text-[10px] font-bold uppercase tracking-widest text-txtsecondary"
          for="system-prompt"
        >
          System Prompt
        </label>
        <textarea
          id="system-prompt"
          class="w-full resize-none rounded-sm border border-border bg-black px-3 py-2 text-sm text-txtmain placeholder-zinc-700 outline-none transition-colors duration-150 focus:border-white"
          placeholder="You are a helpful assistant..."
          rows="3"
          bind:value={$systemPromptStore}
          disabled={isStreaming}
        ></textarea>
      </div>
    </div>
  {/if}

  {#if configMessage}
    <div class="mx-auto mb-3 w-full max-w-[64rem] shrink-0 rounded-sm border border-border bg-surface px-3 py-2 text-xs text-txtsecondary">
      {configMessage}
    </div>
  {/if}

  {#if !hasModels}
    <div class="flex flex-1 items-center justify-center text-center text-txtsecondary">
      <div>
        <p class="text-xs font-bold uppercase tracking-widest">No models configured</p>
        <p class="mt-2 text-sm">Add models to your configuration to start chatting.</p>
      </div>
    </div>
  {:else if isEmpty}
    <!-- Empty / welcome state: centered headline + composer -->
    <div class="flex flex-1 items-center justify-center px-1 pb-12">
      <div class="w-full max-w-[64rem]">
        <div class="mb-10 text-center">
          <h1 class="mb-3 text-3xl font-bold tracking-tight text-txtmain md:text-4xl">
            What can I help with?
          </h1>
          <p class="font-mono text-[11px] uppercase tracking-widest text-txtsecondary">
            Send a message · Shift Enter for newline · attach images
          </p>
        </div>

        {@render composer()}
      </div>
    </div>
  {:else}
    <!-- Conversation state: scrolling messages + sticky composer -->
    <div
      bind:this={scrollContainer}
      onscroll={handleScroll}
      class="min-h-0 flex-1 overflow-y-auto px-1"
      style="scrollbar-gutter: stable both-edges;"
    >
      <div class="mx-auto w-full max-w-[64rem] pt-2" style="padding-bottom: calc(var(--composer-height, 9rem) + 2rem);">
        {#each messages as message, idx (idx)}
          <ChatMessageComponent
            role={message.role}
            content={message.content}
            reasoning_content={message.reasoning_content}
            reasoningTimeMs={message.reasoningTimeMs}
            timings={message.timings}
            promptProgress={message.promptProgress}
            isStreaming={isStreaming && idx === messages.length - 1 && message.role === "assistant"}
            isReasoning={isReasoning && idx === messages.length - 1 && message.role === "assistant"}
            loadingState={idx === messages.length - 1 && message.role === "assistant" ? modelLoadingState : null}
            onEdit={message.role === "user" ? (newContent) => editMessage(idx, newContent) : undefined}
            onRegenerate={message.role === "assistant" && idx > 0 && messages[idx - 1].role === "user"
              ? () => regenerateFromIndex(idx - 1)
              : undefined}
          />
        {/each}
      </div>
    </div>

    <!-- Sticky composer: opaque backdrop so messages aren't visible through it -->
    <div bind:this={composerWrap} class="pointer-events-none absolute bottom-0 left-0 right-0">
      <!-- Short, hard fade just at the top edge of the composer area -->
      <div class="pointer-events-none h-3 bg-gradient-to-t from-background to-transparent"></div>
      <div class="pointer-events-auto bg-background pb-4">
        <div class="mx-auto w-full max-w-[64rem] px-1">
          {@render composer()}
        </div>
      </div>
    </div>
  {/if}
</div>

<ModelConfigDialog
  open={showModelConfig}
  modelId={selectedModelInfo?.id ?? ""}
  onClose={() => (showModelConfig = false)}
  onModelChanged={(newId) => {
    if (newId) {
      $selectedModelStore = newId;
    }
  }}
/>

{#snippet composer()}
  <div
    class="composer overflow-hidden rounded-[2px] border border-border bg-surface shadow-2xl shadow-black/40 transition-colors duration-150 focus-within:border-border-hover"
  >
    <!-- Image attachment cards -->
    {#if attachedImages.length > 0}
      <div class="composer-attachments overflow-x-auto px-3 pt-3 pb-1">
        <div class="flex gap-2.5">
          {#each attachedImages as image, idx (idx)}
            <div
              class="group relative flex w-44 shrink-0 flex-col overflow-hidden rounded-[2px] border border-border bg-zinc-950 transition-colors duration-150 hover:border-border-hover"
            >
              <div class="relative h-20 w-full overflow-hidden bg-black">
                <img
                  src={image.url}
                  alt={image.name}
                  class="h-full w-full object-cover"
                />
                <button
                  class="absolute right-1.5 top-1.5 flex h-5 w-5 items-center justify-center rounded-[2px] bg-black/70 text-white/85 ring-1 ring-white/15 backdrop-blur-sm transition-colors hover:bg-black/90 hover:text-white"
                  onclick={() => removeImage(idx)}
                  title="Remove image"
                  aria-label="Remove image"
                >
                  <X class="h-3 w-3" />
                </button>
              </div>
              <div class="px-2.5 py-1.5">
                <div class="truncate font-mono text-[11px] text-txtmain" title={image.name}>
                  {image.name}
                </div>
                <div class="font-mono text-[10px] text-txtmuted">
                  {formatFileSize(image.size)}
                </div>
              </div>
            </div>
          {/each}
        </div>
      </div>
    {/if}

    <!-- Error message -->
    {#if imageError}
      <div class="mx-3 mt-3 rounded-[2px] border border-error/40 bg-error/10 px-3 py-2 text-xs text-error">
        {imageError}
      </div>
    {/if}
    {#if compactError}
      <div class="mx-3 mt-3 rounded-[2px] border border-error/40 bg-error/10 px-3 py-2 text-xs text-error">
        {compactError}
      </div>
    {/if}

    <!-- Hidden file input -->
    <input
      type="file"
      accept=".jpg,.jpeg,.png,.gif,.webp"
      multiple
      class="hidden"
      bind:this={fileInput}
      onchange={handleImageSelect}
    />

    <!-- Textarea + inline send button -->
    <div class="flex items-end gap-2 px-3 pt-3 pb-3">
      <textarea
        bind:this={textareaEl}
        bind:value={userInput}
        onkeydown={handleKeyDown}
        onpaste={handlePaste}
        disabled={isStreaming || isCompacting || !$selectedModelStore}
        placeholder={isCompacting ? "Compacting context..." : $selectedModelStore ? "Message the agent..." : "Select a model to start..."}
        rows="1"
        class="min-h-12 flex-1 resize-none border-0 bg-transparent px-1 py-2 text-sm leading-6 text-txtmain placeholder-zinc-700 outline-none disabled:opacity-50"
      ></textarea>
      <button
        class="flex h-9 w-9 shrink-0 items-center justify-center rounded-[2px] border border-border bg-black text-txtsecondary transition-all duration-150 hover:border-border-hover hover:text-white disabled:cursor-not-allowed disabled:opacity-40"
        onclick={sendMessage}
        disabled={!$chatsReady || isStreaming || isCompacting || (!userInput.trim() && attachedImages.length === 0) || !$selectedModelStore}
        title="Send message"
        aria-label="Send message"
      >
        <Send class="h-4 w-4" />
      </button>
    </div>

    <!-- Footer: hints | live stats | actions -->
    <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 border-t border-border bg-black/40 px-3 py-2">
      <div class="hidden items-center gap-1.5 font-mono text-[10px] uppercase tracking-widest text-txtmuted sm:flex">
        <kbd class="rounded-[2px] border border-border bg-zinc-900 px-1.5 py-0.5 text-[10px] font-bold tracking-wider text-txtsecondary">
          Enter
        </kbd>
        <span>to send</span>
        <span class="text-txtmuted/70">·</span>
        <kbd class="rounded-[2px] border border-border bg-zinc-900 px-1.5 py-0.5 text-[10px] font-bold tracking-wider text-txtsecondary">
          Shift+Enter
        </kbd>
        <span>for newline</span>
      </div>

      <div class="flex items-center gap-3 font-mono text-[10px] uppercase tracking-wider text-txtsecondary">
        {#if contextTotal > 0}
          <span
            class="flex items-center gap-1.5"
            title={`${contextUsed.toLocaleString()} of ${contextTotal.toLocaleString()} (${contextPercent}%)`}
          >
            <span>Context:</span>
            <span class="text-txtmain">{contextUsed.toLocaleString()}</span>
            <span class="text-txtmuted">/ {contextTotal.toLocaleString()}</span>
            <span class="text-txtmuted">({contextPercent}%)</span>
          </span>
        {/if}
        {#if outputUsed > 0 || liveTokensPerSecond > 0}
          <span class="flex items-center gap-1.5">
            <span>Output:</span>
            <span class="text-txtmain">{outputUsed.toLocaleString()}</span>
            <span class="text-txtmuted">/ &infin;</span>
            {#if liveTokensPerSecond > 0}
              <span class="text-txtmuted/70">·</span>
              <span class="text-txtmain">{liveTokensPerSecond.toFixed(1)}</span>
              <span>T/S</span>
            {/if}
          </span>
        {:else if contextTotal > 0 && contextAvailable > 0}
          <span class="hidden items-center gap-1.5 md:flex" title={`${contextAvailable.toLocaleString()} tokens left`}>
            <span>Free:</span>
            <span class="text-txtmain">{formatTokenCount(contextAvailable)}</span>
          </span>
        {/if}
      </div>

      <div class="ml-auto flex items-center gap-2">
        <button
          class="rounded-[2px] border border-border bg-zinc-950 px-3 py-1 font-mono text-[10px] font-bold uppercase tracking-wider text-txtsecondary transition-colors duration-150 hover:border-border-hover hover:text-white disabled:cursor-not-allowed disabled:opacity-40"
          onclick={() => fileInput?.click()}
          disabled={isStreaming || isCompacting || !$selectedModelStore}
          title="Attach image"
          aria-label="Attach image"
        >
          Attach image
        </button>

        {#if isStreaming}
          <button
            class="rounded-[2px] border border-red-600 bg-red-600 px-3 py-1 font-mono text-[10px] font-bold uppercase tracking-wider text-white transition-colors hover:border-red-500 hover:bg-red-500"
            onclick={cancelStreaming}
          >
            Stop
          </button>
        {:else if isCompacting}
          <button
            class="rounded-[2px] border border-border bg-zinc-950 px-3 py-1 font-mono text-[10px] font-bold uppercase tracking-wider text-txtsecondary"
            disabled
          >
            Compacting
          </button>
        {/if}
      </div>
    </div>
  </div>
{/snippet}

<style>
  .btn.active {
    color: #ffffff;
    border-color: var(--color-border-hover);
  }
</style>
