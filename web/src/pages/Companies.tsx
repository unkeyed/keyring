import { useEffect, useRef, useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearchParams } from "react-router";
import { api, type Company, type Status } from "../api";
import { companyLabel } from "../format";
import { keys, useStatus } from "../queries";
import { Avatar, Button, cx, EnrichmentBadge, ErrorNote, Icon, inputClass, PageHeader, Spinner } from "../ui";

export function Companies() {
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const [draft, setDraft] = useState(q);
  const searchRef = useRef<HTMLInputElement>(null);
  const navigate = useNavigate();
  const status = useStatus();

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
      <div className="flex items-center gap-2 border-b border-line px-5 py-2.5">
        <div className="relative w-64">
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
          <div className="grid h-40 place-items-center">
            <Spinner />
          </div>
        ) : list.isError && rows.length === 0 ? (
          <div className="p-5">
            <ErrorNote error={list.error} />
          </div>
        ) : rows.length === 0 ? (
          <div className="mx-auto mt-20 max-w-sm text-center">
            <p className="font-medium">{q ? "No companies match" : "No companies yet"}</p>
            <p className="mt-1 text-sm text-muted">
              {q ? "Try a domain like acme.com." : "Companies appear when someone signs up with a work email. Gmail and other free-mail addresses don't create one."}
            </p>
          </div>
        ) : (
          <table className="w-full table-fixed border-collapse text-sm" aria-label="Companies">
            <thead className="sticky top-0 z-10 bg-surface">
              <tr className="border-b border-line text-left text-xs text-muted">
                <th scope="col" className="w-[34%] py-2 pr-3 pl-5 font-medium">Company</th>
                <th scope="col" className="w-[20%] px-3 py-2 font-medium">Industry</th>
                <th scope="col" className="w-[14%] px-3 py-2 font-medium">Employees</th>
                <th scope="col" className="w-[12%] px-3 py-2 font-medium">Country</th>
                <th scope="col" className="w-[10%] px-3 py-2 text-right font-medium">Contacts</th>
                <th scope="col" className="w-[10%] py-2 pr-5 pl-3 text-right font-medium">
                  <span className="sr-only">Enrichment</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((c) => (
                <CompanyRow key={c.id} c={c} status={status.data} onOpen={() => void navigate(`/companies/${c.id}`)} />
              ))}
            </tbody>
          </table>
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

function CompanyRow({ c, status, onOpen }: { c: Company; status: Status | undefined; onOpen: () => void }) {
  const label = companyLabel(c);
  return (
    <tr onClick={onOpen} className="h-11 cursor-default border-b border-line hover:bg-subtle">
      <td className="py-1.5 pr-3 pl-5">
        <div className="flex min-w-0 items-center gap-2.5">
          <Avatar src={c.logo_url} label={label} size={24} square />
          <div className="min-w-0">
            <Link to={`/companies/${c.id}`} onClick={(e) => e.stopPropagation()} className="block truncate font-medium hover:underline">
              {label}
            </Link>
            {c.name && <div className="truncate text-xs text-muted">{c.domain}</div>}
          </div>
        </div>
      </td>
      <td className="truncate px-3 text-muted">{c.industry || <span className="text-faint">—</span>}</td>
      <td className="truncate px-3 text-muted">{c.employee_range || <span className="text-faint">—</span>}</td>
      <td className="truncate px-3 text-muted">{c.country || <span className="text-faint">—</span>}</td>
      <td className="px-3 text-right tabular-nums">{c.contact_count.toLocaleString()}</td>
      <td className="py-1.5 pr-5 pl-3 text-right"><EnrichmentBadge state={c.enrichment} status={status} /></td>
    </tr>
  );
}
