import { createContext, useContext, useEffect, useRef, useState, useSyncExternalStore, type ButtonHTMLAttributes, type ReactNode } from "react";
import { Link } from "react-router";
import { ApiError, type JobState } from "./api";
import { absoluteTime, initials, relativeTime } from "./format";

export const cx = (...parts: (string | false | null | undefined)[]) => parts.filter(Boolean).join(" ");

const paths = {
  search: <><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></>,
  users: <><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" /><circle cx="9" cy="7" r="4" /><path d="M22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" /></>,
  building: <><rect x="4" y="2" width="16" height="20" rx="2" /><path d="M9 22v-4h6v4M8 6h.01M12 6h.01M16 6h.01M8 10h.01M12 10h.01M16 10h.01M8 14h.01M12 14h.01M16 14h.01" /></>,
  settings: <path d="M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6" />,
  pin: <><path d="M12 17v5" /><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z" /></>,
  reset: <><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" /><path d="M3 3v5h5" /></>,
  copy: <><rect x="8" y="8" width="14" height="14" rx="2" /><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" /></>,
  check: <path d="M20 6 9 17l-5-5" />,
  x: <path d="M18 6 6 18M6 6l12 12" />,
  plus: <path d="M12 5v14M5 12h14" />,
  up: <path d="m18 15-6-6-6 6" />,
  down: <path d="m6 9 6 6 6-6" />,
  left: <path d="m15 18-6-6 6-6" />,
  right: <path d="m9 18 6-6-6-6" />,
  sun: <><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" /></>,
  moon: <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />,
  monitor: <><rect x="2" y="3" width="20" height="14" rx="2" /><path d="M8 21h8M12 17v4" /></>,
  logout: <><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" /><path d="m16 17 5-5-5-5M21 12H9" /></>,
  merge: <><path d="m8 6 4-4 4 4" /><path d="M12 2v10.3a4 4 0 0 1-1.17 2.87L4 22M20 22l-5-5" /></>,
  refresh: <><path d="M21 12a9 9 0 1 1-3-6.7L21 8" /><path d="M21 3v5h-5" /></>,
  trash: <path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />,
  external: <path d="M15 3h6v6M10 14 21 3M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />,
  key: <><circle cx="7.5" cy="15.5" r="5.5" /><path d="m21 2-9.6 9.6M15.5 7.5l3 3L22 7l-3-3" /></>,
  bot: <><rect x="3" y="11" width="18" height="10" rx="2" /><circle cx="12" cy="5" r="2" /><path d="M12 7v4M8 16h.01M16 16h.01" /></>,
  plug: <path d="M12 22v-5M9 8V2M15 8V2M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8Z" />,
  layers: <><path d="m12 2 10 5-10 5L2 7z" /><path d="m2 17 10 5 10-5M2 12l10 5 10-5" /></>,
  sparkles: <path d="M12 3l1.9 5.8L20 10.5l-6.1 1.7L12 18l-1.9-5.8L4 10.5l6.1-1.7z" />,
  alert: <><circle cx="12" cy="12" r="10" /><path d="M12 8v4M12 16h.01" /></>,
  note: <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />,
  mail: <><rect x="2" y="4" width="20" height="16" rx="2" /><path d="m22 7-10 5L2 7" /></>,
  pencil: <path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z" />,
  login: <><path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4" /><path d="m10 17 5-5-5-5M15 12H3" /></>,
  menu: <path d="M4 6h16M4 12h16M4 18h16" />,
  filter: <path d="M3 5h18M7 12h10M10 19h4" />,
};
export type IconName = keyof typeof paths;

export function Icon({ name, className = "size-4" }: { name: IconName; className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.75} strokeLinecap="round" strokeLinejoin="round" className={cx("shrink-0", className)} aria-hidden="true">
      {paths[name]}
    </svg>
  );
}

const buttonStyles = {
  primary:
    "border-transparent bg-primary text-primary-fg shadow-[inset_0_1px_0_rgb(255_255_255/0.12),0_1px_2px_rgb(0_0_0/0.08)] hover:bg-primary-hover disabled:bg-subtle disabled:text-faint disabled:shadow-none",
  secondary: "border-line-strong bg-surface text-fg hover:border-line-hover hover:bg-subtle disabled:text-faint",
  ghost: "border-transparent text-muted hover:bg-subtle hover:text-fg disabled:text-faint",
  danger: "border-line-strong bg-surface text-danger hover:bg-danger-soft disabled:opacity-60",
};

type ButtonLook = { variant?: keyof typeof buttonStyles; size?: "sm" | "md" };

/** Button classes, for a <Link> that should look like a button. */
export function buttonClass({ variant = "secondary", size = "md" }: ButtonLook = {}) {
  return cx(
    "inline-flex shrink-0 items-center justify-center gap-1.5 rounded-control border font-medium whitespace-nowrap transition-colors disabled:pointer-events-none",
    size === "sm" ? "h-7 px-2 text-xs" : "h-8 px-3 text-sm",
    buttonStyles[variant],
  );
}

export function Button({ variant, size, className, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & ButtonLook) {
  return <button type="button" {...props} className={cx(buttonClass({ variant, size }), className)} />;
}

export function Logo({ className = "size-6 text-xs" }: { className?: string }) {
  return (
    <span aria-hidden="true" className={cx("grid shrink-0 place-items-center rounded-control bg-primary font-bold text-primary-fg", className)}>
      u
    </span>
  );
}

const fieldBase =
  "h-8 min-w-0 rounded-control border border-line-strong bg-surface px-2.5 text-sm text-fg transition-colors placeholder:text-faint hover:border-line-hover focus:border-fg focus:ring-3 focus:ring-halo focus:outline-none focus-visible:outline-none disabled:opacity-60";
export const inputClass = cx(fieldBase, "w-full");
export const selectClass = cx(fieldBase, "pr-7");
export const labelClass = "mb-1.5 block text-xs font-medium text-muted";

export function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="block">
      <span className={labelClass}>{label}</span>
      {children}
      {hint && <span className="mt-1 block text-xs text-faint">{hint}</span>}
    </label>
  );
}

/** A destructive action that asks once, in place: the first click arms it, the second runs it. */
export function ConfirmButton({
  label,
  confirmLabel,
  onConfirm,
  children,
  className,
  disabled,
}: {
  label: string;
  confirmLabel: string;
  onConfirm: () => void;
  children: ReactNode;
  className?: string;
  disabled?: boolean;
}) {
  const [armed, setArmed] = useState(false);
  if (armed) {
    return (
      <Button size="sm" variant="danger" autoFocus disabled={disabled} onBlur={() => setArmed(false)} onClick={() => (setArmed(false), onConfirm())} className={className}>
        {confirmLabel}
      </Button>
    );
  }
  return (
    <Button size="sm" variant="ghost" aria-label={label} title={label} disabled={disabled} onClick={() => setArmed(true)} className={className}>
      {children}
    </Button>
  );
}

export function useCopy(): [boolean, (text: string) => void] {
  const [copied, setCopied] = useState(false);
  const timer = useRef<number>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const copy = (text: string) => {
    void navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setCopied(false), 1500);
    });
  };
  return [copied, copy];
}

export function CopyButton({ text, label = "Copy", className }: { text: string; label?: string; className?: string }) {
  const [copied, copy] = useCopy();
  return (
    <Button size="sm" variant="ghost" onClick={() => copy(text)} className={className} aria-label={copied ? "Copied" : label}>
      <Icon name={copied ? "check" : "copy"} className={cx("size-3.5", copied && "text-success")} />
      <span aria-live="polite">{copied ? "Copied" : label}</span>
    </Button>
  );
}

export function CopyField({ value, label }: { value: string; label?: string }) {
  return (
    <div className="flex min-w-0 items-center gap-1 rounded-control border border-line bg-header pl-2.5">
      <code className="min-w-0 flex-1 truncate py-1.5 font-mono text-xs" title={value}>
        {value}
      </code>
      <CopyButton text={value} label={label} />
    </div>
  );
}

export function CodeBlock({ code, label }: { code: string; label?: string }) {
  const pre = (
    <pre className="min-w-0 flex-1 px-3 py-2.5 font-mono text-xs leading-relaxed whitespace-pre-wrap [overflow-wrap:anywhere]">
      <code>{code}</code>
    </pre>
  );
  if (!label) {
    return (
      <div className="flex items-start rounded-surface border border-line bg-header">
        {pre}
        <CopyButton text={code} className="m-1.5" />
      </div>
    );
  }
  return (
    <div className="rounded-surface border border-line bg-header">
      <div className="flex items-center justify-between gap-2 border-b border-line py-1 pr-1.5 pl-3 text-xs font-medium text-muted">
        <span className="min-w-0 truncate">{label}</span>
        <CopyButton text={code} />
      </div>
      {pre}
    </div>
  );
}

/** Initials on a neutral tile; a photo or logo replaces it when one loads. */
export function Avatar({ src, label, size = 28, square }: { src?: string; label: string; size?: number; square?: boolean }) {
  const [failedSrc, setFailedSrc] = useState<string>();
  const shape = square ? "rounded-control" : "rounded-full";
  if (src && src !== failedSrc) {
    return (
      <img
        src={src}
        alt=""
        width={size}
        height={size}
        loading="lazy"
        referrerPolicy="no-referrer"
        onError={() => setFailedSrc(src)}
        className={cx(shape, "shrink-0 bg-subtle object-cover ring-1 ring-line")}
        style={{ width: size, height: size }}
      />
    );
  }
  return (
    <span
      aria-hidden="true"
      className={cx(shape, "inline-flex shrink-0 items-center justify-center bg-subtle font-medium tracking-normal text-muted ring-1 ring-line ring-inset select-none")}
      style={{ width: size, height: size, fontSize: Math.max(10, Math.round(size * 0.38)) }}
    >
      {initials(label)}
    </span>
  );
}

const tones = {
  neutral: "bg-subtle text-muted",
  outline: "border border-line-strong text-muted",
  success: "bg-success-soft text-success",
  warn: "bg-warn-soft text-warn",
  danger: "bg-danger-soft text-danger",
  info: "bg-info-soft text-info",
};
export type Tone = keyof typeof tones;

export function Badge({ tone = "neutral", children, title, className }: { tone?: Tone; children: ReactNode; title?: string; className?: string }) {
  return (
    <span title={title} className={cx("inline-flex h-5 max-w-full items-center gap-1 rounded-control px-1.5 text-xs font-medium whitespace-nowrap", tones[tone], className)}>
      {children}
    </span>
  );
}

export const enrichmentStates: Record<JobState, { label: string; tone: Tone; help: string }> = {
  pending: { label: "Queued", tone: "neutral", help: "Waiting for the enrichment worker." },
  running: { label: "Enriching", tone: "info", help: "context.dev is looking this up now." },
  done: { label: "Enriched", tone: "success", help: "Filled in from context.dev." },
  failed: { label: "Enrichment failed", tone: "danger", help: "context.dev lookups failed after retries." },
  skipped: { label: "Not enriched", tone: "neutral", help: "Free-mail addresses are not enriched." },
};

export function EnrichmentBadge({ state }: { state: JobState }) {
  const s = enrichmentStates[state];
  return (
    <Badge tone={s.tone} title={s.help}>
      {state === "running" && <span className="size-1.5 animate-pulse rounded-full bg-current" />}
      {s.label}
    </Badge>
  );
}

export function RelativeTime({ iso, className }: { iso: string; className?: string }) {
  return (
    <time dateTime={iso} title={absoluteTime(iso)} className={className}>
      {relativeTime(iso)}
    </time>
  );
}

export function Spinner({ className = "size-4" }: { className?: string }) {
  return <span role="status" aria-label="Loading" className={cx("inline-block animate-spin rounded-full border-2 border-line-strong border-t-fg", className)} />;
}

export function PageSpinner() {
  return (
    <div className="grid h-40 place-items-center">
      <Spinner />
    </div>
  );
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return "Something went wrong.";
}

export function ErrorNote({ error }: { error: unknown }) {
  if (!error) return null;
  return (
    <p role="alert" className="flex items-start gap-1.5 rounded-control bg-danger-soft px-2.5 py-1.5 text-xs text-danger">
      <Icon name="alert" className="mt-px size-3.5" />
      <span className="min-w-0 [overflow-wrap:anywhere]">{errorMessage(error)}</span>
    </p>
  );
}

export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="inline-grid h-4 min-w-4 place-items-center rounded border border-line-strong bg-surface px-1 font-mono text-2xs text-muted">{children}</kbd>;
}

/** Opens and closes a native <dialog> as a modal, which traps focus and closes on Escape. */
export function useModal(open: boolean) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);
  return ref;
}

export function Dialog({ open, onClose, title, children, wide }: { open: boolean; onClose: () => void; title: string; children: ReactNode; wide?: boolean }) {
  const ref = useModal(open);
  return (
    <dialog
      ref={ref}
      onClose={onClose}
      onClick={(e) => e.target === ref.current && onClose()}
      aria-label={title}
      className={cx(
        "m-auto max-h-[calc(100dvh-2rem)] w-[calc(100%-2rem)] overflow-hidden rounded-overlay border-0 bg-surface p-0 text-fg shadow-floating",
        wide ? "max-w-xl" : "max-w-md",
      )}
    >
      {open && (
        <div className="flex max-h-[calc(100dvh-2rem)] flex-col">
          <div className="flex shrink-0 items-center justify-between gap-4 border-b border-line py-2.5 pr-2.5 pl-4">
            <h2 className="min-w-0 truncate text-base font-semibold">{title}</h2>
            <Button variant="ghost" size="sm" onClick={onClose} aria-label="Close" className="w-7 px-0">
              <Icon name="x" />
            </Button>
          </div>
          {children}
        </div>
      )}
    </dialog>
  );
}

export function DialogBody({ children }: { children: ReactNode }) {
  return <div className="min-h-0 overflow-auto p-4">{children}</div>;
}

export function DialogFooter({ children }: { children: ReactNode }) {
  return <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-t border-line bg-bg px-4 py-3">{children}</div>;
}

export function Card({ children, className }: { children: ReactNode; className?: string }) {
  return <section className={cx("rounded-surface border border-line bg-surface", className)}>{children}</section>;
}

export function EmptyState({ icon, title, children, action }: { icon?: IconName; title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-2 rounded-surface border border-dashed border-line-strong px-4 py-10 text-center">
      {icon && <Icon name={icon} className="mb-1 size-5 text-faint" />}
      <p className="text-base font-semibold">{title}</p>
      {children && <p className="max-w-sm text-sm text-balance text-muted">{children}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

/** Set by the app shell on phones, where the page header carries the menu button. */
export const NavMenu = createContext<(() => void) | null>(null);

export function PageHeader({ title, children }: { title: ReactNode; children?: ReactNode }) {
  const openNav = useContext(NavMenu);
  return (
    <header className="flex h-12 shrink-0 items-center gap-2 border-b border-line px-3 md:px-5">
      {openNav && (
        <Button variant="ghost" size="sm" onClick={openNav} aria-label="Open menu" className="-ml-1 w-8 px-0">
          <Icon name="menu" />
        </Button>
      )}
      <h1 className="min-w-0 flex-1 truncate text-sm font-medium">{title}</h1>
      {children && <div className="flex shrink-0 items-center gap-2">{children}</div>}
    </header>
  );
}

/** "Contacts / Maya Okafor": the parent stays a link, the current page does not. */
export function Crumbs({ parent, to, current }: { parent: string; to: string; current: string }) {
  return (
    <span className="flex min-w-0 items-center gap-2">
      <Link to={to} className="shrink-0 font-normal text-muted transition-colors hover:text-fg">
        {parent}
      </Link>
      <span className="text-faint" aria-hidden="true">
        /
      </span>
      <span className="min-w-0 truncate" aria-current="page">
        {current}
      </span>
    </span>
  );
}

export function useMedia(query: string): boolean {
  return useSyncExternalStore(
    (onChange) => {
      const m = matchMedia(query);
      m.addEventListener("change", onChange);
      return () => m.removeEventListener("change", onChange);
    },
    () => matchMedia(query).matches,
  );
}

/** Tailwind's md breakpoint. Below it the app is laid out for a phone. */
export const useWide = () => useMedia("(min-width: 48rem)");
