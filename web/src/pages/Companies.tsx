import { useEffect, useRef, useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearchParams } from "react-router";
import { api, type Company, type Status } from "../api";
import { companyLabel, countryName } from "../format";
import { keys, useStatus } from "../queries";
import { Avatar, Button, cx, EmptyState, EnrichmentBadge, ErrorNote, Icon, inputClass, PageHeader, PageSpinner, useWide } from "../ui";

export function Companies() {
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const [draft, setDraft] = useState(q);
  const searchRef = useRef<HTMLInputElement>(null);
  const navigate = useNavigate();
  const status = useStatus();
  const wide = useWide();

  useEffect(() => {
    if (draft === q) return;
    const t = window.setTimeout(() => setParams(draft ? { q: draft } : {}, { replace: true }), 200);
    return () => window.clearTimeout(t);
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (e.key === "/" && !["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName)) {
        e.preventDefault();
        searchRef.current?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const list = useInfiniteQuery({
    queryKey: [...keys.companies, q],
    queryFn: ({ pageParam }) => api.companies.search({ q: q.trim() || undefined, cursor: pageParam, limit: 50 }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor || undefined,
    placeholderData: (prev) => prev,
  });
  const rows = list.data?.pages.flatMap((p) => p.items ?? []) ?? [];
  const total = status.data?.companies;

  return (
    <>
      <PageHeader
        title={
          <>
            Companies
            {total != null && total > 0 && <span className="ml-2 font-normal text-faint">{total.toLocaleString()}</span>}
          </>
        }
      />
      <div className="flex shrink-0 items-center gap-2 border-b border-line px-3 py-2.5 md:px-5">
        <div className="relative w-full md:w-64">
          <Icon name="search" className="pointer-events-none absolute top-2 left-2.5 size-4 text-faint" />
          <input
            ref={searchRef}
            type="search"
            aria-label="Search companies"
            placeholder="Search name or domain"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            className={cx(inputClass, "pl-8")}
          />
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        {list.isPending ? (
          <PageSpinner />
        ) : list.isError && rows.length === 0 ? (
          <div className="p-5">
            <ErrorNote error={list.error} />
          </div>
        ) : rows.length === 0 ? (
          <div className="mx-auto max-w-md px-4 py-16">
            <EmptyState icon={q ? "search" : "building"} title={q ? "No companies match" : "No companies yet"}>
              {q ? "Try a domain like acme.com." : "Companies appear when someone signs up with a work email. Gmail and other free-mail addresses don't create one."}
            </EmptyState>
          </div>
        ) : wide ? (
          <table className="w-full table-fixed border-collapse text-sm" aria-label="Companies">
            <thead className="sticky top-0 z-10 bg-header">
              <tr className="border-b border-line text-left text-xs whitespace-nowrap text-muted">
                <th scope="col" className="py-2 pr-3 pl-5 font-medium">Company</th>
                <th scope="col" className="w-[22%] px-3 py-2 font-medium">Industry</th>
                <th scope="col" className="hidden w-28 px-3 py-2 font-medium lg:table-cell">Employees</th>
                <th scope="col" className="w-36 px-3 py-2 font-medium">Country</th>
                <th scope="col" className="w-24 py-2 pr-5 pl-3 text-right font-medium">Contacts</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((c) => (
                <CompanyRow key={c.id} c={c} status={status.data} onOpen={() => void navigate(`/companies/${c.id}`)} />
              ))}
            </tbody>
          </table>
        ) : (
          <ul aria-label="Companies" className="divide-y divide-line">
            {rows.map((c) => (
              <CompanyItem key={c.id} c={c} />
            ))}
          </ul>
        )}
        {list.hasNextPage && (
          <div className="flex justify-center py-4">
            <Button size="sm" onClick={() => void list.fetchNextPage()} disabled={list.isFetchingNextPage}>
              {list.isFetchingNextPage ? "Loading…" : "Load more"}
            </Button>
          </div>
        )}
      </div>
    </>
  );
}

const dash = <span className="text-faint">—</span>;

function CompanyRow({ c, status, onOpen }: { c: Company; status: Status | undefined; onOpen: () => void }) {
  const label = companyLabel(c);
  return (
    <tr onClick={onOpen} className="h-12 border-b border-line transition-colors hover:bg-subtle">
      <td className="py-1.5 pr-3 pl-5">
        <div className="flex min-w-0 items-center gap-2.5">
          <Avatar src={c.logo_url} label={label} size={28} square />
          <div className="min-w-0 flex-1">
            <Link to={`/companies/${c.id}`} onClick={(e) => e.stopPropagation()} className="block truncate font-medium hover:underline" title={label}>
              {label}
            </Link>
            {c.name && <div className="truncate text-xs text-muted">{c.domain}</div>}
          </div>
          <EnrichmentBadge state={c.enrichment} status={status} />
        </div>
      </td>
      <td className="truncate px-3 text-muted">{c.industry || dash}</td>
      <td className="hidden truncate px-3 text-muted lg:table-cell">{c.employee_range || dash}</td>
      <td className="truncate px-3 text-muted">{c.country ? countryName(c.country) : dash}</td>
      <td className="py-1.5 pr-5 pl-3 text-right tabular-nums">{c.contact_count.toLocaleString()}</td>
    </tr>
  );
}

function CompanyItem({ c }: { c: Company }) {
  const label = companyLabel(c);
  const facts = [c.industry, c.country && countryName(c.country)].filter(Boolean).join(" · ");
  return (
    <li>
      <Link to={`/companies/${c.id}`} className="flex items-center gap-3 px-4 py-2.5 transition-colors active:bg-subtle">
        <Avatar src={c.logo_url} label={label} size={32} square />
        <div className="min-w-0 flex-1">
          <div className="truncate font-medium">{label}</div>
          {c.name && <div className="truncate text-xs text-muted">{c.domain}</div>}
          {facts && <div className="truncate text-xs text-faint">{facts}</div>}
        </div>
        <span className="flex shrink-0 items-center gap-1 text-xs text-muted tabular-nums" title={`${c.contact_count} contacts`}>
          <Icon name="users" className="size-3.5 text-faint" />
          {c.contact_count.toLocaleString()}
        </span>
      </Link>
    </li>
  );
}
