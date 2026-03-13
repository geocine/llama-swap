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

<div class="min-h-screen bg-background text-foreground flex items-center justify-center p-4">
  <form
    class="w-full max-w-md rounded-xl border border-border bg-surface p-6 shadow-sm"
    onsubmit={(event) => {
      event.preventDefault();
      void handleSubmit();
    }}
  >
    <div class="space-y-2">
      <h1 class="text-2xl font-semibold">llama-swap</h1>
      <p class="text-sm text-gray-600 dark:text-gray-300">
        Enter the UI password configured in your environment.
      </p>
    </div>

    <label class="mt-6 block text-sm font-medium" for="ui-password">Password</label>
    <input
      id="ui-password"
      bind:value={password}
      type="password"
      autocomplete="current-password"
      class="mt-2 w-full rounded-lg border border-border bg-background px-3 py-2 outline-none focus:ring-2 focus:ring-indigo-500"
      placeholder="Enter password"
    />

    {#if $authError}
      <p class="mt-3 text-sm text-red-600 dark:text-red-400">{$authError}</p>
    {/if}

    <button
      type="submit"
      class="mt-6 w-full rounded-lg bg-indigo-600 px-4 py-2 font-medium text-white transition hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-60"
      disabled={$authSubmitting || !password.trim()}
    >
      {$authSubmitting ? "Signing in..." : "Sign in"}
    </button>
  </form>
</div>
