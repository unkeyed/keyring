import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router";
import { api, ApiError, parseID, type Company, type CompanyDetail, type CompanyField, type CompanyID } from "../api";
import { companyLabel, contactLabel, countryName, plural } from "../format";
import { keys, useStatus } from "../queries";
import { EditableField } from "../components/EditableField";
import { RecordLayout, SectionTitle } from "../components/RecordLayout";
import { NotFound } from "../Layout";
import { Avatar, Badge, Button, Crumbs, cx, EnrichmentBadge, ErrorNote, Icon, PageHeader, PageSpinner, RelativeTime } from "../ui";

const missing = { title: "Company not found", body: "The link may be wrong, or the company was removed.", parent: { label: "Companies", to: "/companies" } };

export function CompanyPage() {
  const id = parseID<CompanyID>(useParams().id);
  return id ? <CompanyView key={id} id={id} /> : <NotFound {...missing} />;
}

const fields: { field: CompanyField; label: string; multiline?: boolean; href?: boolean; format?: (v: string) => string; editPlaceholder?: string }[] = [
  { field: "name", label: "Name" },
  { field: "description", label: "Description", multiline: true },
  { field: "industry", label: "Industry" },
  { field: "employee_range", label: "Employees", editPlaceholder: "Like 51-200" },
  { field: "country", label: "Country", format: countryName, editPlaceholder: "Two-letter code, like PT" },
  { field: "linkedin_url", label: "LinkedIn", href: true },
];

function CompanyView({ id }: { id: CompanyID }) {
  const qc = useQueryClient();
  const company = useQuery({ queryKey: keys.company(id), queryFn: () => api.companies.get(id) });
  const status = useStatus();

  const settle = (c: Company) => {
    qc.setQueryData<CompanyDetail>(keys.company(id), (d) => d && { ...d, ...c });
    void qc.invalidateQueries({ queryKey: keys.company(id) });
    void qc.invalidateQueries({ queryKey: keys.companies });
  };
  const enrich = useMutation({
    mutationFn: () => api.companies.enrich(id),
    onSettled: () => qc.invalidateQueries({ queryKey: keys.company(id) }),
  });

  if (company.isPending) return <PageSpinner />;
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
  const label = companyLabel(c);
  const pinned = new Set(c.manual_fields ?? []);
  const contacts = c.contacts ?? [];
  const facts = [c.industry, c.employee_range && `${c.employee_range} employees`, c.country && countryName(c.country)].filter(Boolean);
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
      </PageHeader>

      <RecordLayout
        head={
          <>
            <div className="flex items-start gap-4">
              <Avatar src={c.logo_url} label={label} size={52} square />
              <div className="min-w-0 flex-1">
                <h2 className="truncate text-xl font-semibold" title={label}>
                  {label}
                </h2>
                <a href={`https://${c.domain}`} target="_blank" rel="noreferrer noopener" className="inline-flex max-w-full items-center gap-1 text-sm text-muted hover:text-fg">
                  <span className="truncate">{c.domain}</span>
                  <Icon name="external" className="size-3" />
                </a>
                {facts.length > 0 && <p className="mt-1 text-sm text-muted">{facts.join(" · ")}</p>}
              </div>
            </div>
            <div className="mt-3 flex flex-wrap items-center gap-2 empty:hidden">
              <EnrichmentBadge state={c.enrichment} status={status.data} />
            </div>
            <div className="mt-3 empty:hidden">
              <ErrorNote error={enrich.error} />
            </div>
          </>
        }
        details={
          <>
            <SectionTitle>Details</SectionTitle>
            {fields.map((f) => (
              <EditableField
                key={f.field}
                label={f.label}
                value={c[f.field]}
                pinned={pinned.has(f.field)}
                multiline={f.multiline}
                href={f.href}
                format={f.format}
                editPlaceholder={f.editPlaceholder}
                onSave={async (v) => settle(await api.companies.update(id, { [f.field]: v }))}
                onReset={async () => settle(await api.companies.resetField(id, f.field))}
              />
            ))}
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
          {contacts.length === 0 ? (
            <p className="text-sm text-faint">No contacts at this company.</p>
          ) : (
            <ul className="divide-y divide-line overflow-hidden rounded-surface border border-line bg-surface">
              {contacts.map((p) => (
                <li key={p.id}>
                  <Link to={`/contacts/${p.id}`} className="flex items-center gap-3 px-3 py-2 transition-colors hover:bg-subtle">
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
      </RecordLayout>
    </>
  );
}
