<script lang="ts">
  import { authError, authSubmitting, login } from "../stores/auth";

  let password = $state("");

  async function handleSubmit(): Promise<void> {
    if (!password.trim()) {
      return;
    }

    const success = await login(password);
    if (success) {
      password = "";
    }
  }
</script>

<div class="min-h-screen bg-background flex items-center justify-center p-4">
  <form
    class="w-full max-w-sm rounded-sm border border-border bg-surface p-8"
    onsubmit={(event) => {
      event.preventDefault();
      void handleSubmit();
    }}
  >
    <div class="space-y-2 mb-8">
      <h1 class="text-xl font-bold uppercase tracking-wide pb-0">llama-swap</h1>
      <p class="text-xs text-txtsecondary">
        Enter the UI password configured in your environment.
      </p>
    </div>

    <label class="block text-[10px] font-bold uppercase tracking-widest text-txtsecondary mb-2" for="ui-password">Password</label>
    <input
      id="ui-password"
      bind:value={password}
      type="password"
      autocomplete="current-password"
      class="w-full bg-background border border-border rounded-sm px-3 py-2 text-sm text-txtmain outline-none focus:border-border-hover placeholder-zinc-700 transition-colors duration-150"
      placeholder="Enter password"
    />

    {#if $authError}
      <p class="mt-3 text-xs text-error">{$authError}</p>
    {/if}

    <button
      type="submit"
      class="mt-6 w-full rounded-sm bg-white px-4 py-2 text-xs font-bold uppercase tracking-wide text-black transition-colors duration-150 hover:bg-zinc-200 disabled:cursor-not-allowed disabled:opacity-40 active:scale-[0.99]"
      disabled={$authSubmitting || !password.trim()}
    >
      {$authSubmitting ? "Signing in..." : "Sign in"}
    </button>
  </form>
</div>
