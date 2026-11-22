import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router";
import { api, ApiError, parseID, type Company, type CompanyDetail, type CompanyField, type CompanyID } from "../api";
import { companyLabel, contactLabel, countryName, plural } from "../format";
import { keys, useCompany, usePrefetch, useStatus } from "../queries";
import { DeleteDialog } from "../components/DeleteDialog";
import { EditableField } from "../components/EditableField";
import { LinkList, shortURL } from "../components/Links";
import { RecordLayout, RecordSkeleton, SectionTitle } from "../components/RecordLayout";
import { Timeline } from "../components/Timeline";
import { useToast } from "../components/Toaster";
import { NotFound } from "../Layout";
import { Avatar, Badge, Button, CompanyLogo, Crumbs, cx, EnrichmentBadge, ErrorNote, Icon, PageHeader, RelativeTime, Skeleton, useEscapeTo } from "../ui";

const missing = { title: "Company not found", body: "The link may be wrong, or the company was removed.", parent: { label: "Companies", to: "/companies" } };

export function CompanyPage() {
  const id = parseID<CompanyID>(useParams().id);
  return id ? <CompanyView key={id} id={id} /> : <NotFound {...missing} />;
}

const fields: { field: CompanyField; label: string; multiline?: boolean; href?: boolean; format?: (v: string) => string; editPlaceholder?: string; clamp?: boolean }[] = [
  { field: "name", label: "Name" },
  { field: "tagline", label: "Tagline" },
  { field: "description", label: "Description", multiline: true, clamp: true },
  { field: "industry", label: "Industry" },
  { field: "employee_range", label: "Employees", editPlaceholder: "Like 51-200" },
  { field: "headquarters", label: "HQ", editPlaceholder: "City, region" },
  { field: "country", label: "Country", format: countryName, editPlaceholder: "Two-letter code, like PT" },
  { field: "linkedin_url", label: "LinkedIn", href: true, format: shortURL },
];

function CompanyView({ id }: { id: CompanyID }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const toast = useToast();
  const prefetch = usePrefetch();
  const company = useCompany(id);
  const status = useStatus();
  const [deleting, setDeleting] = useState(false);
  useEscapeTo("/companies");

  const settle = (c: Company) => {
    qc.setQueryData<CompanyDetail>(keys.company(id), (d) => d && { ...d, ...c });
    void qc.invalidateQueries({ queryKey: keys.company(id) });
    void qc.invalidateQueries({ queryKey: keys.companies });
  };
  const enrich = useMutation({
    mutationFn: () => api.companies.enrich(id),
    onSuccess: () => toast("Re-enrichment queued"),
    onSettled: () => qc.invalidateQueries({ queryKey: keys.company(id) }),
  });
  const remove = useMutation({
    mutationFn: () => api.companies.delete(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.companies });
      void qc.invalidateQueries({ queryKey: keys.contacts });
      void qc.invalidateQueries({ queryKey: ["contact"] });
      void qc.invalidateQueries({ queryKey: keys.status });
      toast(`Deleted ${company.data ? companyLabel(company.data) : "the company"}`);
      void navigate("/companies", { replace: true, viewTransition: true });
    },
  });

  if (company.isPending) {
    return (
      <>
        <PageHeader title={<Crumbs parent="Companies" to="/companies" current="" />} />
        <RecordSkeleton square />
      </>
    );
  }
  if (company.isError) {
    return company.error instanceof ApiError && company.error.status === 404 ? (
      <NotFound {...missing} />
    ) : (
      <div className="p-5">
        <ErrorNote error={company.error} />
      </div>
    );
  }

  const c = company.data;
  const loading = company.isPlaceholderData;
  const label = companyLabel(c);
  const pinned = new Set(c.manual_fields ?? []);
  const contacts = c.contacts ?? [];
  const place = [c.headquarters, c.country && countryName(c.country)].filter(Boolean).join(", ");
  const facts = [c.industry, c.employee_range && `${c.employee_range} employees`, place, c.stock].filter(Boolean);
  const segments = c.segments ?? [];
  const canEnrich = status.data?.enrichment_enabled && !status.data.enrichment_halt;

  return (
    <>
      <PageHeader title={<Crumbs parent="Companies" to="/companies" current={label} />}>
        {canEnrich && (
          <Button size="sm" onClick={() => enrich.mutate()} disabled={enrich.isPending || c.enrichment === "running"} title="Look this company up on context.dev again" aria-label="Re-enrich">
            <Icon name="refresh" className={cx("size-3.5", enrich.isPending && "animate-spin")} />
            <span className="hidden sm:inline">Re-enrich</span>
          </Button>
        )}
        <Button size="sm" variant="ghost" onClick={() => setDeleting(true)} aria-label="Delete company" title="Delete company" className="w-7 px-0 hover:text-danger">
          <Icon name="trash" className="size-3.5" />
        </Button>
      </PageHeader>

      <RecordLayout
        head={
          <>
            <div className="flex items-start gap-4">
              <CompanyLogo company={c} size={52} />
              <div className="min-w-0 flex-1">
                <h2 className="truncate text-xl font-semibold" title={label}>
                  {label}
                </h2>
                {c.tagline && (
                  <EditableField
                    bare
                    label="Tagline"
                    value={c.tagline}
                    pinned={pinned.has("tagline")}
                    onSave={async (v) => settle(await api.companies.update(id, { tagline: v }))}
                    onReset={async () => settle(await api.companies.resetField(id, "tagline"))}
                  />
                )}
                <a href={`https://${c.domain}`} target="_blank" rel="noreferrer noopener" className="inline-flex max-w-full items-center gap-1 text-sm text-muted hover:text-fg">
                  <span className="truncate">{c.domain}</span>
                  <Icon name="external" className="size-3" />
                </a>
                {facts.length > 0 && <p className="mt-1 text-sm text-muted">{facts.join(" · ")}</p>}
              </div>
            </div>
            <div className="mt-3 flex flex-wrap items-center gap-1.5 empty:hidden">
              <EnrichmentBadge state={c.enrichment} status={status.data} />
              {segments.map((s) => (
                <Badge key={s} title={s}>
                  <span className="truncate">{s}</span>
                </Badge>
              ))}
            </div>
            <div className="mt-3 empty:hidden">
              <ErrorNote error={enrich.error} />
            </div>
          </>
        }
        details={
          <>
            <SectionTitle>Details</SectionTitle>
            {fields
              .filter((f) => !(f.field === "tagline" && c.tagline))
              .map((f) => (
              <EditableField
                key={f.field}
                label={f.label}
                value={c[f.field]}
                pinned={pinned.has(f.field)}
                multiline={f.multiline}
                href={f.href}
                format={f.format}
                clamp={f.clamp}
                editPlaceholder={f.editPlaceholder}
                onSave={async (v) => settle(await api.companies.update(id, { [f.field]: v }))}
                onReset={async () => settle(await api.companies.resetField(id, f.field))}
              />
            ))}
            <Links company={c} />
          </>
        }
      >
        <section>
          <div className="mb-2 flex items-center justify-between gap-3">
            <SectionTitle className="mb-0">
              People {contacts.length > 0 && <span className="text-faint">{plural(c.contact_count, "contact")}</span>}
            </SectionTitle>
            {c.contact_count > contacts.length && (
              <Link to={`/contacts?company=${c.id}`} className="text-xs text-muted underline-offset-2 hover:text-fg hover:underline">
                See all in Contacts
              </Link>
            )}
          </div>
          {loading ? (
            <Skeleton className="h-24" />
          ) : contacts.length === 0 ? (
            <p className="text-sm text-faint">No contacts at this company.</p>
          ) : (
            <ul className="divide-y divide-line overflow-hidden rounded-surface border border-line bg-surface">
              {contacts.map((p) => (
                <li key={p.id}>
                  <Link
                    to={`/contacts/${p.id}`}
                    onMouseEnter={() => prefetch.contact(p.id)}
                    onFocus={() => prefetch.contact(p.id)}
                    className="flex items-center gap-3 px-3 py-2 transition-colors hover:bg-subtle"
                  >
                    <Avatar src={p.avatar_url} label={contactLabel(p)} size={28} />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm font-medium">{contactLabel(p)}</span>
                      <span className="block truncate text-xs text-muted">{p.title || p.email}</span>
                    </span>
                    <Badge>{p.stage}</Badge>
                    <RelativeTime iso={p.first_seen_at} className="hidden w-24 shrink-0 text-right text-xs whitespace-nowrap text-faint sm:block" />
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </section>
        <section className="mt-9">
          <SectionTitle>Activity</SectionTitle>
          {loading ? <Skeleton className="h-24" /> : <Timeline items={c.activity ?? []} showContact />}
        </section>
      </RecordLayout>
      <DeleteDialog
        open={deleting}
        onClose={() => setDeleting(false)}
        title={`Delete ${label}?`}
        action="Delete company"
        onConfirm={() => remove.mutate()}
        pending={remove.isPending}
        error={remove.error}
      >
        {c.contact_count > 0 ? `Its ${plural(c.contact_count, "contact")} stay, without a company.` : "No contacts are linked to it."}
      </DeleteDialog>
    </>
  );
}

function Links({ company: c }: { company: Company }) {
  const links = { ...c.socials, ...c.links };
  if (!Object.values(links).some(Boolean)) return null;
  return (
    <>
      <SectionTitle className="mt-6">Links</SectionTitle>
      <LinkList links={links} />
    </>
  );
}
