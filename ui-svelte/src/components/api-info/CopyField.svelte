<script lang="ts">
  import { Check, Copy, Eye, EyeOff } from "lucide-svelte";
  import type { Snippet } from "svelte";

  type Props = {
    label: string;
    value: string;
    /** Optional masked display string — when provided and `secret` is true, shown until user reveals. */
    masked?: string;
    /** When true, the field is treated as sensitive (toggleable reveal). */
    secret?: boolean;
    placeholder?: string;
    monospace?: boolean;
    /** Optional inline controls rendered on the right side of the label row. */
    headerActions?: Snippet;
  };

  let {
    label,
    value,
    masked,
    secret = false,
    placeholder,
    monospace = true,
    headerActions,
  }: Props = $props();

  let revealed = $state(false);
  let copied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | null = null;

  let displayValue = $derived(
    !value
      ? (placeholder ?? "")
      : secret && !revealed
        ? (masked ?? maskValue(value))
        : value
  );

  function maskValue(raw: string): string {
    if (raw.length <= 8) return "•".repeat(Math.max(raw.length, 6));
    return `${raw.slice(0, 4)}${"•".repeat(Math.max(raw.length - 8, 4))}${raw.slice(-4)}`;
  }

  async function copy() {
    if (!value) return;
    try {
      await navigator.clipboard.writeText(value);
      copied = true;
      if (copyTimer) clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (copied = false), 1500);
    } catch (err) {
      console.error("Failed to copy", err);
    }
  }
</script>

<div class="flex flex-col gap-1.5">
  {#if label || headerActions}
    <div class="flex min-h-[22px] items-center justify-between gap-2">
      <span class="font-mono text-[10px] font-bold uppercase tracking-widest text-txtsecondary">
        {label}
      </span>
      {#if headerActions}
        {@render headerActions()}
      {/if}
    </div>
  {/if}
  <div
    class="flex items-stretch overflow-hidden rounded-[2px] border border-border bg-black transition-colors duration-150 focus-within:border-border-hover hover:border-border-hover"
  >
    <div
      class="flex-1 truncate px-3 py-2 text-sm {monospace ? 'font-mono' : ''} {value
        ? 'text-txtmain'
        : 'text-txtmuted'}"
      title={value || placeholder || ""}
    >
      {displayValue}
    </div>
    {#if secret && value}
      <button
        class="flex w-9 shrink-0 cursor-pointer items-center justify-center border-l border-border text-txtsecondary transition-colors duration-150 hover:bg-secondary hover:text-white"
        onclick={() => (revealed = !revealed)}
        title={revealed ? "Hide" : "Reveal"}
        aria-label={revealed ? "Hide value" : "Reveal value"}
      >
        {#if revealed}
          <EyeOff class="h-4 w-4" />
        {:else}
          <Eye class="h-4 w-4" />
        {/if}
      </button>
    {/if}
    <button
      class="flex w-9 shrink-0 cursor-pointer items-center justify-center border-l border-border text-txtsecondary transition-colors duration-150 hover:bg-secondary hover:text-white disabled:cursor-not-allowed disabled:opacity-40"
      onclick={copy}
      disabled={!value}
      title={copied ? "Copied" : "Copy"}
      aria-label="Copy to clipboard"
    >
      {#if copied}
        <Check class="h-4 w-4 text-success" />
      {:else}
        <Copy class="h-4 w-4" />
      {/if}
    </button>
  </div>
</div>
