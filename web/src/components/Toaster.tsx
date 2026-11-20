import { createContext, useContext, useState, type ReactNode } from "react";
import { cx, Icon } from "../ui";

type Tone = "done" | "error";
interface Toast {
  id: number;
  message: string;
  tone: Tone;
  leaving: boolean;
}

const ToastContext = createContext<(message: string, tone?: Tone) => void>(() => {});

/** Confirms something that happened out of view, or a save that failed after the screen moved on. */
export const useToast = () => useContext(ToastContext);

const shownFor = { done: 3500, error: 6000 };
let lastId = 0;

export function Toaster({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [toast] = useState(() => (message: string, tone: Tone = "done") => {
    const id = ++lastId;
    setToasts((all) => [...all.slice(-2), { id, message, tone, leaving: false }]);
    window.setTimeout(() => setToasts((all) => all.map((t) => (t.id === id ? { ...t, leaving: true } : t))), shownFor[tone]);
    window.setTimeout(() => setToasts((all) => all.filter((t) => t.id !== id)), shownFor[tone] + 200);
  });
  return (
    <ToastContext.Provider value={toast}>
      {children}
      <div role="status" aria-live="polite" className="pointer-events-none fixed inset-x-0 bottom-0 z-50 flex flex-col items-center gap-2 p-4 sm:items-end sm:p-5">
        {toasts.map((t) => (
          <div
            key={t.id}
            className={cx(
              "pointer-events-auto flex max-w-sm items-start gap-2 rounded-surface bg-surface px-3 py-2 text-sm text-fg shadow-floating transition-[opacity,translate] duration-200 ease-out starting:translate-y-2 starting:opacity-0",
              t.leaving && "translate-y-2 opacity-0",
            )}
          >
            <Icon name={t.tone === "error" ? "alert" : "check"} className={cx("mt-0.5 size-3.5", t.tone === "error" ? "text-danger" : "text-success")} />
            <span className="min-w-0 [overflow-wrap:anywhere]">{t.message}</span>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}
