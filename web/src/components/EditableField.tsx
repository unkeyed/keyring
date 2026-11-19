import { useRef, useState, type KeyboardEvent } from "react";
import { Button, cx, errorMessage, Icon, inputClass } from "../ui";

interface Props {
  label: string;
  value: string;
  /** In manual_fields: a person set it, so sign-ups and enrichment leave it alone. */
  pinned: boolean;
  onSave: (value: string) => Promise<unknown>;
  onReset: () => Promise<unknown>;
  multiline?: boolean;
  placeholder?: string;
  href?: boolean;
}

/** A property row that edits in place: click, type, Enter saves, Escape cancels. */
export function EditableField({ label, value, pinned, onSave, onReset, multiline, placeholder = "Empty", href }: Props) {
  const [draft, setDraft] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // Blur is the one place an edit commits; Escape sets this so its blur discards instead.
  const cancelled = useRef(false);

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
      e.currentTarget.blur();
      setDraft(null);
      setError(null);
    } else if (e.key === "Enter" && (!multiline || e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      e.currentTarget.blur();
    }
  };

  const id = `field-${label.toLowerCase().replace(/\W+/g, "-")}`;

  return (
    <div className="group grid grid-cols-[6.5rem_1fr] items-start gap-2 py-1">
      <label htmlFor={id} className="flex h-7 items-center gap-1 text-xs text-muted">
        {label}
        {pinned && (
          <span title="Edited by a person. Sign-ups and enrichment won't overwrite it." className="text-accent">
            <Icon name="pin" className="size-3" />
            <span className="sr-only">(pinned)</span>
          </span>
        )}
      </label>
      <div className="min-w-0">
        {draft !== null ? (
          multiline ? (
            <textarea
              id={id}
              autoFocus
              rows={4}
              value={draft}
              disabled={busy}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={onKeyDown}
              onBlur={commit}
              className={cx(inputClass, "h-auto py-1.5")}
            />
          ) : (
            <input id={id} autoFocus value={draft} disabled={busy} onChange={(e) => setDraft(e.target.value)} onKeyDown={onKeyDown} onBlur={commit} className={inputClass} />
          )
        ) : (
          <div className="flex min-h-7 items-start gap-1">
            <button
              id={id}
              type="button"
              onClick={() => setDraft(value)}
              className={cx(
                "min-h-7 min-w-0 flex-1 rounded-md px-1.5 py-1 text-left text-[13px] hover:bg-subtle",
                multiline ? "whitespace-pre-wrap" : "truncate",
                !value && "text-faint",
              )}
            >
              {value || placeholder}
            </button>
            {href && value && (
              <a href={value} target="_blank" rel="noreferrer noopener" className="grid size-7 place-items-center rounded-md text-faint hover:bg-subtle hover:text-fg" aria-label={`Open ${label}`}>
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
                className="opacity-0 group-focus-within:opacity-100 group-hover:opacity-100"
              >
                <Icon name="reset" className="size-3.5" />
              </Button>
            )}
          </div>
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
