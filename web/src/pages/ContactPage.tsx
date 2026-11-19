import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router";
import { api, ApiError, parseID, type Contact, type ContactDetail, type ContactField, type ContactID, type ContactPatch, type Note } from "../api";
import { companyLabel, contactLabel } from "../format";
import { keys, useStages, useTags } from "../queries";
import { EditableField } from "../components/EditableField";
import { EnrichmentNote } from "../components/EnrichmentNote";
import { MergeDialog } from "../components/MergeDialog";
import { Timeline } from "../components/Timeline";
import { NotFound } from "../Layout";
import { Avatar, Badge, Button, ConfirmButton, CopyButton, cx, EnrichmentBadge, ErrorNote, Icon, inputClass, PageHeader, RelativeTime, selectClass, Spinner } from "../ui";

export function ContactPage() {
  const id = parseID<ContactID>(useParams().id);
  return id ? <ContactView key={id} id={id} /> : <NotFound />;
}

const fields: { field: ContactField; label: string; multiline?: boolean; href?: boolean }[] = [
  { field: "name", label: "Name" },
  { field: "title", label: "Title" },
  { field: "company", label: "Company domain" },
  { field: "location", label: "Location" },
  { field: "linkedin_url", label: "LinkedIn", href: true },
  { field: "bio", label: "Bio", multiline: true },
];

const fieldValue = (c: Contact, f: ContactField) => (f === "company" ? (c.company?.domain ?? "") : c[f]);
const fieldPatch = (f: ContactField, v: string): ContactPatch => (f === "company" ? { company_domain: v } : { [f]: v });

function ContactView({ id }: { id: ContactID }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const contact = useQuery({ queryKey: keys.contact(id), queryFn: () => api.contacts.get(id) });
  const stages = useStages();
  const [merging, setMerging] = useState(false);

  /** Every write answers with the contact; show it now, then refetch the timeline it added to. */
  const settle = (c: Contact) => {
    qc.setQueryData<ContactDetail>(keys.contact(id), (d) => d && { ...d, ...c });
    void qc.invalidateQueries({ queryKey: keys.contact(id) });
    void qc.invalidateQueries({ queryKey: keys.contacts });
  };
  const update = async (patch: ContactPatch) => settle(await api.contacts.update(id, patch));
  const stage = useMutation({ mutationFn: (s: string) => update({ stage: s }) });
  const enrich = useMutation({
    mutationFn: () => api.contacts.enrich(id),
    onSettled: () => qc.invalidateQueries({ queryKey: keys.contact(id) }),
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (e.key !== "Escape" || document.querySelector("dialog[open]") || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName)) return;
      void navigate("/contacts");
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [navigate]);

  if (contact.isPending) {
    return (
      <div className="grid h-40 place-items-center">
        <Spinner />
      </div>
    );
  }
  if (contact.isError) {
    return contact.error instanceof ApiError && contact.error.status === 404 ? (
      <NotFound />
    ) : (
      <div className="p-5">
        <ErrorNote error={contact.error} />
      </div>
    );
  }

  const c = contact.data;
  const label = contactLabel(c);
  const pinned = new Set(c.manual_fields ?? []);

  return (
    <>
      <PageHeader
        title={
          <span className="flex items-center gap-1.5">
            <Link to="/contacts" className="font-normal text-muted hover:text-fg">
              Contacts
            </Link>
            <span className="text-faint">/</span>
            {label}
          </span>
        }
      >
        <Button size="sm" onClick={() => enrich.mutate()} disabled={enrich.isPending || c.enrichment === "running"} title="Look this person up on context.dev again">
          <Icon name="refresh" className={cx("size-3.5", enrich.isPending && "animate-spin")} />
          Re-enrich
        </Button>
        <Button size="sm" onClick={() => setMerging(true)}>
          <Icon name="merge" className="size-3.5" />
          Merge…
        </Button>
      </PageHeader>

      <div className="flex min-h-0 flex-1">
        <div className="min-w-0 flex-1 overflow-auto">
          <div className="mx-auto max-w-3xl px-8 py-7">
            <div className="flex items-start gap-4">
              <Avatar src={c.avatar_url} label={label} size={56} />
              <div className="min-w-0 flex-1">
                <h2 className="truncate text-xl font-semibold tracking-tight">{label}</h2>
                <p className="mt-0.5 truncate text-[13px] text-muted">
                  {c.title}
                  {c.title && c.company && " at "}
                  {c.company && (
                    <Link to={`/companies/${c.company.id}`} className="text-fg hover:underline">
                      {companyLabel(c.company)}
                    </Link>
                  )}
                </p>
                <div className="mt-1 flex items-center gap-1 text-[13px]">
                  <a href={`mailto:${c.email}`} className="text-muted hover:text-fg">
                    {c.email}
                  </a>
                  <CopyButton text={c.email} label="Copy" />
                </div>
              </div>
            </div>

            <div className="mt-5 flex flex-wrap items-center gap-3">
              <label className="flex items-center gap-2 text-xs text-muted">
                Stage
                <select className={selectClass} value={c.stage} disabled={stage.isPending} onChange={(e) => stage.mutate(e.target.value)}>
                  {(stages.data ?? [c.stage]).map((s) => (
                    <option key={s} value={s}>
                      {s}
                    </option>
                  ))}
                </select>
              </label>
              <EnrichmentBadge state={c.enrichment} />
              <span className="text-xs text-faint">
                First seen <RelativeTime iso={c.first_seen_at} />, last seen <RelativeTime iso={c.last_seen_at} />
              </span>
            </div>
            <div className="mt-3 space-y-2">
              <EnrichmentNote state={c.enrichment} />
              <ErrorNote error={stage.error ?? enrich.error} />
            </div>

            <Notes contactId={id} notes={c.notes ?? []} />

            <section className="mt-9">
              <h3 className="mb-3 text-xs font-semibold tracking-wide text-muted uppercase">Activity</h3>
              <Timeline items={c.activity ?? []} />
            </section>
          </div>
        </div>

        <aside className="w-80 shrink-0 overflow-auto border-l border-line bg-bg/40 px-4 py-5">
          <h3 className="mb-1 px-1 text-xs font-semibold tracking-wide text-muted uppercase">Details</h3>
          {fields.map((f) => (
            <EditableField
              key={f.field}
              label={f.label}
              value={fieldValue(c, f.field)}
              pinned={pinned.has(f.field)}
              multiline={f.multiline}
              href={f.href}
              placeholder={f.field === "company" ? "No company" : "Empty"}
              onSave={(v) => update(fieldPatch(f.field, v))}
              onReset={async () => settle(await api.contacts.resetField(id, f.field))}
            />
          ))}

          <Tags tags={c.tags ?? []} onChange={update} />

          <h3 className="mt-6 mb-2 px-1 text-xs font-semibold tracking-wide text-muted uppercase">Sources</h3>
          <ul className="space-y-2 px-1">
            {(c.identities ?? []).map((i) => (
              <li key={`${i.provider}/${i.external_id}`} className="rounded-md border border-line bg-surface p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-[13px] font-medium">{i.provider}</span>
                  {i.deleted_at ? <Badge tone="danger">Deleted</Badge> : i.email_verified ? <Badge tone="success">Verified</Badge> : <Badge>Unverified</Badge>}
                </div>
                <div className="mt-1 truncate font-mono text-[11px] text-faint" title={i.external_id}>
                  {i.external_id}
                </div>
                <div className="mt-1 flex justify-between gap-2 text-xs text-muted">
                  <span className="truncate">{i.email}</span>
                  <RelativeTime iso={i.occurred_at} className="shrink-0" />
                </div>
              </li>
            ))}
            {(c.identities ?? []).length === 0 && <li className="text-xs text-faint">Added through the API.</li>}
          </ul>
        </aside>
      </div>

      <MergeDialog open={merging} onClose={() => setMerging(false)} contact={c} />
    </>
  );
}

function Tags({ tags, onChange }: { tags: string[]; onChange: (p: ContactPatch) => Promise<void> }) {
  const all = useTags();
  const [draft, setDraft] = useState("");
  const qc = useQueryClient();
  const change = useMutation({ mutationFn: onChange, onSuccess: () => qc.invalidateQueries({ queryKey: keys.tags }) });

  const add = (e: FormEvent) => {
    e.preventDefault();
    const t = draft.trim().toLowerCase();
    if (!t || tags.includes(t)) return setDraft("");
    change.mutate({ add_tags: [t] });
    setDraft("");
  };

  return (
    <>
      <h3 className="mt-6 mb-2 px-1 text-xs font-semibold tracking-wide text-muted uppercase">Tags</h3>
      <div className="flex flex-wrap items-center gap-1.5 px-1">
        {tags.map((t) => (
          <span key={t} className="inline-flex h-6 items-center gap-0.5 rounded bg-accent-soft pr-0.5 pl-2 text-xs font-medium text-accent">
            {t}
            <button
              type="button"
              aria-label={`Remove tag ${t}`}
              onClick={() => change.mutate({ remove_tags: [t] })}
              className="grid size-5 place-items-center rounded hover:bg-accent/15"
            >
              <Icon name="x" className="size-3" />
            </button>
          </span>
        ))}
        <form onSubmit={add} className="min-w-24 flex-1">
          <input
            aria-label="Add tag"
            placeholder="Add tag…"
            list="tag-options"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            className={cx(inputClass, "h-6 border-transparent bg-transparent px-1.5 text-xs hover:border-line")}
          />
          <datalist id="tag-options">
            {all.data?.filter((t) => !tags.includes(t.tag)).map((t) => <option key={t.tag} value={t.tag} />)}
          </datalist>
        </form>
      </div>
      <div className="mt-1 px-1">
        <ErrorNote error={change.error} />
      </div>
    </>
  );
}

function Notes({ contactId, notes }: { contactId: ContactID; notes: Note[] }) {
  const qc = useQueryClient();
  const [body, setBody] = useState("");
  const refresh = () => qc.invalidateQueries({ queryKey: keys.contact(contactId) });
  const add = useMutation({
    mutationFn: () => api.contacts.addNote(contactId, body.trim()),
    onSuccess: () => {
      setBody("");
      return refresh();
    },
  });
  const remove = useMutation({ mutationFn: (n: Note) => api.notes.delete(n.id), onSuccess: refresh });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (body.trim()) add.mutate();
  };

  return (
    <section className="mt-9">
      <h3 className="mb-3 text-xs font-semibold tracking-wide text-muted uppercase">Notes {notes.length > 0 && <span className="font-normal text-faint">{notes.length}</span>}</h3>
      <form onSubmit={submit} className="rounded-lg border border-line-strong bg-surface focus-within:border-accent focus-within:ring-2 focus-within:ring-accent/25">
        <textarea
          aria-label="New note"
          placeholder="Write a note…"
          rows={2}
          value={body}
          onChange={(e) => setBody(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) submit(e);
          }}
          className="block w-full resize-y rounded-t-lg bg-transparent px-3 py-2 text-[13px] placeholder:text-faint focus:outline-none"
        />
        <div className="flex items-center justify-end gap-2 px-2 pb-2">
          <span className="text-xs text-faint">⌘↵ to save</span>
          <Button type="submit" size="sm" variant="primary" disabled={!body.trim() || add.isPending}>
            Add note
          </Button>
        </div>
      </form>
      <div className="mt-2">
        <ErrorNote error={add.error ?? remove.error} />
      </div>
      <ul className="mt-3 space-y-2">
        {notes.map((n) => (
          <li key={n.id} className="group rounded-lg border border-line px-3 py-2.5">
            <p className="text-[13px] whitespace-pre-wrap">{n.body}</p>
            <div className="mt-1.5 flex items-center gap-2 text-xs text-faint">
              <span>{n.author.label}</span>
              <RelativeTime iso={n.created_at} />
              <ConfirmButton
                label="Delete note"
                confirmLabel="Delete note?"
                className="ml-auto opacity-0 group-focus-within:opacity-100 group-hover:opacity-100"
                onConfirm={() => remove.mutate(n)}
              >
                <Icon name="trash" className="size-3.5" />
              </ConfirmButton>
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}
