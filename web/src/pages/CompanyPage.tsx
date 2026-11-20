import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router";
import { api, ApiError, parseID, type Company, type CompanyDetail, type CompanyField, type CompanyID } from "../api";
import { companyLabel, contactLabel, plural } from "../format";
import { keys } from "../queries";
import { EditableField } from "../components/EditableField";
import { EnrichmentNote } from "../components/EnrichmentNote";
import { NotFound } from "../Layout";
import { Avatar, Badge, Button, cx, EnrichmentBadge, ErrorNote, Icon, PageHeader, RelativeTime, Spinner } from "../ui";

export function CompanyPage() {
  const id = parseID<CompanyID>(useParams().id);
  return id ? <CompanyView key={id} id={id} /> : <NotFound />;
}

const fields: { field: CompanyField; label: string; multiline?: boolean; href?: boolean }[] = [
  { field: "name", label: "Name" },
  { field: "description", label: "Description", multiline: true },
  { field: "industry", label: "Industry" },
  { field: "employee_range", label: "Employees" },
  { field: "country", label: "Country" },
  { field: "linkedin_url", label: "LinkedIn", href: true },
];

function CompanyView({ id }: { id: CompanyID }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const company = useQuery({ queryKey: keys.company(id), queryFn: () => api.companies.get(id) });

  const settle = (c: Company) => {
    qc.setQueryData<CompanyDetail>(keys.company(id), (d) => d && { ...d, ...c });
    void qc.invalidateQueries({ queryKey: keys.company(id) });
    void qc.invalidateQueries({ queryKey: keys.companies });
  };
  const enrich = useMutation({
    mutationFn: () => api.companies.enrich(id),
    onSettled: () => qc.invalidateQueries({ queryKey: keys.company(id) }),
  });

  if (company.isPending) {
    return (
      <div className="grid h-40 place-items-center">
        <Spinner />
      </div>
    );
  }
  if (company.isError) {
    return company.error instanceof ApiError && company.error.status === 404 ? (
      <NotFound />
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

  return (
    <>
      <PageHeader
        title={
          <span className="flex items-center gap-1.5">
            <Link to="/companies" className="font-normal text-muted hover:text-fg">
              Companies
            </Link>
            <span className="text-faint">/</span>
            {label}
          </span>
        }
      >
        <Button size="sm" onClick={() => enrich.mutate()} disabled={enrich.isPending || c.enrichment === "running"} title="Look this company up on context.dev again">
          <Icon name="refresh" className={cx("size-3.5", enrich.isPending && "animate-spin")} />
          Re-enrich
        </Button>
      </PageHeader>

      <div className="flex min-h-0 flex-1">
        <div className="min-w-0 flex-1 overflow-auto">
          <div className="mx-auto max-w-3xl px-8 py-7">
            <div className="flex items-start gap-4">
              <Avatar src={c.logo_url} label={label} size={56} square />
              <div className="min-w-0 flex-1">
                <h2 className="truncate text-xl font-semibold tracking-tight">{label}</h2>
                <a href={`https://${c.domain}`} target="_blank" rel="noreferrer noopener" className="mt-0.5 inline-flex items-center gap-1 text-sm text-muted hover:text-fg">
                  {c.domain}
                  <Icon name="external" className="size-3" />
                </a>
                <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted">
                  <EnrichmentBadge state={c.enrichment} />
                  {c.industry && <Badge>{c.industry}</Badge>}
                  {c.employee_range && <Badge>{c.employee_range} employees</Badge>}
                  {c.country && <Badge>{c.country}</Badge>}
                </div>
              </div>
            </div>
            {c.description && <p className="mt-5 text-sm leading-relaxed whitespace-pre-wrap text-muted">{c.description}</p>}
            <div className="mt-3 space-y-2">
              <EnrichmentNote state={c.enrichment} />
              <ErrorNote error={enrich.error} />
            </div>

            <section className="mt-8">
              <div className="mb-3 flex items-center justify-between">
                <h3 className="text-xs font-semibold tracking-wide text-muted uppercase">People {contacts.length > 0 && <span className="font-normal text-faint">{plural(c.contact_count, "contact")}</span>}</h3>
                {c.contact_count > contacts.length && (
                  <Link to={`/contacts?company=${c.id}`} className="text-xs text-fg underline underline-offset-2 hover:opacity-70">
                    See all in Contacts
                  </Link>
                )}
              </div>
              {contacts.length === 0 ? (
                <p className="text-sm text-faint">No contacts at this company.</p>
              ) : (
                <ul className="divide-y divide-line rounded-surface border border-line">
                  {contacts.map((p) => (
                    <li key={p.id}>
                      <button type="button" onClick={() => void navigate(`/contacts/${p.id}`)} className="flex w-full items-center gap-3 px-3 py-2 text-left hover:bg-subtle">
                        <Avatar src={p.avatar_url} label={contactLabel(p)} size={26} />
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-sm font-medium">{contactLabel(p)}</span>
                          <span className="block truncate text-xs text-muted">{p.title || p.email}</span>
                        </span>
                        <Badge>{p.stage}</Badge>
                        <RelativeTime iso={p.first_seen_at} className="w-24 text-right text-xs whitespace-nowrap text-faint" />
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          </div>
        </div>

        <aside className="w-80 shrink-0 overflow-auto border-l border-line bg-bg/40 px-4 py-5">
          <h3 className="mb-1 px-1 text-xs font-semibold tracking-wide text-muted uppercase">Details</h3>
          {fields.map((f) => (
            <EditableField
              key={f.field}
              label={f.label}
              value={c[f.field]}
              pinned={pinned.has(f.field)}
              multiline={f.multiline}
              href={f.href}
              onSave={async (v) => settle(await api.companies.update(id, { [f.field]: v }))}
              onReset={async () => settle(await api.companies.resetField(id, f.field))}
            />
          ))}
        </aside>
      </div>
    </>
  );
}
