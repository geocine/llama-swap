<script lang="ts">
  import {
    conversations,
    currentChatId,
    selectConversation,
    deleteConversation,
    renameConversation,
    newConversation,
  } from "../../stores/chats";
  import { sidebarOpen } from "../../stores/playgroundUI";
  import { confirmDialog } from "../../stores/confirm";
  import { Plus, Trash2, Check, X, Pencil, PanelLeftClose } from "lucide-svelte";

  // Newest conversations first — copy before sorting since the source is reactive
  let sorted = $derived([...$conversations].sort((a, b) => b.updatedAt - a.updatedAt));

  let editingId = $state<string | null>(null);
  let editingTitle = $state("");
  let inputEl: HTMLInputElement | undefined = $state();

  function startEditing(id: string, currentTitle: string) {
    editingId = id;
    editingTitle = currentTitle;
    queueMicrotask(() => {
      inputEl?.focus();
      inputEl?.select();
    });
  }

  function commitEdit() {
    if (editingId) {
      renameConversation(editingId, editingTitle);
    }
    editingId = null;
    editingTitle = "";
  }

  function cancelEdit() {
    editingId = null;
    editingTitle = "";
  }

  function handleEditKey(event: KeyboardEvent) {
    if (event.key === "Enter") {
      event.preventDefault();
      commitEdit();
    } else if (event.key === "Escape") {
      event.preventDefault();
      cancelEdit();
    }
  }

  async function confirmDelete(id: string, title: string) {
    const label = title?.trim() ? title : "this conversation";
    const ok = await confirmDialog({
      title: "Delete conversation",
      message: `Delete "${label}"? This cannot be undone.`,
      confirmLabel: "Delete",
      cancelLabel: "Cancel",
      danger: true,
    });
    if (ok) {
      deleteConversation(id);
    }
  }

  // Group conversations by relative date for a clean section list
  function bucket(updatedAt: number): "Today" | "Yesterday" | "Earlier" {
    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
    const yesterday = today - 24 * 60 * 60 * 1000;
    if (updatedAt >= today) return "Today";
    if (updatedAt >= yesterday) return "Yesterday";
    return "Earlier";
  }

  type Bucket = "Today" | "Yesterday" | "Earlier";
  let grouped = $derived.by(() => {
    const out: Record<Bucket, typeof sorted> = { Today: [], Yesterday: [], Earlier: [] };
    for (const c of sorted) out[bucket(c.updatedAt)].push(c);
    return out;
  });
</script>

<aside class="flex h-full w-full flex-col border-r border-border bg-surface-elevated">
  <!-- Header -->
  <div class="flex shrink-0 items-center gap-2 border-b border-border px-3 py-2.5">
    <button
      class="-ml-1 inline-flex h-7 w-7 items-center justify-center rounded-sm text-txtsecondary transition-colors duration-150 hover:bg-secondary hover:text-white"
      onclick={() => sidebarOpen.set(false)}
      title="Hide history"
      aria-label="Hide history"
    >
      <PanelLeftClose class="h-3.5 w-3.5" />
    </button>
    <span class="flex-1 font-mono text-[10px] font-bold uppercase tracking-widest text-txtsecondary">
      Conversations
    </span>
    <button
      class="btn p-1.5"
      onclick={() => newConversation()}
      title="New chat"
      aria-label="New chat"
    >
      <Plus class="h-3.5 w-3.5" />
    </button>
  </div>

  <!-- List -->
  <div class="flex-1 overflow-y-auto">
    {#if sorted.length === 0}
      <div class="flex h-full items-center justify-center px-6 text-center">
        <div class="font-mono text-[10px] uppercase tracking-widest text-txtmuted">
          No conversations yet
        </div>
      </div>
    {:else}
      {#each (["Today", "Yesterday", "Earlier"] as const) as section (section)}
        {#if grouped[section].length > 0}
          <div class="px-3 pb-1 pt-3 font-mono text-[10px] uppercase tracking-widest text-txtmuted">
            {section}
          </div>
          <ul class="px-1 pb-1">
            {#each grouped[section] as conv (conv.id)}
              <li class="group">
                {#if editingId === conv.id}
                  <div class="flex items-center gap-1 rounded-sm border border-border-hover bg-surface px-2 py-1.5">
                    <input
                      bind:this={inputEl}
                      bind:value={editingTitle}
                      onkeydown={handleEditKey}
                      onblur={commitEdit}
                      class="min-w-0 flex-1 bg-transparent text-xs text-txtmain outline-none placeholder-zinc-700"
                    />
                    <button
                      class="shrink-0 p-1 text-txtsecondary transition-colors duration-150 hover:text-success"
                      onclick={commitEdit}
                      aria-label="Save"
                    >
                      <Check class="h-3 w-3" />
                    </button>
                    <button
                      class="shrink-0 p-1 text-txtsecondary transition-colors duration-150 hover:text-error"
                      onmousedown={(e) => { e.preventDefault(); cancelEdit(); }}
                      aria-label="Cancel"
                    >
                      <X class="h-3 w-3" />
                    </button>
                  </div>
                {:else}
                  <div
                    class="relative flex items-center gap-1 rounded-sm transition-colors duration-150 {conv.id === $currentChatId
                      ? 'bg-secondary'
                      : 'hover:bg-secondary/60'}"
                  >
                    <button
                      type="button"
                      class="min-w-0 flex-1 truncate px-2 py-2 text-left text-xs text-txtmain"
                      onclick={() => selectConversation(conv.id)}
                      title={conv.title}
                    >
                      <span
                        class="block truncate {conv.id === $currentChatId ? 'text-white' : 'text-txtmain'}"
                      >
                        {conv.title || "New chat"}
                      </span>
                    </button>
                    <div
                      class="flex shrink-0 items-center gap-0.5 pr-1 opacity-0 transition-opacity duration-150 group-hover:opacity-100 focus-within:opacity-100"
                    >
                      <button
                        class="p-1 text-txtsecondary transition-colors duration-150 hover:text-white"
                        onclick={() => startEditing(conv.id, conv.title)}
                        title="Rename"
                        aria-label="Rename"
                      >
                        <Pencil class="h-3 w-3" />
                      </button>
                      <button
                        class="p-1 text-txtsecondary transition-colors duration-150 hover:text-error"
                        onclick={() => confirmDelete(conv.id, conv.title)}
                        title="Delete"
                        aria-label="Delete"
                      >
                        <Trash2 class="h-3 w-3" />
                      </button>
                    </div>
                  </div>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      {/each}
    {/if}
  </div>
</aside>
