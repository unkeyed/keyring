import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { Button, cx, errorMessage, Icon } from "../ui";

interface Props {
  label: string;
  value: string;
  /** In manual_fields: a person set it, so sign-ups and enrichment leave it alone. */
  pinned: boolean;
  onSave: (value: string) => Promise<unknown>;
  onReset: () => Promise<unknown>;
  multiline?: boolean;
  /** Shown when the value is empty. */
  placeholder?: string;
  /** Shown in the empty input while editing. */
  editPlaceholder?: string;
  href?: boolean;
  /** How the stored value reads, as "Portugal" for "PT". Editing shows the stored value. */
  format?: (value: string) => string;
  /** Content above the value, as the company a domain belongs to. */
  lead?: ReactNode;
  /** Long text shows four lines, with a toggle for the rest. */
  clamp?: boolean;
  /** Just the value, for a page header; the label names it for screen readers only. */
  bare?: boolean;
  className?: string;
}

/**
 * Read and edit mode share one box: same height, padding and border width,
 * so starting an edit never moves the text or resizes the row.
 */
const box = "w-full min-w-0 rounded-control border px-1.5 py-[3px] text-left text-sm";

/** A property row that edits in place: click, type, Enter saves, Escape cancels. */
export function EditableField({ label, value, pinned, onSave, onReset, multiline, placeholder = "Empty", editPlaceholder, href, format, lead, clamp, bare, className }: Props) {
  const [draft, setDraft] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // Blur is the one place an edit commits; Escape sets this so its blur discards instead.
  const cancelled = useRef(false);
  // An edit finished from the keyboard hands focus back to the value, so Tab carries on from here.
  const refocus = useRef(false);
  const button = useRef<HTMLButtonElement>(null);
  const [expanded, setExpanded] = useState(false);
  const [overflows, setOverflows] = useState(false);

  const run = async (op: () => Promise<unknown>) => {
    setBusy(true);
    setError(null);
    try {
      await op();
      setDraft(null);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const commit = () => {
    if (cancelled.current) {
      cancelled.current = false;
      return;
    }
    if (draft === null) return;
    if (draft.trim() === value) return setDraft(null);
    void run(() => onSave(draft.trim()));
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement | HTMLTextAreaElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      cancelled.current = true;
      refocus.current = true;
      e.currentTarget.blur();
      setDraft(null);
      setError(null);
    } else if (e.key === "Enter" && (!multiline || e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      refocus.current = true;
      e.currentTarget.blur();
    }
  };

  const id = `field-${label.toLowerCase().replace(/\W+/g, "-")}${bare ? "-bare" : ""}`;
  const editing = draft !== null;
  const shown = value && format ? format(value) : value;
  const clamped = clamp && !expanded && !editing;

  useEffect(() => {
    if (editing || !refocus.current) return;
    refocus.current = false;
    button.current?.focus();
  }, [editing]);

  useLayoutEffect(() => {
    const el = button.current;
    if (clamp && el && !expanded) setOverflows(el.scrollHeight > el.clientHeight + 1);
  }, [clamp, expanded, shown, editing]);

  return (
    <div className={cx("group", bare ? "-mx-1.5" : "grid grid-cols-[6rem_minmax(0,1fr)] items-start gap-x-2 py-0.5", className)}>
      <label htmlFor={id} className={bare ? "sr-only" : "flex h-7 items-center gap-1 text-xs text-muted"}>
        {label}
        {pinned && (
          <span title="Edited by a person. Sign-ups and enrichment won't overwrite it." className="text-faint">
            <Icon name="pin" className="size-3" />
            <span className="sr-only">(pinned)</span>
          </span>
        )}
      </label>
      <div className="min-w-0">
        {lead}
        <div className="flex items-start gap-0.5">
          {editing ? (
            multiline ? (
              <textarea
                id={id}
                autoFocus
                rows={3}
                value={draft}
                disabled={busy}
                placeholder={editPlaceholder}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={onKeyDown}
                onBlur={commit}
                className={cx(box, "field-sizing-content min-h-7 resize-none border-fg bg-surface placeholder:text-faint focus:outline-none")}
              />
            ) : (
              <input
                id={id}
                autoFocus
                value={draft}
                disabled={busy}
                placeholder={editPlaceholder}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={onKeyDown}
                onBlur={commit}
                className={cx(box, "h-7 border-fg bg-surface placeholder:text-faint focus:outline-none")}
              />
            )
          ) : (
            <button
              ref={button}
              id={id}
              type="button"
              title={multiline ? undefined : shown}
              onClick={() => setDraft(value)}
              className={cx(
                box,
                "min-h-7 border-transparent transition-colors hover:bg-subtle",
                multiline ? "whitespace-pre-wrap [overflow-wrap:anywhere]" : "truncate",
                clamped && "line-clamp-4",
                !value ? "text-faint" : lead ? "text-muted" : "text-fg",
              )}
            >
              {shown || placeholder}
            </button>
          )}
          {href && value && (
            <a
              href={value}
              target="_blank"
              rel="noreferrer noopener"
              className={cx("grid size-7 shrink-0 place-items-center rounded-control text-faint hover:bg-subtle hover:text-fg", editing && "invisible")}
              aria-label={`Open ${label}`}
            >
              <Icon name="external" className="size-3.5" />
            </a>
          )}
          {pinned && (
            <Button
              size="sm"
              variant="ghost"
              disabled={busy}
              onClick={() => void run(onReset)}
              title="Reset: let sign-ups and enrichment fill this again"
              aria-label={`Reset ${label} to automatic`}
              className={cx("w-7 px-0 group-focus-within:opacity-100 pointer-fine:opacity-0 pointer-fine:group-hover:opacity-100", editing && "invisible")}
            >
              <Icon name="reset" className="size-3.5" />
            </Button>
          )}
        </div>
        {clamp && overflows && !editing && (
          <button type="button" onClick={() => setExpanded(!expanded)} aria-expanded={expanded} aria-controls={id} className="px-1.5 text-xs text-muted transition-colors hover:text-fg">
            {expanded ? "Less" : "More"}
          </button>
        )}
        {error && (
          <p role="alert" className="mt-1 px-1.5 text-xs text-danger">
            {error}
          </p>
        )}
      </div>
    </div>
  );
}
