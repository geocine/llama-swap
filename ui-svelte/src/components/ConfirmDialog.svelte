<script lang="ts">
  import { tick } from "svelte";
  import { X } from "lucide-svelte";
  import { confirmState, resolveConfirm } from "../stores/confirm";

  let dialogEl: HTMLDialogElement | undefined = $state();
  let confirmBtnEl: HTMLButtonElement | undefined = $state();

  $effect(() => {
    if (!dialogEl) return;
    if ($confirmState.open) {
      if (!dialogEl.open) {
        dialogEl.showModal();
        // Move focus to the confirm button so Enter immediately accepts.
        tick().then(() => confirmBtnEl?.focus());
      }
    } else if (dialogEl.open) {
      dialogEl.close();
    }
  });

  function accept() {
    resolveConfirm(true);
  }

  function dismiss() {
    resolveConfirm(false);
  }

  function handleKeydown(event: KeyboardEvent) {
    // <dialog> already handles Escape via the cancel event below; keep
    // Enter as an explicit accept so it works even when focus is elsewhere.
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      accept();
    }
  }

  function handleBackdropClick(event: MouseEvent) {
    if (event.target === dialogEl) {
      dismiss();
    }
  }
</script>

<dialog
  bind:this={dialogEl}
  oncancel={(e) => {
    e.preventDefault();
    dismiss();
  }}
  onclose={dismiss}
  onclick={handleBackdropClick}
  onkeydown={handleKeydown}
  class="confirm-dialog bg-surface text-txtmain rounded-sm shadow-2xl w-full max-w-md p-0 backdrop:bg-black/70 m-auto border border-border outline-none focus:outline-none"
>
  {#if $confirmState.open}
    <div class="flex flex-col">
      <div class="flex items-center justify-between border-b border-border px-4 py-3">
        <h2 class="text-sm font-bold uppercase tracking-wide">
          {$confirmState.title}
        </h2>
        <button
          type="button"
          onclick={dismiss}
          title="Close"
          aria-label="Close"
          class="inline-flex h-7 w-7 items-center justify-center rounded-sm text-txtsecondary outline-none transition-colors duration-150 hover:bg-secondary hover:text-white focus:outline-none focus-visible:bg-secondary focus-visible:text-white"
        >
          <X class="h-4 w-4" />
        </button>
      </div>

      <div class="px-4 py-4">
        <p class="break-words text-sm text-txtmain">
          {$confirmState.message}
        </p>
      </div>

      <div class="flex justify-end gap-2 border-t border-border px-4 py-3">
        <button
          type="button"
          class="btn"
          onclick={dismiss}
        >
          {$confirmState.cancelLabel}
        </button>
        <button
          type="button"
          bind:this={confirmBtnEl}
          class="btn {$confirmState.danger ? 'btn-danger' : 'btn-primary'}"
          onclick={accept}
        >
          {$confirmState.confirmLabel}
        </button>
      </div>
    </div>
  {/if}
</dialog>

<style>
  /* Tame the default <dialog> backdrop so it animates with the rest of the
     overlay rather than snapping in at full opacity. */
  .confirm-dialog::backdrop {
    transition: background-color 120ms ease;
  }
</style>
