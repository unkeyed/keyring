import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router";
import { api, ApiError, parseID, type CompanyRef, type Contact, type ContactDetail, type ContactField, type ContactID, type ContactPatch, type Note } from "../api";
import { companyLabel, contactLabel } from "../format";
import { keys, useCompany, useContact, usePrefetch, useStages, useStatus } from "../queries";
import { DeleteDialog } from "../components/DeleteDialog";
import { EditableField } from "../components/EditableField";
import { LinkList, shortURL } from "../components/Links";
import { MergeDialog } from "../components/MergeDialog";
import { RecordLayout, RecordSkeleton, SectionTitle } from "../components/RecordLayout";
import { TagEditor } from "../components/Tags";
import { Timeline } from "../components/Timeline";
import { useToast } from "../components/Toaster";
import { NotFound } from "../Layout";
import {
  Avatar,
  Badge,
  Button,
  CompanyLogo,
  ConfirmButton,
  CopyButton,
  Crumbs,
  cx,
  EnrichmentBadge,
  errorMessage,
  ErrorNote,
  Icon,
  PageHeader,
  RelativeTime,
  selectClass,
  Skeleton,
  useEscapeTo,
} from "../ui";

const missing = { title: "Contact not found", body: "It may have been merged into another contact, or the link is wrong.", parent: { label: "Contacts", to: "/contacts" } };

export function ContactPage() {
  const id = parseID<ContactID>(useParams().id);
  return id ? <ContactView key={id} id={id} /> : <NotFound {...missing} />;
}

const fields: { field: ContactField; label: string; multiline?: boolean; href?: boolean; format?: (v: string) => string }[] = [
  { field: "name", label: "Name" },
  { field: "title", label: "Title" },
  { field: "company", label: "Company" },
  { field: "location", label: "Location" },
  { field: "linkedin_url", label: "LinkedIn", href: true, format: shortURL },
  { field: "bio", label: "Bio", multiline: true },
];

const fieldValue = (c: Contact, f: ContactField) => (f === "company" ? (c.company?.domain ?? "") : c[f]);
const fieldPatch = (f: ContactField, v: string): ContactPatch => (f === "company" ? { company_domain: v } : { [f]: v });

/** What a stage or tag change will look like once saved, shown before the server answers. */
const applyPatch = (c: ContactDetail, p: ContactPatch): ContactDetail => ({
  ...c,
  stage: p.stage ?? c.stage,
  tags: [...new Set([...(c.tags ?? []), ...(p.add_tags ?? [])])].filter((t) => !p.remove_tags?.includes(t)).sort(),
});

function ContactView({ id }: { id: ContactID }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const toast = useToast();
  const contact = useContact(id);
  const stages = useStages();
  const status = useStatus();
  const [merging, setMerging] = useState(false);
  const [deleting, setDeleting] = useState(false);
  useEscapeTo("/contacts");

  /** Every write answers with the contact; show it now, then refetch the timeline it added to. */
  const settle = (c: Contact) => {
    qc.setQueryData<ContactDetail>(keys.contact(id), (d) => d && { ...d, ...c });
    void qc.invalidateQueries({ queryKey: keys.contact(id) });
    void qc.invalidateQueries({ queryKey: keys.contacts });
  };
  const update = async (patch: ContactPatch) => settle(await api.contacts.update(id, patch));

  // Stage and tag changes show at once and roll back if the save fails. Only
  // the last of several quick changes refetches, so earlier answers can't
  // overwrite later optimistic ones.
  const patchKey = ["patch-contact", id];
  const patch = useMutation({
    mutationKey: patchKey,
    mutationFn: (p: ContactPatch) => api.contacts.update(id, p),
    onMutate: async (p) => {
      await qc.cancelQueries({ queryKey: keys.contact(id) });
      const prev = qc.getQueryData<ContactDetail>(keys.contact(id));
      if (prev) qc.setQueryData(keys.contact(id), applyPatch(prev, p));
      return prev;
    },
    onError: (err, _p, prev) => {
      if (prev) qc.setQueryData(keys.contact(id), prev);
      toast(`Couldn't save: ${errorMessage(err)}`, "error");
    },
    onSettled: () => {
      if (qc.isMutating({ mutationKey: patchKey }) > 1) return;
      void qc.invalidateQueries({ queryKey: keys.contact(id) });
      void qc.invalidateQueries({ queryKey: keys.contacts });
      void qc.invalidateQueries({ queryKey: keys.tags });
    },
  });
  const enrich = useMutation({
    mutationFn: () => api.contacts.enrich(id),
    onSuccess: (r) => toast(r.state === "skipped" ? "Free-mail addresses aren't enriched" : "Re-enrichment queued"),
    onSettled: () => qc.invalidateQueries({ queryKey: keys.contact(id) }),
  });
  const remove = useMutation({
    mutationFn: () => api.contacts.delete(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.contacts });
      void qc.invalidateQueries({ queryKey: ["company"] });
      void qc.invalidateQueries({ queryKey: keys.status });
      void qc.invalidateQueries({ queryKey: keys.tags });
      toast(`Deleted ${contact.data ? contactLabel(contact.data) : "the contact"}`);
      void navigate("/contacts", { replace: true, viewTransition: true });
    },
  });

  if (contact.isPending) {
    return (
      <>
        <PageHeader title={<Crumbs parent="Contacts" to="/contacts" current="" />} />
        <RecordSkeleton />
      </>
    );
  }
  if (contact.isError) {
    return contact.error instanceof ApiError && contact.error.status === 404 ? (
      <NotFound {...missing} />
    ) : (
      <div className="p-5">
        <ErrorNote error={contact.error} />
      </div>
    );
  }

  const c = contact.data;
  const loading = contact.isPlaceholderData;
  const label = contactLabel(c);
  const pinned = new Set(c.manual_fields ?? []);
  const canEnrich = status.data?.enrichment_enabled && !status.data.enrichment_halt;

  return (
    <>
      <PageHeader title={<Crumbs parent="Contacts" to="/contacts" current={label} />}>
        {canEnrich && (
          <Button size="sm" onClick={() => enrich.mutate()} disabled={enrich.isPending || c.enrichment === "running"} title="Look this person up on context.dev again" aria-label="Re-enrich">
            <Icon name="refresh" className={cx("size-3.5", enrich.isPending && "animate-spin")} />
            <span className="hidden sm:inline">Re-enrich</span>
          </Button>
        )}
        <Button size="sm" onClick={() => setMerging(true)} aria-label="Merge with another contact" title="Merge with another contact">
          <Icon name="merge" className="size-3.5" />
          <span className="hidden sm:inline">Merge…</span>
        </Button>
        <Button size="sm" variant="ghost" onClick={() => setDeleting(true)} aria-label="Delete contact" title="Delete contact" className="w-7 px-0 hover:text-danger">
          <Icon name="trash" className="size-3.5" />
        </Button>
      </PageHeader>

      <RecordLayout
        head={
          <>
            <div className="flex items-start gap-4">
              <Avatar src={c.avatar_url} label={label} size={52} />
              <div className="min-w-0 flex-1">
                <h2 className="truncate text-xl font-semibold" title={label}>
                  {label}
                </h2>
                {(c.title || c.company) && (
                  <p className="truncate text-sm text-muted">
                    {c.title}
                    {c.title && c.company && " at "}
                    {c.company && (
                      <Link to={`/companies/${c.company.id}`} className="text-fg hover:underline">
                        {companyLabel(c.company)}
                      </Link>
                    )}
                  </p>
                )}
                <div className="mt-0.5 flex min-w-0 items-center gap-1 text-sm">
                  <a href={`mailto:${c.email}`} className="min-w-0 truncate text-muted hover:text-fg" title={c.email}>
                    {c.email}
                  </a>
                  <CopyButton text={c.email} label="Copy" className="-my-1" />
                </div>
              </div>
            </div>

            <div className="mt-5 flex flex-wrap items-center gap-x-3 gap-y-2">
              <label className="flex items-center gap-2 text-xs text-muted">
                Stage
                <select className={selectClass} value={c.stage} onChange={(e) => patch.mutate({ stage: e.target.value })}>
                  {(stages.data ?? [c.stage]).map((s) => (
                    <option key={s} value={s}>
                      {s}
                    </option>
                  ))}
                </select>
              </label>
              <EnrichmentBadge state={c.enrichment} status={status.data} />
              <span className="text-xs text-faint">
                First seen <RelativeTime iso={c.first_seen_at} />, last seen <RelativeTime iso={c.last_seen_at} />
              </span>
            </div>
            <div className="mt-3 empty:hidden">
              <ErrorNote error={enrich.error} />
            </div>
            {c.company && <CompanyCard company={c.company} />}
          </>
        }
        details={
          <>
            <SectionTitle>Details</SectionTitle>
            {fields.map((f) => (
              <EditableField
                key={f.field}
                label={f.label}
                value={fieldValue(c, f.field)}
                pinned={pinned.has(f.field)}
                multiline={f.multiline}
                href={f.href}
                format={f.format}
                placeholder={f.field === "company" ? "No company" : "Empty"}
                editPlaceholder={f.field === "company" ? "Company domain, like acme.com" : undefined}
                lead={
                  f.field === "company" &&
                  c.company && (
                    <Link to={`/companies/${c.company.id}`} className="flex h-7 items-center gap-1.5 rounded-control px-[7px] text-sm font-medium hover:bg-subtle">
                      <span className="truncate">{companyLabel(c.company)}</span>
                      <Icon name="right" className="size-3.5 text-faint" />
                    </Link>
                  )
                }
                onSave={(v) => update(fieldPatch(f.field, v))}
                onReset={async () => settle(await api.contacts.resetField(id, f.field))}
              />
            ))}

            <Profiles links={c.socials} />

            <TagEditor tags={c.tags ?? []} onChange={patch.mutate} lead={c.company && <CompanyTags company={c.company} tags={c.company_tags ?? []} />} />

            <SectionTitle className="mt-6">Sources</SectionTitle>
            {loading ? (
              <Skeleton className="h-[4.75rem]" />
            ) : (
            <ul className="space-y-2">
              {(c.identities ?? []).map((i) => (
                <li key={`${i.provider}/${i.external_id}`} className="rounded-surface border border-line bg-surface p-2.5">
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-sm font-medium">{i.provider}</span>
                    {i.deleted_at ? <Badge tone="danger">Deleted</Badge> : i.email_verified ? <Badge tone="success">Verified</Badge> : <Badge>Unverified</Badge>}
                  </div>
                  <div className="mt-1 truncate font-mono text-2xs text-faint" title={i.external_id}>
                    {i.external_id}
                  </div>
                  <div className="mt-1 flex justify-between gap-2 text-xs text-muted">
                    <span className="truncate" title={i.email}>
                      {i.email}
                    </span>
                    <RelativeTime iso={i.occurred_at} className="shrink-0" />
                  </div>
                </li>
              ))}
              {(c.identities ?? []).length === 0 && <li className="text-xs text-faint">Added by hand, not through a sign-up.</li>}
            </ul>
            )}
          </>
        }
      >
        <Notes contactId={id} notes={c.notes ?? []} loading={loading} />
        <section className="mt-9">
          <SectionTitle>Activity</SectionTitle>
          {loading ? <TimelineSkeleton /> : <Timeline items={c.activity ?? []} />}
        </section>
      </RecordLayout>

      <MergeDialog open={merging} onClose={() => setMerging(false)} contact={c} />
      <DeleteDialog
        open={deleting}
        onClose={() => setDeleting(false)}
        title={`Delete ${label}?`}
        action="Delete contact"
        onConfirm={() => remove.mutate()}
        pending={remove.isPending}
        error={remove.error}
      >
        Their sources, notes, tags and activity are deleted with them.
      </DeleteDialog>
    </>
  );
}

function Profiles({ links }: { links: Record<string, string> }) {
  if (!Object.values(links).some(Boolean)) return null;
  return (
    <>
      <SectionTitle className="mt-6">Profiles</SectionTitle>
      <LinkList links={links} />
    </>
  );
}

function TimelineSkeleton() {
  return (
    <div className="space-y-4" role="status" aria-label="Loading activity">
      {[0, 1, 2].map((i) => (
        <div key={i} className="flex items-center gap-3">
          <Skeleton className="size-6 shrink-0 rounded-full" />
          <Skeleton className="h-3 w-64 max-w-[70%]" />
        </div>
      ))}
    </div>
  );
}

/**
 * What enrichment knows about the contact's company, which is often more than
 * it knows about the person. It draws at once from the contact's company and
 * keeps one height, so the logo and tagline arriving never move the page.
 */
function CompanyCard({ company: ref }: { company: CompanyRef }) {
  const company = useCompany(ref.id);
  const prefetch = usePrefetch();
  const co = company.data;
  const label = companyLabel(ref);
  const links = { linkedin: co?.linkedin_url, x: co?.socials?.x };
  return (
    <section className="mt-5 flex h-[3.875rem] items-center gap-3 rounded-surface border border-line bg-surface px-3">
      <CompanyLogo company={co ?? { ...ref, logos: [], logo_url: "" }} size={32} />
      <div className="min-w-0 flex-1">
        <Link to={`/companies/${ref.id}`} onMouseEnter={() => prefetch.company(ref.id)} onFocus={() => prefetch.company(ref.id)} className="block truncate text-sm font-medium hover:underline">
          {label}
        </Link>
        {co ? <p className="truncate text-xs text-muted">{co.tagline || [co.industry, co.domain].filter(Boolean).join(" · ")}</p> : <Skeleton className="my-0.5 h-3 w-48 max-w-full" />}
      </div>
      <div className="flex shrink-0 items-center">
        {Object.entries(links).map(
          ([kind, url]) =>
            url && (
              <a
                key={kind}
                href={url}
                target="_blank"
                rel="noopener noreferrer"
                aria-label={`${label} on ${kind === "x" ? "X" : "LinkedIn"}`}
                className="grid size-7 place-items-center rounded-control text-muted transition-colors hover:bg-subtle hover:text-fg"
              >
                <Icon name={kind === "x" ? "xLogo" : "linkedin"} className="size-3.5" />
              </a>
            ),
        )}
      </div>
    </section>
  );
}

/** Tags the contact carries through its company. They are changed on the company, so each links there. */
function CompanyTags({ company, tags }: { company: CompanyRef; tags: string[] }) {
  const from = `From ${companyLabel(company)}`;
  return tags.map((t) => (
    <Link key={t} to={`/companies/${company.id}`} title={`${t}: ${from}; change it on the company`} className="max-w-full rounded-control">
      <Badge className="hover:bg-subtle-strong">
        <Icon name="building" className="size-3 shrink-0 text-faint" />
        <span className="truncate">{t}</span>
        <span className="sr-only">({from})</span>
      </Badge>
    </Link>
  ));
}

function Notes({ contactId, notes, loading }: { contactId: ContactID; notes: Note[]; loading: boolean }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [body, setBody] = useState("");
  const key = keys.contact(contactId);
  const add = useMutation({
    mutationFn: (text: string) => api.contacts.addNote(contactId, text),
    onSuccess: (note) => {
      setBody("");
      qc.setQueryData<ContactDetail>(key, (d) => d && { ...d, notes: [note, ...(d.notes ?? [])] });
      return qc.invalidateQueries({ queryKey: key });
    },
  });
  const remove = useMutation({
    mutationFn: (n: Note) => api.notes.delete(n.id),
    onMutate: async (n) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<ContactDetail>(key);
      if (prev) qc.setQueryData(key, { ...prev, notes: (prev.notes ?? []).filter((x) => x.id !== n.id) });
      return prev;
    },
    onError: (err, _n, prev) => {
      if (prev) qc.setQueryData(key, prev);
      toast(`Couldn't delete the note: ${errorMessage(err)}`, "error");
    },
    onSuccess: () => toast("Note deleted"),
    onSettled: () => qc.invalidateQueries({ queryKey: key }),
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (body.trim() && !add.isPending) add.mutate(body.trim());
  };

  return (
    <section>
      <SectionTitle>
        Notes {notes.length > 0 && <span className="text-faint">{notes.length}</span>}
      </SectionTitle>
      <form onSubmit={submit} className="rounded-surface border border-line-strong bg-surface transition-colors focus-within:border-fg focus-within:ring-3 focus-within:ring-halo">
        <textarea
          aria-label="New note"
          placeholder="Write a note…"
          rows={2}
          value={body}
          onChange={(e) => setBody(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) submit(e);
          }}
          className="block field-sizing-content max-h-64 min-h-14 w-full resize-none rounded-t-surface bg-transparent px-3 py-2 text-sm placeholder:text-faint focus:outline-none"
        />
        <div className="flex items-center justify-end gap-3 px-2 pb-2">
          <span className="hidden text-xs text-faint sm:inline">⌘↵ to save</span>
          <Button type="submit" size="sm" variant="primary" disabled={!body.trim() || add.isPending}>
            {add.isPending ? "Adding…" : "Add note"}
          </Button>
        </div>
      </form>
      <div className="mt-2 empty:hidden">
        <ErrorNote error={add.error} />
      </div>
      {loading ? (
        <Skeleton className="mt-3 h-16" />
      ) : (
        <ul className="mt-3 space-y-2">
          {notes.map((n) => (
            <li key={n.id} className="group rounded-surface border border-line bg-surface px-3 py-2.5 transition-[opacity,translate] duration-200 ease-out starting:-translate-y-1 starting:opacity-0">
              <p className="text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">{n.body}</p>
              <div className="mt-1.5 flex items-center gap-2 text-xs text-faint">
                <span className="min-w-0 truncate">{n.author.label}</span>
                <RelativeTime iso={n.created_at} className="shrink-0" />
                <ConfirmButton
                  label="Delete note"
                  confirmLabel="Delete?"
                  className="-my-1 ml-auto group-focus-within:opacity-100 pointer-fine:opacity-0 pointer-fine:group-hover:opacity-100"
                  onConfirm={() => remove.mutate(n)}
                >
                  <Icon name="trash" className="size-3.5" />
                </ConfirmButton>
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
