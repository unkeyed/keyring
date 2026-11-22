import { useEffect, useRef, useState, type RefObject } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearchParams } from "react-router";
import { api, parseID, type CompanyID, type Contact, type ContactQuery } from "../api";
import { companyLabel, contactLabel } from "../format";
import { keys, usePrefetch, useProviders, useStages, useStatus, useTags } from "../queries";
import { Avatar, Badge, Button, cx, EmptyState, ErrorNote, Icon, inputClass, Kbd, ListSkeleton, listShortcuts, PageHeader, ProgressBar, RelativeTime, selectClass, SearchKbd, ShortcutsHelp, useWide } from "../ui";
import { NewContactDialog } from "../components/CreateDialogs";
import { useRowNav } from "../components/RowNav";
import { TagCell } from "../components/Tags";
import { Onboarding } from "./Onboarding";

const ranges = {
  "": { label: "Any time", days: 0 },
  "1d": { label: "Last 24 hours", days: 1 },
  "7d": { label: "Last 7 days", days: 7 },
  "30d": { label: "Last 30 days", days: 30 },
  "90d": { label: "Last 90 days", days: 90 },
  custom: { label: "Custom range…", days: 0 },
} as const;
type Range = keyof typeof ranges;

/** The list's filters, exactly as they sit in the URL, so links and Back keep them. */
interface Filters {
  q: string;
  stage: string;
  tag: string;
  provider: string;
  company: CompanyID | null;
  signed: Range;
  after: string; // YYYY-MM-DD, with signed = custom
  before: string;
}

function readFilters(p: URLSearchParams): Filters {
  const signed = p.get("signed") ?? "";
  return {
    q: p.get("q") ?? "",
    stage: p.get("stage") ?? "",
    tag: p.get("tag") ?? "",
    provider: p.get("provider") ?? "",
    company: parseID<CompanyID>(p.get("company") ?? undefined),
    signed: signed in ranges ? (signed as Range) : "",
    after: p.get("after") ?? "",
    before: p.get("before") ?? "",
  };
}

/** Filters other than the search box, which is always visible. */
const activeFilters = (f: Filters) => [f.stage, f.tag, f.provider, f.company, f.signed].filter(Boolean).length;
const isFiltered = (f: Filters) => Boolean(f.q) || activeFilters(f) > 0;

const startOfDay = (ymd: string, plusDays = 0) => {
  const d = new Date(`${ymd}T00:00:00`);
  d.setDate(d.getDate() + plusDays);
  return d.toISOString();
};

/** Relative ranges resolve when the page is fetched, so the query key stays stable. */
function toQuery(f: Filters, cursor: string | undefined): ContactQuery {
  const q: ContactQuery = {
    q: f.q.trim() || undefined,
    stage: f.stage || undefined,
    tag: f.tag || undefined,
    provider: f.provider || undefined,
    company_id: f.company ?? undefined,
    cursor,
    limit: 50,
  };
  const days = ranges[f.signed].days;
  if (days) q.signed_up_after = new Date(Date.now() - days * 86_400_000).toISOString();
  if (f.signed === "custom") {
    if (f.after) q.signed_up_after = startOfDay(f.after);
    if (f.before) q.signed_up_before = startOfDay(f.before, 1);
  }
  return q;
}

/** The row to reselect when coming Back from a contact. */
let lastOpened: Contact["id"] | null = null;

export function Contacts() {
  const [params, setParams] = useSearchParams();
  const filters = readFilters(params);
  const filtered = isFiltered(filters);
  const navigate = useNavigate();
  const status = useStatus();
  const wide = useWide();

  const setFilter = (patch: Partial<Record<keyof Filters, string>>) => {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        for (const [k, v] of Object.entries(patch)) {
          if (v) next.set(k, v);
          else next.delete(k);
        }
        return next;
      },
      { replace: true },
    );
  };

  const list = useInfiniteQuery({
    queryKey: [...keys.contacts, filters],
    queryFn: ({ pageParam }) => api.contacts.search(toQuery(filters, pageParam)),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor || undefined,
    placeholderData: (prev) => prev,
    // New sign-ups show up without a reload; faster while waiting for the very
    // first. Paged-in lists stay put, since a refetch would re-request every page.
    refetchInterval: (q) => {
      const pages = q.state.data?.pages ?? [];
      if (pages.length > 1) return false;
      if (pages[0]?.items?.length) return 15_000;
      return filtered ? false : 4_000;
    },
  });
  const rows = list.data?.pages.flatMap((p) => p.items ?? []) ?? [];
  const empty = list.isSuccess && rows.length === 0;
  const stale = list.isPlaceholderData;

  const [creating, setCreating] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const prefetch = usePrefetch();

  const open = (c: Contact) => {
    lastOpened = c.id;
    void navigate(`/contacts/${c.id}`, { viewTransition: true });
  };

  const nav = useRowNav({
    count: rows.length,
    listRef,
    searchRef,
    onOpen: (i) => rows[i] && open(rows[i]),
    onNew: () => setCreating(true),
    onNearEnd: () => list.hasNextPage && !list.isFetchingNextPage && void list.fetchNextPage(),
  });

  const filterKey = params.toString();
  const { select, focusRow } = nav;
  useEffect(() => select(0), [filterKey, select]);

  // Declared after the reset above, so coming Back returns to the row that was opened.
  useEffect(() => {
    if (lastOpened == null || !list.data) return;
    const i = list.data.pages.flatMap((p) => p.items ?? []).findIndex((r) => r.id === lastOpened);
    lastOpened = null;
    if (i < 0) return;
    select(i);
    focusRow(i);
  }, [list.data, select, focusRow]);

  const total = status.data?.contacts;
  const clear = () => setParams({}, { replace: true });

  return (
    <>
      <PageHeader
        title={
          <>
            Contacts
            {total != null && total > 0 && <span className="ml-2 font-normal text-faint">{total.toLocaleString()}</span>}
          </>
        }
      >
        {(filtered || !empty) && <ShortcutsHelp shortcuts={listShortcuts("contact")} />}
        <Button variant="primary" size="sm" onClick={() => setCreating(true)} aria-label="New contact" title="New contact (n)">
          <Icon name="plus" className="size-3.5" />
          <span className="hidden sm:inline">New contact</span>
          <span className="ml-0.5 hidden md:contents">
            <Kbd inverse>N</Kbd>
          </span>
        </Button>
      </PageHeader>
      <NewContactDialog open={creating} onClose={() => setCreating(false)} />

      {(filtered || !empty) && <Toolbar filters={filters} setFilter={setFilter} searchRef={searchRef} filtered={filtered} wide={wide} onClear={clear} busy={stale} />}

      <div ref={listRef} aria-busy={stale || list.isPending} className={cx("relative min-h-0 flex-1 overflow-auto transition-opacity", stale && "opacity-60")}>
        {list.isPending ? (
          <ListSkeleton />
        ) : list.isError && rows.length === 0 ? (
          <div className="p-5">
            <ErrorNote error={list.error} />
          </div>
        ) : empty && !filtered ? (
          <Onboarding />
        ) : empty ? (
          <div className="mx-auto max-w-md px-4 py-16">
            <EmptyState icon="search" title="No contacts match" action={<Button onClick={clear}>Clear filters</Button>}>
              {filters.q ? `Nothing matches “${filters.q}” with these filters.` : "No one matches these filters."}
            </EmptyState>
          </div>
        ) : wide ? (
          <table className="w-full table-fixed border-separate border-spacing-0 text-sm" aria-label="Contacts">
            <thead className="sticky top-0 z-10 bg-header">
              <tr className="text-left text-xs whitespace-nowrap text-muted [&>th]:border-b [&>th]:border-line">
                <th scope="col" className="py-2 pr-3 pl-5 font-medium">Person</th>
                <th scope="col" className="w-[24%] px-3 py-2 font-medium">Company</th>
                <th scope="col" className="w-24 px-3 py-2 font-medium">Stage</th>
                <th scope="col" className="w-36 px-3 py-2 font-medium xl:w-52">Tags</th>
                <th scope="col" className="hidden w-28 px-3 py-2 font-medium xl:table-cell">Source</th>
                <th scope="col" className="w-28 py-2 pr-5 pl-3 text-right font-medium">Signed up</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((c, i) => (
                <ContactRow key={c.id} c={c} row={nav.rowProps(i)} onOpen={() => open(c)} onPrefetch={() => prefetch.contact(c.id)} />
              ))}
            </tbody>
          </table>
        ) : (
          <ul aria-label="Contacts" className="divide-y divide-line">
            {rows.map((c, i) => (
              <ContactItem key={c.id} c={c} row={nav.rowProps(i)} onOpen={() => (lastOpened = c.id)} onPrefetch={() => prefetch.contact(c.id)} />
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

type RowProps = ReturnType<ReturnType<typeof useRowNav>["rowProps"]>;

function ContactRow({ c, row, onOpen, onPrefetch }: { c: Contact; row: RowProps; onOpen: () => void; onPrefetch: () => void }) {
  const label = contactLabel(c);
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
          <Avatar src={c.avatar_url} label={label} size={28} />
          <div className="min-w-0">
            <Link to={`/contacts/${c.id}`} viewTransition onClick={(e) => e.stopPropagation()} className="block truncate font-medium hover:underline" tabIndex={-1} title={label}>
              {label}
            </Link>
            {c.name && (
              <div className="truncate text-xs text-muted" title={c.email}>
                {c.email}
              </div>
            )}
          </div>
        </div>
      </td>
      <td className="truncate px-3 text-muted">{c.company ? companyLabel(c.company) : <span className="text-faint">—</span>}</td>
      <td className="px-3">
        <Badge className="min-w-0">
          <span className="truncate">{c.stage}</span>
        </Badge>
      </td>
      <td className="px-3">
        <TagCell tags={c.tags ?? []} companyTags={c.company_tags ?? []} />
      </td>
      <td className="hidden truncate px-3 text-xs text-muted xl:table-cell">{(c.providers ?? []).join(", ") || <span className="text-faint">manual</span>}</td>
      <td className="py-1.5 pr-5 pl-3 text-right text-xs whitespace-nowrap text-muted">
        <RelativeTime iso={c.first_seen_at} />
      </td>
    </tr>
  );
}

function ContactItem({ c, row, onOpen, onPrefetch }: { c: Contact; row: RowProps; onOpen: () => void; onPrefetch: () => void }) {
  const label = contactLabel(c);
  return (
    <li>
      <Link
        to={`/contacts/${c.id}`}
        viewTransition
        {...row}
        onFocus={() => (row.onFocus(), onPrefetch())}
        onPointerDown={onPrefetch}
        onClick={onOpen}
        className="flex items-center gap-3 px-4 py-2.5 transition-colors outline-none focus-visible:bg-subtle active:bg-subtle"
      >
        <Avatar src={c.avatar_url} label={label} size={32} />
        <div className="min-w-0 flex-1">
          <div className="truncate font-medium">{label}</div>
          {c.name && <div className="truncate text-xs text-muted">{c.email}</div>}
          {c.company && <div className="truncate text-xs text-faint">{companyLabel(c.company)}</div>}
        </div>
        <Badge className="max-w-[40%]">
          <span className="truncate">{c.stage}</span>
        </Badge>
      </Link>
    </li>
  );
}

function Toolbar({
  filters,
  setFilter,
  searchRef,
  filtered,
  wide,
  onClear,
  busy,
}: {
  filters: Filters;
  setFilter: (p: Partial<Record<keyof Filters, string>>) => void;
  searchRef: RefObject<HTMLInputElement | null>;
  filtered: boolean;
  wide: boolean;
  onClear: () => void;
  busy: boolean;
}) {
  const stages = useStages();
  const tags = useTags();
  const providers = useProviders();
  const active = activeFilters(filters);
  // On a phone the filters fold away behind one button, open from the start if any is set.
  const [showFilters, setShowFilters] = useState(active > 0);

  // The input is local so typing stays instant; the URL (and the fetch) follows after a pause.
  const [q, setQ] = useState(filters.q);
  useEffect(() => setQ(filters.q), [filters.q]);
  useEffect(() => {
    if (q === filters.q) return;
    const t = window.setTimeout(() => setFilter({ q }), 200);
    return () => window.clearTimeout(t);
  });

  const select = cx(selectClass, "w-full md:w-auto md:min-w-0 md:max-w-40 md:flex-1");

  return (
    <div className="relative flex shrink-0 flex-wrap items-center gap-2 border-b border-line px-3 py-2.5 md:px-5">
      <ProgressBar active={busy} />
      <div className="relative min-w-0 flex-1 md:max-w-64 md:min-w-44 md:flex-[1.5]">
        <Icon name="search" className="pointer-events-none absolute top-2 left-2.5 size-4 text-faint" />
        <input
          ref={searchRef}
          type="search"
          aria-label="Search contacts"
          placeholder="Search contacts"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          className={cx(inputClass, "peer pl-8 lg:pr-8")}
        />
          <SearchKbd />
      </div>
      {!wide && (
        <Button onClick={() => setShowFilters(!showFilters)} aria-expanded={showFilters} aria-controls="contact-filters">
          <Icon name="filter" className="size-3.5" />
          Filters
          {active > 0 && <span className="grid h-4 min-w-4 place-items-center rounded-full bg-primary px-1 text-2xs text-primary-fg">{active}</span>}
        </Button>
      )}
      {(wide || showFilters) && (
        <div id="contact-filters" className="grid w-full grid-cols-2 gap-2 md:contents">
          <select aria-label="Stage" className={select} value={filters.stage} onChange={(e) => setFilter({ stage: e.target.value })}>
            <option value="">All stages</option>
            {stages.data?.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
          <select aria-label="Tag" className={select} value={filters.tag} onChange={(e) => setFilter({ tag: e.target.value })}>
            <option value="">All tags</option>
            {tags.data?.map(({ tag, count }) => (
              <option key={tag} value={tag}>
                {tag} ({count})
              </option>
            ))}
          </select>
          <select aria-label="Source" className={select} value={filters.provider} onChange={(e) => setFilter({ provider: e.target.value })}>
            <option value="">All sources</option>
            {providers.data?.map((p) => (
              <option key={p.name} value={p.name}>
                {p.title}
              </option>
            ))}
          </select>
          <select aria-label="Signed up" className={select} value={filters.signed} onChange={(e) => setFilter({ signed: e.target.value, after: "", before: "" })}>
            {Object.entries(ranges).map(([k, r]) => (
              <option key={k} value={k}>
                {r.label}
              </option>
            ))}
          </select>
          {filters.signed === "custom" && (
            <span className="col-span-2 flex items-center gap-1.5 text-xs text-muted">
              <input type="date" aria-label="Signed up on or after" className={cx(inputClass, "md:w-36")} value={filters.after} onChange={(e) => setFilter({ after: e.target.value })} />
              to
              <input type="date" aria-label="Signed up on or before" className={cx(inputClass, "md:w-36")} value={filters.before} onChange={(e) => setFilter({ before: e.target.value })} />
            </span>
          )}
          {filters.company != null && <CompanyChip id={filters.company} onRemove={() => setFilter({ company: "" })} />}
        </div>
      )}
      {filtered && (
        <Button variant="ghost" size="sm" onClick={onClear}>
          Clear
        </Button>
      )}
    </div>
  );
}

function CompanyChip({ id, onRemove }: { id: CompanyID; onRemove: () => void }) {
  const company = useQuery({ queryKey: keys.company(id), queryFn: () => api.companies.get(id) });
  return (
    <span className="col-span-2 inline-flex h-8 min-w-0 items-center gap-1 rounded-control border border-line-strong bg-subtle pr-1 pl-2.5 text-sm md:col-span-1">
      <Icon name="building" className="size-3.5 text-muted" />
      <span className="min-w-0 flex-1 truncate">{company.data ? companyLabel(company.data) : "Company"}</span>
      <Button size="sm" variant="ghost" aria-label="Remove company filter" onClick={onRemove} className="h-6 w-6 px-0">
        <Icon name="x" className="size-3.5" />
      </Button>
    </span>
  );
}
