import { defineStore } from "pinia";

export type ToastSeverity = "success" | "error" | "info";

export interface Toast {
  id: number;
  message: string;
  severity: ToastSeverity;
}

const DEFAULT_TIMEOUT_MS = 4000;

let nextId = 0;

export const useToastStore = defineStore("toast", {
  state: (): { items: Toast[] } => ({
    items: [],
  }),

  actions: {
    show(
      message: string,
      severity: ToastSeverity = "info",
      timeout: number = DEFAULT_TIMEOUT_MS
    ): number {
      const id = nextId++;
      this.items.push({ id, message, severity });

      if (timeout > 0) {
        window.setTimeout(() => this.dismiss(id), timeout);
      }

      return id;
    },

    dismiss(id: number) {
      const index = this.items.findIndex((item) => item.id === id);
      if (index !== -1) {
        this.items.splice(index, 1);
      }
    },

    clear() {
      this.items = [];
    },
  },
});
