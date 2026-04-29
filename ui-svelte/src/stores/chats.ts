import { writable, derived, get, type Readable } from "svelte/store";
import type { ChatMessage } from "../lib/types";

export interface Conversation {
  id: string;
  title: string;
  messages: ChatMessage[];
  createdAt: number;
  updatedAt: number;
}

const CONVERSATIONS_KEY = "playground-conversations";
const CURRENT_KEY = "playground-current-conversation";
const LEGACY_MESSAGES_KEY = "playground-messages";
const PERSIST_THROTTLE_MS = 1000;

function generateId(): string {
  return Date.now().toString(36) + Math.random().toString(36).slice(2, 9);
}

// Title heuristic — first user message text, single line, capped to ~60 chars
function deriveTitle(messages: ChatMessage[]): string {
  const first = messages.find((m) => m.role === "user");
  if (!first) return "New chat";
  const text =
    typeof first.content === "string"
      ? first.content
      : first.content
          .filter((p): p is { type: "text"; text: string } => p.type === "text")
          .map((p) => p.text)
          .join(" ");
  const oneLine = text.replace(/\s+/g, " ").trim();
  if (!oneLine) return "New chat";
  return oneLine.length > 60 ? oneLine.slice(0, 57) + "..." : oneLine;
}

// Load conversations from localStorage, migrating legacy `playground-messages`
// (single-conversation format) into the new multi-conversation list.
function loadInitial(): Conversation[] {
  if (typeof window === "undefined") return [];

  try {
    const saved = localStorage.getItem(CONVERSATIONS_KEY);
    if (saved) {
      const parsed = JSON.parse(saved) as Conversation[];
      if (Array.isArray(parsed)) return parsed;
    }
  } catch (e) {
    console.error("Failed to parse conversations from storage", e);
  }

  // Legacy migration: a single chat persisted as `playground-messages`
  try {
    const legacy = localStorage.getItem(LEGACY_MESSAGES_KEY);
    if (legacy) {
      const messages = JSON.parse(legacy) as ChatMessage[];
      if (Array.isArray(messages) && messages.length > 0) {
        const now = Date.now();
        const migrated: Conversation = {
          id: generateId(),
          title: deriveTitle(messages),
          messages,
          createdAt: now,
          updatedAt: now,
        };
        // Drop the legacy key so we don't re-migrate
        try { localStorage.removeItem(LEGACY_MESSAGES_KEY); } catch {}
        return [migrated];
      }
    }
  } catch (e) {
    console.error("Failed to migrate legacy chat history", e);
  }

  return [];
}

function loadCurrentId(initial: Conversation[]): string | null {
  if (typeof window === "undefined") return initial[0]?.id ?? null;
  try {
    const saved = localStorage.getItem(CURRENT_KEY);
    if (saved && initial.some((c) => c.id === saved)) return saved;
  } catch {}
  return initial[0]?.id ?? null;
}

const initialConversations = loadInitial();
const conversationsStore = writable<Conversation[]>(initialConversations);
const currentChatIdStore = writable<string | null>(loadCurrentId(initialConversations));

// Throttled persistence — streaming chunks update the active conversation
// many times per second; we batch the localStorage writes to avoid stalls.
let pendingWrite: ReturnType<typeof setTimeout> | null = null;
let lastWriteAt = 0;
function persistConversations(): void {
  if (typeof window === "undefined") return;
  const now = Date.now();
  const elapsed = now - lastWriteAt;
  const flush = () => {
    try {
      localStorage.setItem(CONVERSATIONS_KEY, JSON.stringify(get(conversationsStore)));
    } catch (e) {
      console.error("Failed to persist conversations", e);
    }
    lastWriteAt = Date.now();
    pendingWrite = null;
  };
  if (pendingWrite) return;
  if (elapsed >= PERSIST_THROTTLE_MS) {
    flush();
  } else {
    pendingWrite = setTimeout(flush, PERSIST_THROTTLE_MS - elapsed);
  }
}
conversationsStore.subscribe(persistConversations);

currentChatIdStore.subscribe((id) => {
  if (typeof window === "undefined") return;
  try {
    if (id) localStorage.setItem(CURRENT_KEY, id);
    else localStorage.removeItem(CURRENT_KEY);
  } catch {}
});

export const conversations: Readable<Conversation[]> = { subscribe: conversationsStore.subscribe };
export const currentChatId: Readable<string | null> = { subscribe: currentChatIdStore.subscribe };

export const activeConversation: Readable<Conversation | null> = derived(
  [conversationsStore, currentChatIdStore],
  ([$convs, $id]) => $convs.find((c) => c.id === $id) ?? null
);

export function newConversation(): string {
  const now = Date.now();
  const conv: Conversation = {
    id: generateId(),
    title: "New chat",
    messages: [],
    createdAt: now,
    updatedAt: now,
  };
  conversationsStore.update((list) => [conv, ...list]);
  currentChatIdStore.set(conv.id);
  return conv.id;
}

export function selectConversation(id: string): void {
  currentChatIdStore.set(id);
}

export function deleteConversation(id: string): void {
  let nextId: string | null | undefined;
  conversationsStore.update((list) => {
    const filtered = list.filter((c) => c.id !== id);
    nextId = filtered[0]?.id ?? null;
    return filtered;
  });
  currentChatIdStore.update((curr) => (curr === id ? (nextId ?? null) : curr));
}

export function renameConversation(id: string, title: string): void {
  const trimmed = title.trim() || "New chat";
  conversationsStore.update((list) =>
    list.map((c) => (c.id === id ? { ...c, title: trimmed, updatedAt: Date.now() } : c))
  );
}

// Replace the messages of a conversation. Auto-derives title only while it is
// still the default ("New chat") to avoid clobbering user-renamed titles.
export function setMessages(id: string, messages: ChatMessage[]): void {
  conversationsStore.update((list) =>
    list.map((c) => {
      if (c.id !== id) return c;
      const shouldRetitle = !c.title || c.title === "New chat";
      return {
        ...c,
        messages,
        title: shouldRetitle ? deriveTitle(messages) : c.title,
        updatedAt: Date.now(),
      };
    })
  );
}

// Ensure there is a current conversation; create one if the list is empty.
export function ensureActiveConversation(): string {
  const list = get(conversationsStore);
  const currentId = get(currentChatIdStore);
  if (currentId && list.some((c) => c.id === currentId)) return currentId;
  if (list.length > 0) {
    currentChatIdStore.set(list[0].id);
    return list[0].id;
  }
  return newConversation();
}
