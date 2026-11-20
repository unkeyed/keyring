import { useEffect, useRef, useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearchParams } from "react-router";
import { api, type Company, type Status } from "../api";
import { companyLabel, countryName } from "../format";
import { keys, usePrefetch, useStatus } from "../queries";
import { NewCompanyDialog } from "../components/CreateDialogs";
import { useRowNav } from "../components/RowNav";
import { Button, CompanyLogo, cx, EmptyState, EnrichmentBadge, ErrorNote, Icon, inputClass, Kbd, ListSkeleton, listShortcuts, PageHeader, ProgressBar, SearchKbd, ShortcutsHelp, useWide } from "../ui";

export function Companies() {
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const [draft, setDraft] = useState(q);
  const [creating, setCreating] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const navigate = useNavigate();
  const prefetch = usePrefetch();
  const status = useStatus();
  const wide = useWide();

  useEffect(() => {
    if (draft === q) return;
    const t = window.setTimeout(() => setParams(draft ? { q: draft } : {}, { replace: true }), 200);
    return () => window.clearTimeout(t);
  });

  const list = useInfiniteQuery({
    queryKey: [...keys.companies, q],
    queryFn: ({ pageParam }) => api.companies.search({ q: q.trim() || undefined, cursor: pageParam, limit: 50 }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor || undefined,
    placeholderData: (prev) => prev,
  });
  const rows = list.data?.pages.flatMap((p) => p.items ?? []) ?? [];
  const total = status.data?.companies;
  const stale = list.isPlaceholderData;
  const open = (c: Company) => void navigate(`/companies/${c.id}`, { viewTransition: true });

  const nav = useRowNav({
    count: rows.length,
    listRef,
    searchRef,
    onOpen: (i) => rows[i] && open(rows[i]),
    onNew: () => setCreating(true),
    onNearEnd: () => list.hasNextPage && !list.isFetchingNextPage && void list.fetchNextPage(),
  });
  const { select } = nav;
  useEffect(() => select(0), [q, select]);

  return (
    <>
      <PageHeader
        title={
          <>
            Companies
            {total != null && total > 0 && <span className="ml-2 font-normal text-faint">{total.toLocaleString()}</span>}
          </>
        }
      >
        {rows.length > 0 && <ShortcutsHelp shortcuts={listShortcuts("company")} />}
        <Button variant="primary" size="sm" onClick={() => setCreating(true)} aria-label="New company" title="New company (n)">
          <Icon name="plus" className="size-3.5" />
          <span className="hidden sm:inline">New company</span>
          <span className="ml-0.5 hidden md:contents">
            <Kbd inverse>N</Kbd>
          </span>
        </Button>
      </PageHeader>
      <NewCompanyDialog open={creating} onClose={() => setCreating(false)} />
      <div className="relative flex shrink-0 items-center gap-2 border-b border-line px-3 py-2.5 md:px-5">
        <ProgressBar active={stale} />
        <div className="relative w-full md:w-64">
          <Icon name="search" className="pointer-events-none absolute top-2 left-2.5 size-4 text-faint" />
          <input
            ref={searchRef}
            type="search"
            aria-label="Search companies"
            placeholder="Search name or domain"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            className={cx(inputClass, "peer pl-8 lg:pr-8")}
          />
          <SearchKbd />
        </div>
      </div>
      <div ref={listRef} aria-busy={stale || list.isPending} className={cx("relative min-h-0 flex-1 overflow-auto transition-opacity", stale && "opacity-60")}>
        {list.isPending ? (
          <ListSkeleton square />
        ) : list.isError && rows.length === 0 ? (
          <div className="p-5">
            <ErrorNote error={list.error} />
          </div>
        ) : rows.length === 0 ? (
          <div className="mx-auto max-w-md px-4 py-16">
            <EmptyState icon={q ? "search" : "building"} title={q ? "No companies match" : "No companies yet"}>
              {q ? "Try a domain like acme.com." : "Companies appear when someone signs up with a work email, or when you add one with New company. Gmail and other free-mail addresses don't create one."}
            </EmptyState>
          </div>
        ) : wide ? (
          <table className="w-full table-fixed border-separate border-spacing-0 text-sm" aria-label="Companies">
            <thead className="sticky top-0 z-10 bg-header">
              <tr className="text-left text-xs whitespace-nowrap text-muted [&>th]:border-b [&>th]:border-line">
                <th scope="col" className="py-2 pr-3 pl-5 font-medium">Company</th>
                <th scope="col" className="w-[22%] px-3 py-2 font-medium">Industry</th>
                <th scope="col" className="hidden w-28 px-3 py-2 font-medium lg:table-cell">Employees</th>
                <th scope="col" className="w-36 px-3 py-2 font-medium">Country</th>
                <th scope="col" className="w-24 py-2 pr-5 pl-3 text-right font-medium">Contacts</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((c, i) => (
                <CompanyRow key={c.id} c={c} status={status.data} row={nav.rowProps(i)} onOpen={() => open(c)} onPrefetch={() => prefetch.company(c.id)} />
              ))}
            </tbody>
          </table>
        ) : (
          <ul aria-label="Companies" className="divide-y divide-line">
            {rows.map((c, i) => (
              <CompanyItem key={c.id} c={c} row={nav.rowProps(i)} onPrefetch={() => prefetch.company(c.id)} />
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

type RowProps = ReturnType<ReturnType<typeof useRowNav>["rowProps"]>;

function CompanyRow({ c, status, row, onOpen, onPrefetch }: { c: Company; status: Status | undefined; row: RowProps; onOpen: () => void; onPrefetch: () => void }) {
  const label = companyLabel(c);
  return (
    <tr
      {...row}
      onFocus={() => (row.onFocus(), onPrefetch())}
      onMouseEnter={onPrefetch}
      onClick={onOpen}
      className="group/row h-12 scroll-mt-9 transition-colors outline-none hover:bg-subtle focus-visible:bg-subtle [&>td]:border-b [&>td]:border-line"
    >
      <td className="py-1.5 pr-3 pl-5 group-focus-visible/row:shadow-[inset_2px_0_0_var(--fg)]">
        <div className="flex min-w-0 items-center gap-2.5">
          <CompanyLogo company={c} size={28} />
          <div className="min-w-0 flex-1">
            <Link to={`/companies/${c.id}`} viewTransition onClick={(e) => e.stopPropagation()} className="block truncate font-medium hover:underline" tabIndex={-1} title={label}>
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

function CompanyItem({ c, row, onPrefetch }: { c: Company; row: RowProps; onPrefetch: () => void }) {
  const label = companyLabel(c);
  const facts = [c.industry, c.country && countryName(c.country)].filter(Boolean).join(" · ");
  return (
    <li>
      <Link
        to={`/companies/${c.id}`}
        viewTransition
        {...row}
        onFocus={() => (row.onFocus(), onPrefetch())}
        onPointerDown={onPrefetch}
        className="flex items-center gap-3 px-4 py-2.5 transition-colors outline-none focus-visible:bg-subtle active:bg-subtle"
      >
        <CompanyLogo company={c} size={32} />
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
