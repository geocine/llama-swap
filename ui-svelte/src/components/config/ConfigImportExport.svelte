<script lang="ts">
  import { FileDown, FileUp } from "lucide-svelte";
  import {
    downloadModelConfigSettings,
    importModelConfigSettings,
  } from "../../stores/api";

  type StatusKind = "info" | "error";

  interface Props {
    /**
     * Called whenever an import or export finishes — successfully or not.
     * Parents render the message wherever it best fits their layout.
     */
    onstatus?: (message: string, kind: StatusKind) => void;
    /**
     * Invoked after a successful import so the parent can refresh derived
     * state (e.g. reloading model lists) before the success message lands.
     */
    onafterimport?: () => void | Promise<void>;
    /** Tailwind class applied to the two buttons. Lets callers match their toolbar. */
    buttonClass?: string;
  }

  let {
    onstatus,
    onafterimport,
    buttonClass = "btn p-2",
  }: Props = $props();

  let fileInput: HTMLInputElement | null = $state(null);
  let busy = $state(false);

  function notify(message: string, kind: StatusKind) {
    onstatus?.(message, kind);
  }

  async function handleExport() {
    busy = true;
    try {
      await downloadModelConfigSettings();
      notify("Exported full YAML config.", "info");
    } catch (err) {
      notify(
        err instanceof Error ? err.message : "Failed to export config",
        "error",
      );
    } finally {
      busy = false;
    }
  }

  async function handleImport(event: Event) {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = "";
    if (!file) return;

    busy = true;
    try {
      const result = await importModelConfigSettings(file);
      await onafterimport?.();
      const count = result.imported.length;
      const skipped = result.skipped.length;
      const noun = `model setting${count === 1 ? "" : "s"}`;
      const tail = skipped > 0 ? `, skipped ${skipped}` : "";
      notify(
        `Imported ${count} ${noun}${tail}. Applies on next load.`,
        "info",
      );
    } catch (err) {
      notify(
        err instanceof Error ? err.message : "Failed to import config",
        "error",
      );
    } finally {
      busy = false;
    }
  }
</script>

<input
  class="hidden"
  type="file"
  accept=".yaml,.yml,text/yaml,application/x-yaml"
  bind:this={fileInput}
  onchange={handleImport}
/>

<button
  type="button"
  class={buttonClass}
  onclick={handleExport}
  disabled={busy}
  title="Export full YAML config (all models)"
  aria-label="Export full YAML config"
>
  <FileDown class="h-4 w-4" />
</button>

<button
  type="button"
  class={buttonClass}
  onclick={() => fileInput?.click()}
  disabled={busy}
  title="Import YAML config (merges all models)"
  aria-label="Import YAML config"
>
  <FileUp class="h-4 w-4" />
</button>
