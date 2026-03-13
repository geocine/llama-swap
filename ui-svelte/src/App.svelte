<script lang="ts">
  import { onMount } from "svelte";
  import Router from "svelte-spa-router";
  import Header from "./components/Header.svelte";
  import LoginForm from "./components/LoginForm.svelte";
  import LogViewer from "./routes/LogViewer.svelte";
  import Models from "./routes/Models.svelte";
  import Activity from "./routes/Activity.svelte";
  import Playground from "./routes/Playground.svelte";
  import PlaygroundStub from "./routes/PlaygroundStub.svelte";
  import { enableAPIEvents } from "./stores/api";
  import { authReady, authRequired, initializeAuth, isAuthenticated } from "./stores/auth";
  import { initScreenWidth, isDarkMode, appTitle, connectionState } from "./stores/theme";
  import { currentRoute } from "./stores/route";

  const routes = {
    "/": PlaygroundStub,
    "/models": Models,
    "/logs": LogViewer,
    "/activity": Activity,
    "*": PlaygroundStub,
  };

  function handleRouteLoaded(event: { detail: { route: string | RegExp } }) {
    const route = event.detail.route;
    currentRoute.set(typeof route === "string" ? route : "/");
  }

  function shouldRenderApp(): boolean {
    return $authReady && (!$authRequired || $isAuthenticated);
  }

  $effect(() => {
    document.documentElement.setAttribute("data-theme", $isDarkMode ? "dark" : "light");
  });

  $effect(() => {
    const icon = $connectionState === "connecting" ? "\u{1F7E1}" : $connectionState === "connected" ? "\u{1F7E2}" : "\u{1F534}";
    document.title = `${icon} ${$appTitle}`;
  });

  $effect(() => {
    enableAPIEvents(shouldRenderApp());
  });

  onMount(() => {
    const cleanupScreenWidth = initScreenWidth();
    void initializeAuth();

    return () => {
      cleanupScreenWidth();
      enableAPIEvents(false);
    };
  });
</script>

{#if !$authReady}
  <div class="min-h-screen bg-background text-foreground flex items-center justify-center p-4">
    <div class="text-sm text-gray-600 dark:text-gray-300">Loading UI...</div>
  </div>
{:else if !$authRequired || $isAuthenticated}
  <div class="flex flex-col h-screen">
    <Header />

    <main class="flex-1 overflow-auto p-4">
      <div class="h-full" class:hidden={$currentRoute !== "/"}>
        <Playground />
      </div>
      <div class="h-full" class:hidden={$currentRoute === "/"}>
        <Router {routes} on:routeLoaded={handleRouteLoaded} />
      </div>
    </main>
  </div>
{:else}
  <LoginForm />
{/if}
