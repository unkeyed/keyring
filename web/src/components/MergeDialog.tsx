import { useState, type KeyboardEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router";
import { api, type Contact } from "../api";
import { companyLabel, contactLabel } from "../format";
import { keys } from "../queries";
import { useToast } from "./Toaster";
import { Avatar, Button, cx, Dialog, DialogBody, DialogFooter, ErrorNote, Icon, inputClass, Spinner } from "../ui";

export function MergeDialog({ open, onClose, contact }: { open: boolean; onClose: () => void; contact: Contact }) {
  const [other, setOther] = useState<Contact | null>(null);
  const close = () => {
    setOther(null);
    onClose();
  };
  return (
    <Dialog open={open} onClose={close} title={other ? "Confirm merge" : `Merge ${contactLabel(contact)} with…`} wide>
      {other ? <Confirm current={contact} other={other} onBack={() => setOther(null)} onDone={close} /> : <Pick exclude={contact} onPick={setOther} />}
    </Dialog>
  );
}

/** Opens on the newest contacts, since a duplicate usually signed up recently; typing searches everyone. */
function Pick({ exclude, onPick }: { exclude: Contact; onPick: (c: Contact) => void }) {
  const [q, setQ] = useState("");
  const query = q.trim();
  const results = useQuery({
    queryKey: [...keys.contacts, "merge-search", query],
    queryFn: () => api.contacts.search({ q: query || undefined, limit: 9 }),
    placeholderData: (prev) => prev,
  });
  const items = (results.data?.items ?? []).filter((c) => c.id !== exclude.id).slice(0, 8);
  const [active, setActive] = useState(0);
  const at = Math.min(active, items.length - 1);
  const optionId = (c: Contact) => `merge-option-${c.id}`;

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setActive(Math.max(0, Math.min(items.length - 1, at + (e.key === "ArrowDown" ? 1 : -1))));
    } else if (e.key === "Enter" && items[at]) {
      e.preventDefault();
      onPick(items[at]);
    }
  };

  return (
    <DialogBody>
      <div className="relative">
        <Icon name="search" className="pointer-events-none absolute top-2 left-2.5 size-4 text-faint" />
        <input
          autoFocus
          data-autofocus
          type="search"
          role="combobox"
          aria-expanded={items.length > 0}
          aria-controls="merge-options"
          aria-activedescendant={items[at] ? optionId(items[at]) : undefined}
          aria-label="Find the other contact"
          placeholder="Search by name or email"
          value={q}
          onChange={(e) => {
            setQ(e.target.value);
            setActive(0);
          }}
          onKeyDown={onKeyDown}
          className={cx(inputClass, "pl-8")}
        />
      </div>
      <h3 className="mt-4 mb-1 px-2 text-xs font-medium text-muted">{query ? "Matches" : "Recent contacts"}</h3>
      <ul id="merge-options" role="listbox" className="relative max-h-72 space-y-0.5 overflow-auto" aria-label={query ? "Matching contacts" : "Recent contacts"}>
        {items.map((c, i) => (
          <li key={c.id} id={optionId(c)} role="option" aria-selected={i === at}>
            <button
              type="button"
              tabIndex={-1}
              onClick={() => onPick(c)}
              onMouseMove={() => setActive(i)}
              className={cx("flex w-full items-center gap-2.5 rounded-surface px-2 py-1.5 text-left transition-colors", i === at && "bg-subtle")}
            >
              <Avatar src={c.avatar_url} label={contactLabel(c)} size={24} />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium">{contactLabel(c)}</span>
                <span className="block truncate text-xs text-muted">{c.email}</span>
              </span>
              {c.company && <span className="max-w-[35%] truncate text-xs text-faint">{companyLabel(c.company)}</span>}
            </button>
          </li>
        ))}
      </ul>
      {results.isPending && (
        <div className="grid h-16 place-items-center">
          <Spinner />
        </div>
      )}
      {results.isSuccess && !results.isFetching && items.length === 0 && (
        <p className="py-4 text-center text-sm text-faint">{query ? `No other contacts match “${query}”.` : "There are no other contacts yet."}</p>
      )}
      <ErrorNote error={results.error} />
    </DialogBody>
  );
}

function Confirm({ current, other, onBack, onDone }: { current: Contact; other: Contact; onBack: () => void; onDone: () => void }) {
  const [keepCurrent, setKeepCurrent] = useState(true);
  const keep = keepCurrent ? current : other;
  const drop = keepCurrent ? other : current;
  const qc = useQueryClient();
  const navigate = useNavigate();
  const toast = useToast();
  const merge = useMutation({
    mutationFn: () => api.contacts.merge(keep.id, drop.id),
    onSuccess: (kept) => {
      qc.removeQueries({ queryKey: keys.contact(drop.id) });
      void qc.invalidateQueries({ queryKey: keys.contact(kept.id) });
      void qc.invalidateQueries({ queryKey: keys.contacts });
      onDone();
      toast(`Merged ${drop.email} into ${contactLabel(kept)}`);
      if (kept.id !== current.id) void navigate(`/contacts/${kept.id}`, { replace: true });
    },
  });

  return (
    <>
      <DialogBody>
        <div className="grid grid-cols-1 items-center gap-2 sm:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)]">
          <Side c={drop} role="Merged in, then deleted" tone="danger" />
          <Button variant="ghost" size="sm" onClick={() => setKeepCurrent(!keepCurrent)} aria-label="Swap which contact is kept" title="Swap" className="mx-auto w-7 px-0">
            <Icon name="right" className="size-4 rotate-90 sm:rotate-0" />
          </Button>
          <Side c={keep} role="Kept" tone="success" />
        </div>
        <p className="mt-4 text-sm text-muted [overflow-wrap:anywhere]">
          <b className="font-medium text-fg">{drop.email}</b>&apos;s sources, notes, tags and activity move to <b className="font-medium text-fg">{keep.email}</b>. Empty fields
          on the kept contact are filled from the other one. This can&apos;t be undone.
        </p>
        <div className="mt-3">
          <ErrorNote error={merge.error} />
        </div>
      </DialogBody>
      <DialogFooter>
        <Button variant="ghost" onClick={onBack} disabled={merge.isPending}>
          Back
        </Button>
        <Button variant="primary" autoFocus onClick={() => merge.mutate()} disabled={merge.isPending} className="max-w-full">
          <span className="truncate">{merge.isPending ? "Merging…" : `Merge into ${contactLabel(keep)}`}</span>
        </Button>
      </DialogFooter>
    </>
  );
}

function Side({ c, role, tone }: { c: Contact; role: string; tone: "success" | "danger" }) {
  return (
    <div className="min-w-0 rounded-surface border border-line p-3">
      <div className={cx("mb-2 text-xs font-medium", tone === "success" ? "text-success" : "text-danger")}>{role}</div>
      <div className="flex items-center gap-2">
        <Avatar src={c.avatar_url} label={contactLabel(c)} size={28} />
        <div className="min-w-0">
          <div className="truncate text-sm font-medium">{contactLabel(c)}</div>
          <div className="truncate text-xs text-muted">{c.email}</div>
        </div>
      </div>
    </div>
  );
}
