<script lang="ts">
  import ChatInterface from "../components/playground/ChatInterface.svelte";
  import ChatSidebar from "../components/playground/ChatSidebar.svelte";
  import { sidebarOpen } from "../stores/playgroundUI";
  import { isNarrow } from "../stores/theme";

  // On narrow screens default to closed regardless of stored preference
  let sidebarVisible = $derived($isNarrow ? false : $sidebarOpen);
</script>

<div class="flex h-full">
  {#if sidebarVisible}
    <div class="w-64 shrink-0">
      <ChatSidebar />
    </div>
  {/if}

  <div class="min-w-0 flex-1">
    <ChatInterface />
  </div>
</div>
