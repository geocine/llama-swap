<script lang="ts">
  import { Eye, EyeOff, Lock, ShieldCheck } from "lucide-svelte";
  import { authError, authSubmitting, login } from "../stores/auth";
  import GlitterCloth from "./effects/glitter-cloth/GlitterCloth.svelte";
  import Logo from "./Logo.svelte";

  let password = $state("");
  let revealed = $state(false);

  async function handleSubmit(): Promise<void> {
    if (!password.trim()) {
      return;
    }

    const success = await login(password);
    if (success) {
      password = "";
      revealed = false;
    }
  }
</script>

<div class="relative min-h-screen bg-background flex items-center justify-center p-4 overflow-hidden">
  <div class="pointer-events-none absolute inset-0" aria-hidden="true">
    <GlitterCloth
      color="#222326"
      speed={1}
      brightness={1}
      blendStrength={0.02}
      noiseScale={4}
      vignetteStrength={50}
      vignettePower={0.5}
    />
  </div>
  <form
    class="relative z-10 w-full max-w-md overflow-hidden rounded-sm border border-border bg-surface/90 backdrop-blur-sm shadow-2xl shadow-black/40"
    onsubmit={(event) => {
      event.preventDefault();
      void handleSubmit();
    }}
  >
    <div class="flex items-center gap-3 px-6 py-5 border-b border-border">
      <span
        class="inline-flex h-9 w-9 items-center justify-center rounded-sm border border-border bg-background text-white"
        aria-hidden="true"
      >
        <Logo size={20} />
      </span>
      <span class="font-mono text-base font-bold tracking-tight text-txtmain">llama-swap</span>
      <span
        class="ml-auto select-none rounded-sm border border-border bg-background px-2 py-1 font-mono text-[9px] font-bold uppercase tracking-widest text-txtsecondary"
        title="geocine edition"
        aria-label="geocine edition"
      >
        geocine edition
      </span>
    </div>

    <div class="px-6 pt-6 pb-6">
      <h1 class="text-3xl font-extrabold uppercase tracking-tight text-txtmain leading-none">
        Login
      </h1>
      <p class="mt-2 text-xs text-txtsecondary">Enter your UI password.</p>

      <label
        for="ui-password"
        class="mt-6 block text-[10px] font-bold uppercase tracking-widest text-txtsecondary"
      >
        Password
      </label>

      <div class="mt-2 relative">
        <span
          class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-txtsecondary"
          aria-hidden="true"
        >
          <Lock class="h-4 w-4" />
        </span>
        <input
          id="ui-password"
          bind:value={password}
          type={revealed ? "text" : "password"}
          autocomplete="current-password"
          class="w-full bg-background border border-border rounded-sm pl-10 pr-10 py-2.5 text-sm text-txtmain outline-none focus:border-border-hover placeholder-zinc-700 transition-colors duration-150"
          placeholder="Enter password"
        />
        <button
          type="button"
          class="absolute inset-y-0 right-0 flex items-center px-3 text-txtsecondary hover:text-txtmain transition-colors duration-150 outline-none focus:outline-none focus-visible:text-txtmain"
          onclick={() => (revealed = !revealed)}
          aria-label={revealed ? "Hide password" : "Show password"}
          tabindex="-1"
        >
          {#if revealed}
            <EyeOff class="h-4 w-4" />
          {:else}
            <Eye class="h-4 w-4" />
          {/if}
        </button>
      </div>

      {#if $authError}
        <p class="mt-3 text-xs text-error">{$authError}</p>
      {/if}

      <button
        type="submit"
        class="mt-5 w-full rounded-sm bg-white px-4 py-2.5 text-xs font-bold uppercase tracking-widest text-black transition-colors duration-150 hover:bg-zinc-200 disabled:cursor-not-allowed disabled:opacity-40 active:scale-[0.99]"
        disabled={$authSubmitting || !password.trim()}
      >
        {$authSubmitting ? "Signing in..." : "Sign in"}
      </button>
    </div>

    <div
      class="flex items-center justify-center gap-1.5 border-t border-border px-6 py-3 text-[11px] text-txtsecondary"
    >
      <ShieldCheck class="h-3.5 w-3.5" />
      <span>Protected local interface</span>
    </div>
  </form>
</div>
