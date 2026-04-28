<script lang="ts">
  import { link } from "svelte-spa-router";
  import { authRequired, isAuthenticated, logout } from "../stores/auth";
  import { screenWidth, appTitle, isNarrow } from "../stores/theme";
  import { currentRoute } from "../stores/route";
  import { playgroundActivity } from "../stores/playgroundActivity";
  import ConnectionStatus from "./ConnectionStatus.svelte";

  function handleTitleChange(newTitle: string): void {
    const sanitized = newTitle.replace(/\n/g, "").trim().substring(0, 64) || "llama-swap";
    appTitle.set(sanitized);
  }

  function handleKeyDown(e: KeyboardEvent): void {
    if (e.key === "Enter") {
      e.preventDefault();
      const target = e.currentTarget as HTMLElement;
      handleTitleChange(target.textContent || "(set title)");
      target.blur();
    }
  }

  function handleBlur(e: FocusEvent): void {
    const target = e.currentTarget as HTMLElement;
    handleTitleChange(target.textContent || "(set title)");
  }

  function isActive(path: string, current: string): boolean {
    return path === "/" ? current === "/" : current.startsWith(path);
  }

  function handleLogout(): void {
    void logout();
  }
</script>

<header
  class="flex items-center justify-between bg-surface-elevated border-b border-border px-4 {$isNarrow
    ? 'py-1 h-[48px]'
    : 'py-2 h-[56px]'}"
>
  {#if $screenWidth !== "xs" && $screenWidth !== "sm"}
    <h1
      contenteditable="true"
      class="p-0 text-sm font-bold uppercase tracking-wide outline-none hover:text-white transition-colors duration-150 cursor-text"
      onblur={handleBlur}
      onkeydown={handleKeyDown}
    >
      {$appTitle}
    </h1>
  {/if}

  <menu class="flex items-center gap-1 overflow-x-auto">
    <a
      href="/"
      use:link
      class="navlink {isActive('/', $currentRoute) ? 'text-white' : ''} {$playgroundActivity ? 'activity-link' : ''}"
    >
      Chat
    </a>
    <a
      href="/models"
      use:link
      class="navlink"
      class:text-white={isActive("/models", $currentRoute)}
    >
      Models
    </a>
    <a
      href="/activity"
      use:link
      class="navlink"
      class:text-white={isActive("/activity", $currentRoute)}
    >
      Activity
    </a>
    <a
      href="/logs"
      use:link
      class="navlink"
      class:text-white={isActive("/logs", $currentRoute)}
    >
      Logs
    </a>
    {#if $authRequired && $isAuthenticated}
      <button
        class="navlink"
        onclick={handleLogout}
        title="Sign out"
      >
        Logout
      </button>
    {/if}
    <ConnectionStatus />
  </menu>
</header>

<style>
  .activity-link {
    background: linear-gradient(90deg, #6366f1, #8b5cf6, #a855f7, #8b5cf6, #6366f1);
    background-size: 200% 100%;
    -webkit-background-clip: text;
    background-clip: text;
    -webkit-text-fill-color: transparent;
    animation: gradient-shift 2s linear infinite;
  }

  @keyframes gradient-shift {
    0% {
      background-position: 0% 50%;
    }
    100% {
      background-position: 200% 50%;
    }
  }
</style>
