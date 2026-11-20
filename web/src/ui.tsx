import { createContext, useContext, useEffect, useEffectEvent, useRef, useState, useSyncExternalStore, type ButtonHTMLAttributes, type ReactNode } from "react";
import { Link, useNavigate } from "react-router";
import { ApiError, type Company, type JobState, type Logo, type Status } from "./api";
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
  keyboard: <><rect x="2" y="5" width="20" height="14" rx="2" /><path d="M6 9h.01M10 9h.01M14 9h.01M18 9h.01M6 13h.01M18 13h.01M9 15h6" /></>,
  filter: <path d="M3 5h18M7 12h10M10 19h4" />,
  globe: <><circle cx="12" cy="12" r="10" /><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20M2 12h20" /></>,
  link: <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />,
  linkedin: <><path d="M16 8a6 6 0 0 1 6 6v7h-4v-7a2 2 0 0 0-4 0v7h-4v-7a6 6 0 0 1 6-6z" /><rect width="4" height="12" x="2" y="9" /><circle cx="4" cy="4" r="2" /></>,
  xLogo: <path d="M4 4l11.7 16H20L8.3 4zM4 20l6.8-6.8m2.4-2.4L20 4" />,
  github: <><path d="M15 22v-4a4.8 4.8 0 0 0-1-3.5c3 0 6-2 6-5.5.08-1.25-.27-2.48-1-3.5.28-1.15.28-2.35 0-3.5 0 0-1 0-3 1.5-2.64-.5-5.36-.5-8 0C6 2 5 2 5 2c-.3 1.15-.3 2.35 0 3.5A5.4 5.4 0 0 0 4 9c0 3.5 3 5.5 6 5.5-.39.49-.68 1.05-.85 1.65-.17.6-.22 1.23-.15 1.85v4" /><path d="M9 18c-4.51 2-5-2-7-2" /></>,
  youtube: <><path d="M2.5 17a24 24 0 0 1 0-10 2 2 0 0 1 1.4-1.4 49.6 49.6 0 0 1 16.2 0A2 2 0 0 1 21.5 7a24 24 0 0 1 0 10 2 2 0 0 1-1.4 1.4 49.6 49.6 0 0 1-16.2 0A2 2 0 0 1 2.5 17" /><path d="m10 15 5-3-5-3z" /></>,
  facebook: <path d="M18 2h-3a5 5 0 0 0-5 5v3H7v4h3v8h4v-8h3l1-4h-4V7a1 1 0 0 1 1-1h3z" />,
  briefcase: <><path d="M16 20V4a2 2 0 0 0-2-2h-4a2 2 0 0 0-2 2v16" /><rect width="20" height="14" x="2" y="6" rx="2" /></>,
  tag: <><path d="M12.6 2.6A2 2 0 0 0 11.2 2H4a2 2 0 0 0-2 2v7.2a2 2 0 0 0 .6 1.4l8.7 8.7a2.4 2.4 0 0 0 3.4 0l6.6-6.6a2.4 2.4 0 0 0 0-3.4z" /><circle cx="7.5" cy="7.5" r=".5" /></>,
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
    "border-transparent bg-primary text-primary-fg shadow-button hover:bg-primary-hover disabled:bg-subtle disabled:text-faint disabled:shadow-none",
  secondary: "border-line-strong bg-surface text-fg hover:border-line-hover hover:bg-subtle disabled:text-faint",
  ghost: "border-transparent text-muted hover:bg-subtle hover:text-fg disabled:text-faint",
  danger: "border-line-strong bg-surface text-danger hover:bg-danger-soft disabled:opacity-60",
};

type ButtonLook = { variant?: keyof typeof buttonStyles; size?: "sm" | "md" };

export function buttonClass({ variant = "secondary", size = "md" }: ButtonLook = {}) {
  return cx(
    "inline-flex shrink-0 items-center justify-center gap-1.5 rounded-control border font-medium whitespace-nowrap transition-colors active:translate-y-px disabled:pointer-events-none",
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
      K
    </span>
  );
}

const fieldBase =
  "h-8 min-w-0 rounded-control border border-line-strong bg-surface px-2.5 text-sm text-fg transition-colors placeholder:text-faint hover:border-line-hover focus:border-fg focus:ring-3 focus:ring-halo focus:outline-none focus-visible:outline-none disabled:opacity-60";
export const inputClass = cx(fieldBase, "w-full");
export const selectClass = cx(fieldBase, "select-chevron pr-7");
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

/**
 * A destructive action that asks once, in place: the first click arms it, the
 * second runs it, and Escape or leaving disarms it. Both faces share one grid
 * cell, so arming never changes the button's width.
 */
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
  return (
    <Button
      size="sm"
      variant={armed ? "danger" : "ghost"}
      aria-label={armed ? confirmLabel : label}
      title={armed ? undefined : label}
      data-armed={armed || undefined}
      disabled={disabled}
      onBlur={() => setArmed(false)}
      onKeyDown={(e) => {
        if (e.key === "Escape" && armed) {
          e.preventDefault();
          e.stopPropagation();
          setArmed(false);
        }
      }}
      onClick={() => (armed ? (setArmed(false), onConfirm()) : setArmed(true))}
      className={cx("grid", className)}
    >
      <span className={cx("col-start-1 row-start-1 flex items-center justify-center gap-1.5", armed && "invisible")}>{children}</span>
      <span className={cx("col-start-1 row-start-1", !armed && "invisible")}>{confirmLabel}</span>
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

function Initials({ label, size, className }: { label: string; size: number; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cx("inline-flex shrink-0 items-center justify-center bg-subtle font-medium tracking-normal text-muted ring-1 ring-line ring-inset select-none", className)}
      style={{ width: size, height: size, fontSize: Math.max(10, Math.round(size * 0.38)) }}
    >
      {initials(label)}
    </span>
  );
}

export function Avatar({ src, label, size = 28 }: { src?: string; label: string; size?: number }) {
  const [failedSrc, setFailedSrc] = useState<string>();
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
        className="shrink-0 rounded-full bg-subtle object-cover ring-1 ring-line"
        style={{ width: size, height: size }}
      />
    );
  }
  return <Initials label={label} size={size} className="rounded-full" />;
}

/**
 * How a company tile shows a logo in one theme. cover fills the tile edge to
 * edge; contain pads it. A contrast tile is the primary colour, near-black in
 * light mode and near-white in dark, for art drawn for the other theme.
 */
export interface LogoChoice {
  url: string;
  fit: "cover" | "contain";
  tile: "plain" | "contrast";
}

/**
 * The logo a square tile shows in a theme: an icon with its own background
 * fills the tile; otherwise the first icon whose art reads on this theme;
 * otherwise one that reads only on the other theme, on a contrasting tile;
 * then wordmarks the same way. A wordmark more than three times wider than
 * tall is unreadable at tile size, so initials stand in for it.
 */
export function pickLogo(logos: Logo[], theme: "light" | "dark"): LogoChoice | null {
  const readsOn = (l: Logo, t: "light" | "dark") => (t === "light" ? l.on_light : l.on_dark);
  const other = theme === "light" ? "dark" : "light";
  for (const kind of ["icon", "wordmark"] as const) {
    const usable = logos.filter((l) => l.kind === kind && !(kind === "wordmark" && l.aspect > 3));
    const own = usable.find((l) => l.own_background);
    if (own) return { url: own.url, fit: kind === "icon" ? "cover" : "contain", tile: "plain" };
    const here = usable.find((l) => readsOn(l, theme));
    if (here) return { url: here.url, fit: "contain", tile: "plain" };
    const there = usable.find((l) => readsOn(l, other));
    if (there) return { url: there.url, fit: "contain", tile: "contrast" };
  }
  return null;
}

/**
 * A company's logo in a square tile, chosen per theme by pickLogo. Both
 * themes' choices render and CSS shows the one for the current theme, so
 * switching themes swaps the logo without a re-render. Initials stand in
 * when there is no logo or the image fails.
 */
export function CompanyLogo({ company, size }: { company: Pick<Company, "logos" | "logo_url" | "name" | "domain">; size: number }) {
  const label = company.name || company.domain;
  // Companies enriched before logos existed have only logo_url, of unknown colours.
  const logos: Logo[] = company.logos?.length
    ? company.logos
    : company.logo_url
      ? [{ url: company.logo_url, kind: "icon", own_background: false, on_light: true, on_dark: true, aspect: 0, width: 0 }]
      : [];
  const light = pickLogo(logos, "light");
  const dark = pickLogo(logos, "dark");
  if (JSON.stringify(light) === JSON.stringify(dark)) return <LogoTile choice={light} label={label} size={size} />;
  return (
    <>
      <span className="contents dark:hidden">
        <LogoTile choice={light} label={label} size={size} />
      </span>
      <span className="hidden dark:contents">
        <LogoTile choice={dark} label={label} size={size} />
      </span>
    </>
  );
}

function LogoTile({ choice, label, size }: { choice: LogoChoice | null; label: string; size: number }) {
  const [failedSrc, setFailedSrc] = useState<string>();
  if (!choice || choice.url === failedSrc) return <Initials label={label} size={size} className="rounded-control" />;
  return (
    <span
      className={cx("inline-flex shrink-0 overflow-hidden rounded-control ring-1 ring-line", choice.tile === "contrast" ? "bg-primary" : "bg-surface")}
      style={{ width: size, height: size, padding: choice.fit === "cover" ? 0 : Math.round(size * 0.12) }}
    >
      <img
        src={choice.url}
        alt=""
        loading="lazy"
        referrerPolicy="no-referrer"
        onError={() => setFailedSrc(choice.url)}
        className={cx("size-full", choice.fit === "cover" ? "object-cover" : "object-contain")}
      />
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

export function Badge({
  tone = "neutral",
  children,
  title,
  className,
  onRemove,
  removeLabel,
}: {
  tone?: Tone;
  children: ReactNode;
  title?: string;
  className?: string;
  onRemove?: () => void;
  removeLabel?: string;
}) {
  return (
    <span
      title={title}
      className={cx("inline-flex h-5 max-w-full items-center gap-1 rounded-control px-1.5 text-xs font-medium whitespace-nowrap", onRemove && "pr-0.5", tones[tone], className)}
    >
      {children}
      {onRemove && (
        <button type="button" aria-label={removeLabel} onClick={onRemove} className="grid size-4 shrink-0 place-items-center rounded-inner transition-colors hover:bg-subtle-strong hover:text-fg">
          <Icon name="x" className="size-3" />
        </button>
      )}
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

/**
 * A record's enrichment state, when it tells the reader something. Enriched
 * records say nothing, and so does a queued record while enrichment is off or
 * paused: the app shell says that once, for every record.
 */
export function EnrichmentBadge({ state, status }: { state: JobState; status: Status | undefined }) {
  if (state === "done") return null;
  if (state === "pending" && (!status?.enrichment_enabled || status.enrichment_halt)) return null;
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

export function Skeleton({ className }: { className?: string }) {
  return <span aria-hidden="true" className={cx("block animate-pulse rounded-control bg-subtle", className)} />;
}

/** Stands in for list rows while the first page loads, at the rows' own height. */
export function ListSkeleton({ rows = 8, square }: { rows?: number; square?: boolean }) {
  return (
    <div role="status" aria-label="Loading" className="divide-y divide-line border-b border-line">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex h-14 items-center gap-3 px-4 md:h-12 md:px-5" style={{ opacity: 1 - i * 0.1 }}>
          <Skeleton className={cx("size-7 shrink-0", square ? "rounded-control" : "rounded-full")} />
          <div className="min-w-0 flex-1 space-y-1.5">
            <Skeleton className="h-3 w-40 max-w-[60%]" />
            <Skeleton className="h-2.5 w-28 max-w-[40%]" />
          </div>
          <Skeleton className="hidden h-3 w-24 md:block" />
          <Skeleton className="h-3 w-14" />
        </div>
      ))}
    </div>
  );
}

/** A thin indeterminate bar along the bottom edge of its positioned parent. */
export function ProgressBar({ active }: { active: boolean }) {
  return (
    <span aria-hidden="true" className={cx("pointer-events-none absolute inset-x-0 -bottom-px h-0.5 overflow-hidden transition-opacity duration-300", active ? "opacity-100 delay-150" : "opacity-0")}>
      <span className="block h-full w-2/5 animate-[progress_1.1s_ease-in-out_infinite] bg-fg/50" />
    </span>
  );
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

export function Kbd({ children, inverse, large }: { children: ReactNode; inverse?: boolean; large?: boolean }) {
  return (
    <kbd
      className={cx(
        "inline-grid place-items-center rounded-inner border px-1 font-mono",
        large ? "h-5 min-w-5 text-xs" : "h-4 min-w-4 text-2xs",
        inverse ? "border-current/25 bg-current/10 text-current/75" : "border-line-strong bg-surface text-muted",
      )}
    >
      {children}
    </kbd>
  );
}

export type Shortcut = { keys: string[]; label: string };

/**
 * A keyboard button in the page header that lists the page's shortcuts, also
 * opened with "?". Hidden on phones, which have no keyboard to use them with.
 */
export function ShortcutsHelp({ shortcuts }: { shortcuts: Shortcut[] }) {
  const [open, setOpen] = useState(false);
  const onKey = useEffectEvent((e: KeyboardEvent) => {
    if (e.key !== "?" || e.metaKey || e.ctrlKey || e.altKey || isTyping(e.target) || document.querySelector("dialog[open]")) return;
    e.preventDefault();
    setOpen(true);
  });
  useEffect(() => {
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  return (
    <>
      <span className="hidden md:contents">
        <Button variant="ghost" size="sm" onClick={() => setOpen(true)} aria-label="Keyboard shortcuts" title="Keyboard shortcuts (?)" className="w-7 px-0">
          <Icon name="keyboard" className="size-4" />
        </Button>
      </span>
      <Dialog open={open} onClose={() => setOpen(false)} title="Keyboard shortcuts">
        <DialogBody>
          <dl className="grid grid-cols-[1fr_auto] gap-x-6 gap-y-2.5 text-sm">
            {[...shortcuts, { keys: ["?"], label: "Show these shortcuts" }].map((s) => (
              <div key={s.label} className="contents">
                <dt className="text-muted">{s.label}</dt>
                <dd className="flex items-center justify-end gap-1">
                  {s.keys.map((k) => (
                    <Kbd key={k} large>{k}</Kbd>
                  ))}
                </dd>
              </div>
            ))}
          </dl>
        </DialogBody>
      </Dialog>
    </>
  );
}

/** Shows "/" at the end of a search box until it is focused or filled. Its input needs the `peer` class. */
export function SearchKbd() {
  return (
    <span aria-hidden className="pointer-events-none absolute top-1/2 right-2 hidden -translate-y-1/2 lg:block peer-focus:lg:hidden peer-[:not(:placeholder-shown)]:lg:hidden">
      <Kbd>/</Kbd>
    </span>
  );
}

/**
 * Opens and closes a native <dialog> as a modal, which traps focus and closes
 * on Escape. React focuses autoFocus elements before the dialog is shown, which
 * does nothing, so the element marked data-autofocus is focused once it is.
 */
export function useModal(open: boolean) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) {
      d.showModal();
      d.querySelector<HTMLElement>("[data-autofocus]")?.focus();
    }
    if (!open && d.open) d.close();
  }, [open]);
  return ref;
}

/**
 * Content mounts on the first opening and stays mounted while the dialog
 * animates out. Each opening remounts it, so forms start empty every time.
 */
export function Dialog({ open, onClose, title, children, wide }: { open: boolean; onClose: () => void; title: string; children: ReactNode; wide?: boolean }) {
  const ref = useModal(open);
  const [opening, setOpening] = useState(open ? 1 : 0);
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setOpening(opening + 1);
  }
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
      {opening > 0 && (
        <div key={opening} className="flex max-h-[calc(100dvh-2rem)] flex-col">
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
  return <div className="relative min-h-0 overflow-auto p-4">{children}</div>;
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
    <header className="flex h-12 shrink-0 items-center gap-3 border-b border-line px-3 md:px-5">
      {openNav && (
        <Button variant="ghost" size="sm" onClick={openNav} aria-label="Open menu" className="-ml-1 w-8 px-0">
          <Icon name="menu" />
        </Button>
      )}
      <h1 className="min-w-0 flex-1 truncate text-sm font-medium">{title}</h1>
      {children && <div className="flex shrink-0 items-center gap-1.5">{children}</div>}
    </header>
  );
}

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

/** Where a keypress belongs to the control, not to page shortcuts. */
export function isTyping(t: EventTarget | null): boolean {
  if (!(t instanceof HTMLElement)) return false;
  return t.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName);
}

/**
 * Escape goes back to a list, unless something else owns it: an open dialog,
 * a field being typed in, or an armed ConfirmButton.
 */
export function useEscapeTo(path: string) {
  const navigate = useNavigate();
  const onKey = useEffectEvent((e: KeyboardEvent) => {
    if (e.key !== "Escape" || e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return;
    if (document.querySelector("dialog[open]") || isTyping(e.target) || (e.target as Element).closest?.("[data-armed]")) return;
    void navigate(path, { viewTransition: true });
  });
  useEffect(() => {
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
}

/** The shortcuts every list page shares (see useRowNav). */
export const listShortcuts = (noun: string): Shortcut[] => [
  { keys: ["/"], label: "Search" },
  { keys: ["j", "k"], label: "Move down and up" },
  { keys: ["↵"], label: `Open the ${noun}` },
  { keys: ["Esc"], label: "Back to the list" },
  { keys: ["n"], label: `New ${noun}` },
];
