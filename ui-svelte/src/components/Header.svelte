<script lang="ts">
  import { link } from "svelte-spa-router";
  import { authRequired, isAuthenticated, logout } from "../stores/auth";
  import { screenWidth, appTitle, isNarrow } from "../stores/theme";
  import { currentRoute } from "../stores/route";
  import { playgroundActivity } from "../stores/playgroundActivity";
  import ConnectionStatus from "./ConnectionStatus.svelte";
  import Logo from "./Logo.svelte";

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
  <div class="flex items-center gap-2.5">
    <a
      href="/"
      use:link
      title="Home"
      aria-label="Home"
      class="inline-flex h-7 w-7 items-center justify-center rounded-sm border border-border bg-surface text-white outline-none transition-colors duration-150 hover:border-border-hover focus:outline-none focus-visible:border-border-hover"
    >
      <Logo size={16} />
    </a>
    {#if $screenWidth !== "xs" && $screenWidth !== "sm"}
      <div class="flex items-baseline gap-1.5">
        <h1
          contenteditable="true"
          class="p-0 font-mono text-sm font-bold tracking-tight text-txtmain outline-none transition-colors duration-150 hover:text-white cursor-text"
          onblur={handleBlur}
          onkeydown={handleKeyDown}
          spellcheck="false"
        >
          {$appTitle}
        </h1>
        <span
          class="select-none rounded-sm border border-border bg-surface px-1.5 py-0.5 font-mono text-[9px] font-bold uppercase tracking-widest text-txtsecondary"
          title="geocine edition"
          aria-label="geocine edition"
        >
          geocine edition
        </span>
      </div>
    {/if}
  </div>

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
    background: linear-gradient(90deg, #71717a, #e4e4e7, #ffffff, #e4e4e7, #71717a);
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
