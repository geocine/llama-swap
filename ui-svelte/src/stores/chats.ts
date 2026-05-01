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
const DB_NAME = "llama-swap-playground";
const DB_VERSION = 2;
const CONVERSATION_STORE = "conversations";
const IMAGE_STORE = "images";
const PERSIST_THROTTLE_MS = 2000;
const MAX_CONVERSATIONS = 25;
const MAX_STORED_CONVERSATIONS_CHARS = 1_000_000;
const MAX_MESSAGES_PER_CONVERSATION = 40;
const MAX_PERSISTED_TEXT_CHARS = 30_000;
const MAX_RENDERED_TEXT_CHARS = 120_000;
const STORED_IMAGE_PREFIX = "indexeddb://image/";

interface StoredImage {
  id: string;
  blob: Blob;
  type: string;
  createdAt: number;
}

const imageRefByDataUrl = new Map<string, string>();
const dataUrlByImageRef = new Map<string, string>();

function generateId(): string {
  return Date.now().toString(36) + Math.random().toString(36).slice(2, 9);
}

function generateImageId(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return generateId();
}

function imageRef(id: string): string {
  return `${STORED_IMAGE_PREFIX}${id}`;
}

function imageIdFromRef(url: string): string | null {
  return url.startsWith(STORED_IMAGE_PREFIX) ? url.slice(STORED_IMAGE_PREFIX.length) : null;
}

let dbPromise: Promise<IDBDatabase> | null = null;

function openChatDB(): Promise<IDBDatabase> {
  if (dbPromise) return dbPromise;
  dbPromise = new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, DB_VERSION);

    request.onupgradeneeded = () => {
      const db = request.result;
      if (!db.objectStoreNames.contains(CONVERSATION_STORE)) {
        db.createObjectStore(CONVERSATION_STORE, { keyPath: "id" });
      }
      if (!db.objectStoreNames.contains(IMAGE_STORE)) {
        db.createObjectStore(IMAGE_STORE, { keyPath: "id" });
      }
    };

    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
  return dbPromise;
}

function txDone(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
    tx.onabort = () => reject(tx.error);
  });
}

function requestResult<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

async function loadConversationsFromDB(): Promise<Conversation[]> {
  if (typeof indexedDB === "undefined") return [];

  const db = await openChatDB();
  const tx = db.transaction(CONVERSATION_STORE, "readonly");
  const store = tx.objectStore(CONVERSATION_STORE);
  const conversations = await requestResult<Conversation[]>(store.getAll());
  await txDone(tx);

  return hydrateConversationImages(
    trimConversationsForRender(
      conversations
        .filter((conversation) => conversation && typeof conversation.id === "string")
        .sort((a, b) => b.updatedAt - a.updatedAt)
        .slice(0, MAX_CONVERSATIONS)
    )
  );
}

async function saveConversationsToDB(conversations: Conversation[]): Promise<void> {
  if (typeof indexedDB === "undefined") return;

  const trimmed = await trimConversationsForStorage(conversations);
  const db = await openChatDB();
  const tx = db.transaction(CONVERSATION_STORE, "readwrite");
  const store = tx.objectStore(CONVERSATION_STORE);
  store.clear();
  for (const conversation of trimmed) {
    store.put(conversation);
  }
  await txDone(tx);
  void cleanupUnreferencedImages(trimmed).catch((e) => {
    console.error("Failed to clean up persisted chat images", e);
  });
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

function truncateText(value: string): string {
  if (value.length <= MAX_PERSISTED_TEXT_CHARS) return value;
  return `${value.slice(0, MAX_PERSISTED_TEXT_CHARS)}\n\n[message truncated for browser storage]`;
}

function truncateForRender(value: string): string {
  if (value.length <= MAX_RENDERED_TEXT_CHARS) return value;
  return `${value.slice(0, MAX_RENDERED_TEXT_CHARS)}\n\n[message truncated for browser rendering]`;
}

async function dataUrlToBlob(url: string): Promise<Blob> {
  const response = await fetch(url);
  return response.blob();
}

function blobToDataUrl(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as string);
    reader.onerror = () => reject(new Error("Failed to read persisted image"));
    reader.readAsDataURL(blob);
  });
}

async function storeImageDataUrl(url: string): Promise<string> {
  const cachedRef = imageRefByDataUrl.get(url);
  if (cachedRef) return cachedRef;

  const blob = await dataUrlToBlob(url);
  const id = generateImageId();
  const ref = imageRef(id);
  const db = await openChatDB();
  const tx = db.transaction(IMAGE_STORE, "readwrite");
  const store = tx.objectStore(IMAGE_STORE);
  const image: StoredImage = {
    id,
    blob,
    type: blob.type,
    createdAt: Date.now(),
  };
  store.put(image);
  await txDone(tx);
  imageRefByDataUrl.set(url, ref);
  dataUrlByImageRef.set(ref, url);
  return ref;
}

async function loadStoredImageUrl(id: string): Promise<string | null> {
  const ref = imageRef(id);
  const cachedUrl = dataUrlByImageRef.get(ref);
  if (cachedUrl) return cachedUrl;

  const db = await openChatDB();
  const tx = db.transaction(IMAGE_STORE, "readonly");
  const store = tx.objectStore(IMAGE_STORE);
  const image = await requestResult<StoredImage | undefined>(store.get(id));
  await txDone(tx);
  if (!image?.blob) return null;
  const url = await blobToDataUrl(image.blob);
  imageRefByDataUrl.set(url, ref);
  dataUrlByImageRef.set(ref, url);
  return url;
}

async function trimMessageForStorage(message: ChatMessage): Promise<ChatMessage> {
  const reasoning_content = message.reasoning_content ? truncateText(message.reasoning_content) : message.reasoning_content;

  if (typeof message.content === "string") {
    return { ...message, content: truncateText(message.content), reasoning_content };
  }

  const content = await Promise.all(
    message.content.map(async (part) => {
      if (part.type === "text") {
        return { ...part, text: truncateText(part.text) };
      }
      if (imageIdFromRef(part.image_url.url)) {
        return part;
      }
      if (part.image_url.url.startsWith("data:image/")) {
        try {
          const url = await storeImageDataUrl(part.image_url.url);
          return { ...part, image_url: { url } };
        } catch (e) {
          console.error("Failed to persist chat image", e);
          return { ...part, image_url: { url: "" } };
        }
      }
      return part;
    })
  );

  return {
    ...message,
    content,
    reasoning_content,
  };
}

function trimMessageForRender(message: ChatMessage): ChatMessage {
  const reasoning_content = message.reasoning_content
    ? truncateForRender(message.reasoning_content)
    : message.reasoning_content;

  if (typeof message.content === "string") {
    return { ...message, content: truncateForRender(message.content), reasoning_content };
  }

  return {
    ...message,
    content: message.content.map((part) => {
      if (part.type === "text") {
        return { ...part, text: truncateForRender(part.text) };
      }
      return part;
    }),
    reasoning_content,
  };
}

async function trimConversationsForStorage(conversations: Conversation[]): Promise<Conversation[]> {
  const trimmed = conversations
    .slice()
    .sort((a, b) => b.updatedAt - a.updatedAt)
    .slice(0, MAX_CONVERSATIONS);

  return Promise.all(
    trimmed.map(async (conversation) => ({
      ...conversation,
      messages: await Promise.all(
        conversation.messages.slice(-MAX_MESSAGES_PER_CONVERSATION).map((message) => trimMessageForStorage(message))
      ),
    }))
  );
}

function trimConversationsForRender(conversations: Conversation[]): Conversation[] {
  return conversations.map((conversation) => ({
    ...conversation,
    messages: conversation.messages.map(trimMessageForRender),
  }));
}

async function hydrateConversationImages(conversations: Conversation[]): Promise<Conversation[]> {
  return Promise.all(
    conversations.map(async (conversation) => ({
      ...conversation,
      messages: await Promise.all(
        conversation.messages.map(async (message) => {
          if (typeof message.content === "string") return message;
          const content = await Promise.all(
            message.content.map(async (part) => {
              if (part.type !== "image_url") return part;
              const id = imageIdFromRef(part.image_url.url);
              if (!id) return part;
              const url = await loadStoredImageUrl(id);
              return { ...part, image_url: { url: url ?? "" } };
            })
          );
          return { ...message, content };
        })
      ),
    }))
  );
}

function collectImageRefs(conversations: Conversation[]): Set<string> {
  const refs = new Set<string>();
  for (const conversation of conversations) {
    for (const message of conversation.messages) {
      if (typeof message.content === "string") continue;
      for (const part of message.content) {
        if (part.type !== "image_url") continue;
        const id = imageIdFromRef(part.image_url.url);
        if (id) refs.add(id);
      }
    }
  }
  return refs;
}

async function cleanupUnreferencedImages(conversations: Conversation[]): Promise<void> {
  if (typeof indexedDB === "undefined") return;
  const refs = collectImageRefs(conversations);
  const db = await openChatDB();
  const tx = db.transaction(IMAGE_STORE, "readwrite");
  const store = tx.objectStore(IMAGE_STORE);
  const keys = await requestResult<IDBValidKey[]>(store.getAllKeys());
  for (const key of keys) {
    if (typeof key === "string" && !refs.has(key)) {
      const ref = imageRef(key);
      const url = dataUrlByImageRef.get(ref);
      if (url) imageRefByDataUrl.delete(url);
      dataUrlByImageRef.delete(ref);
      store.delete(key);
    }
  }
  await txDone(tx);
}

async function migrateLocalStorageConversations(): Promise<Conversation[] | null> {
  if (typeof window === "undefined") return null;

  try {
    const saved = localStorage.getItem(CONVERSATIONS_KEY);
    if (saved) {
      if (saved.length > MAX_STORED_CONVERSATIONS_CHARS) {
        console.warn("Saved chat history is too large to migrate; clearing it to keep the UI responsive.");
        localStorage.removeItem(CONVERSATIONS_KEY);
        localStorage.removeItem(CURRENT_KEY);
        return [];
      }

      const parsed = JSON.parse(saved) as Conversation[];
      if (Array.isArray(parsed)) {
        const conversations = trimConversationsForRender(parsed);
        await saveConversationsToDB(conversations);
        localStorage.removeItem(CONVERSATIONS_KEY);
        return conversations;
      }
    }
  } catch (e) {
    console.error("Failed to migrate conversations from localStorage", e);
  }

  try {
    const legacy = localStorage.getItem(LEGACY_MESSAGES_KEY);
    if (legacy) {
      if (legacy.length > MAX_STORED_CONVERSATIONS_CHARS) {
        console.warn("Legacy chat history is too large to migrate; clearing it to keep the UI responsive.");
        localStorage.removeItem(LEGACY_MESSAGES_KEY);
        return [];
      }

      const messages = JSON.parse(legacy) as ChatMessage[];
      if (Array.isArray(messages) && messages.length > 0) {
        const now = Date.now();
        const migrated = trimConversationsForRender([{
          id: generateId(),
          title: deriveTitle(messages),
          messages,
          createdAt: now,
          updatedAt: now,
        }]);
        await saveConversationsToDB(migrated);
        localStorage.removeItem(LEGACY_MESSAGES_KEY);
        return migrated;
      }
    }
  } catch (e) {
    console.error("Failed to migrate legacy chat history", e);
  }

  return null;
}

async function loadInitial(): Promise<Conversation[]> {
  if (typeof window === "undefined") return [];

  const migrated = await migrateLocalStorageConversations();
  if (migrated !== null) return migrated;

  try {
    return await loadConversationsFromDB();
  } catch (e) {
    console.error("Failed to load conversations from IndexedDB", e);
    return [];
  }
}

function loadCurrentId(initial: Conversation[] = []): string | null {
  if (typeof window === "undefined") return null;
  try {
    const saved = localStorage.getItem(CURRENT_KEY);
    if (saved && initial.some((c) => c.id === saved)) return saved;
  } catch {}
  return initial[0]?.id ?? null;
}

const conversationsStore = writable<Conversation[]>([]);
const currentChatIdStore = writable<string | null>(null);
export const chatsReady = writable(false);
let didLoadInitialConversations = false;

void loadInitial().then((initialConversations) => {
  didLoadInitialConversations = true;
  conversationsStore.set(initialConversations);
  currentChatIdStore.set(loadCurrentId(initialConversations));
  chatsReady.set(true);
});

// Throttled persistence — streaming chunks update the active conversation
// many times per second; we batch the localStorage writes to avoid stalls.
let pendingWrite: ReturnType<typeof setTimeout> | null = null;
let lastWriteAt = 0;
let persistencePaused = false;
let persistenceDirty = false;
function persistConversations(): void {
  if (typeof window === "undefined" || !didLoadInitialConversations) return;
  if (persistencePaused) {
    persistenceDirty = true;
    return;
  }

  const now = Date.now();
  const elapsed = now - lastWriteAt;
  const flush = () => {
    void saveConversationsToDB(get(conversationsStore)).catch((e) => {
      console.error("Failed to persist conversations", e);
    });
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

export function setConversationPersistencePaused(paused: boolean): void {
  persistencePaused = paused;
  if (!paused && persistenceDirty) {
    persistenceDirty = false;
    persistConversations();
  }
}

currentChatIdStore.subscribe((id) => {
  if (typeof window === "undefined" || !didLoadInitialConversations) return;
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
  if (!didLoadInitialConversations) return "";
  const list = get(conversationsStore);
  const currentId = get(currentChatIdStore);
  if (currentId && list.some((c) => c.id === currentId)) return currentId;
  if (list.length > 0) {
    currentChatIdStore.set(list[0].id);
    return list[0].id;
  }
  return newConversation();
}
