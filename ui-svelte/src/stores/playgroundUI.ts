import { persistentStore } from "./persistent";

// UI-level state for the playground (separate from chat data).
// Persisting this lets the user keep their preferred layout across sessions.
export const sidebarOpen = persistentStore<boolean>("playground-sidebar-open", true);
