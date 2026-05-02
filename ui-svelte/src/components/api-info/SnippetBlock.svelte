<script lang="ts">
  import { Check, Copy } from "lucide-svelte";

  type Props = {
    label: string;
    text: string;
    copyLabel?: string;
  };

  let { label, text, copyLabel = "snippet" }: Props = $props();

  let copied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | null = null;

  async function copy(): Promise<void> {
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
      copied = true;
      if (copyTimer) clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (copied = false), 1500);
    } catch (err) {
      console.error("Failed to copy", err);
    }
  }
</script>

<div>
  <div class="mb-1.5 flex items-center justify-between gap-2">
    <span class="font-mono text-[10px] font-bold uppercase tracking-widest text-txtsecondary">
      {label}
    </span>
    <button
      class="flex cursor-pointer items-center gap-1.5 rounded-[2px] border border-border bg-zinc-950 px-2 py-1 font-mono text-[10px] font-bold uppercase tracking-wider text-txtsecondary transition-colors duration-150 hover:border-border-hover hover:text-white disabled:cursor-not-allowed disabled:opacity-40"
      onclick={copy}
      disabled={!text}
      title={`Copy ${copyLabel}`}
      aria-label={`Copy ${copyLabel}`}
    >
      {#if copied}
        <Check class="h-3 w-3 text-success" />
        Copied
      {:else}
        <Copy class="h-3 w-3" />
        Copy
      {/if}
    </button>
  </div>
  <pre class="overflow-x-auto rounded-[2px] border border-border bg-black px-3 py-2 font-mono text-[11px] leading-relaxed text-txtmain">{text}</pre>
</div>
