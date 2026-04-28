import { writable, derived } from "svelte/store";

const chatStreaming = writable(false);

export const playgroundActivity = derived(
  [chatStreaming],
  ([$chat]) => $chat
);

export const playgroundStores = {
  chatStreaming,
};
