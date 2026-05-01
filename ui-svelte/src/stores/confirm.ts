import { writable } from "svelte/store";

export interface ConfirmOptions {
  title?: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
}

export interface ConfirmState extends Required<Omit<ConfirmOptions, "danger">> {
  open: boolean;
  danger: boolean;
  resolve: ((value: boolean) => void) | null;
}

const initial: ConfirmState = {
  open: false,
  title: "Confirm",
  message: "",
  confirmLabel: "OK",
  cancelLabel: "Cancel",
  danger: false,
  resolve: null,
};

export const confirmState = writable<ConfirmState>(initial);

/**
 * Open a custom confirm modal and resolve to true/false.
 *
 * Replaces the native window.confirm with a styled dialog so confirms
 * use the app's dark zinc theme rather than the browser default.
 */
export function confirmDialog(options: ConfirmOptions): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    confirmState.set({
      open: true,
      title: options.title ?? "Confirm",
      message: options.message,
      confirmLabel: options.confirmLabel ?? "OK",
      cancelLabel: options.cancelLabel ?? "Cancel",
      danger: options.danger ?? false,
      resolve,
    });
  });
}

export function resolveConfirm(value: boolean): void {
  confirmState.update((state) => {
    state.resolve?.(value);
    return { ...initial };
  });
}
