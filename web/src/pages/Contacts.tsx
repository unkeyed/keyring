import { useEffect, useRef, useState, type RefObject } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearchParams } from "react-router";
import { api, parseID, type CompanyID, type Contact, type ContactQuery } from "../api";
import { companyLabel, contactLabel } from "../format";
import { keys, useProviders, useStages, useStatus, useTags } from "../queries";
import { Avatar, Badge, Button, cx, ErrorNote, Icon, inputClass, Kbd, PageHeader, RelativeTime, selectClass, Spinner } from "../ui";
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

const isFiltered = (f: Filters) => Boolean(f.q || f.stage || f.tag || f.provider || f.company || f.signed);

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
    // New sign-ups show up without a reload; faster while waiting for the first.
    // Paged-in lists stay put, since a refetch would re-request every page.
    refetchInterval: (q) => {
      const pages = q.state.data?.pages ?? [];
      if (pages.length > 1) return false;
      return pages[0]?.items?.length ? 15_000 : 4_000;
    },
  });
  const rows = list.data?.pages.flatMap((p) => p.items ?? []) ?? [];
  const empty = list.isSuccess && rows.length === 0;

  const [selected, setSelected] = useState(0);
  const searchRef = useRef<HTMLInputElement>(null);
  const tableRef = useRef<HTMLTableSectionElement>(null);

  const filterKey = params.toString();
  useEffect(() => setSelected(0), [filterKey]);

  // Declared after the reset above, so coming Back reselects the row that was opened.
  useEffect(() => {
    if (lastOpened == null || rows.length === 0) return;
    const i = rows.findIndex((r) => r.id === lastOpened);
    if (i >= 0) setSelected(i);
    lastOpened = null;
  }, [rows]);

  const sel = Math.min(selected, Math.max(rows.length - 1, 0));

  useEffect(() => {
    tableRef.current?.querySelector(`[data-row="${sel}"]`)?.scrollIntoView({ block: "nearest" });
  }, [sel]);

  const open = (c: Contact) => {
    lastOpened = c.id;
    void navigate(`/contacts/${c.id}`);
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const t = e.target as HTMLElement;
      const typing = t.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName);
      if (typing) {
        if (e.key === "Escape" && t === searchRef.current) searchRef.current.blur();
        if ((e.key === "ArrowDown" || e.key === "Enter") && t === searchRef.current && rows.length) {
          e.preventDefault();
          searchRef.current.blur();
          if (e.key === "Enter") open(rows[sel]);
        }
        return;
      }
      if (document.querySelector("dialog[open]")) return;
      const onControl = t.closest("button, a") !== null;
      if (e.key === "/") {
        e.preventDefault();
        searchRef.current?.focus();
        searchRef.current?.select();
      } else if (e.key === "j" || e.key === "ArrowDown") {
        e.preventDefault();
        const next = Math.min(sel + 1, rows.length - 1);
        setSelected(next);
        if (next >= rows.length - 5 && list.hasNextPage && !list.isFetchingNextPage) void list.fetchNextPage();
      } else if (e.key === "k" || e.key === "ArrowUp") {
        e.preventDefault();
        setSelected(Math.max(sel - 1, 0));
      } else if ((e.key === "Enter" || e.key === "o") && rows[sel] && !onControl) {
        e.preventDefault();
        open(rows[sel]);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  const total = status.data?.contacts;

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
        <span className={cx("hidden items-center gap-1 text-xs text-faint", (filtered || !empty) && "xl:flex")}>
          <Kbd>/</Kbd> search <Kbd>j</Kbd>
          <Kbd>k</Kbd> move <Kbd>↵</Kbd> open
        </span>
      </PageHeader>

      {(filtered || !empty) && (
        <Toolbar filters={filters} setFilter={setFilter} searchRef={searchRef} filtered={filtered} onClear={() => setParams({}, { replace: true })} />
      )}

      <div className="min-h-0 flex-1 overflow-auto">
        {list.isPending ? (
          <div className="grid h-40 place-items-center">
            <Spinner />
          </div>
        ) : list.isError && rows.length === 0 ? (
          <div className="p-5">
            <ErrorNote error={list.error} />
          </div>
        ) : empty && !filtered ? (
          <Onboarding />
        ) : empty ? (
          <div className="mx-auto mt-20 max-w-xs text-center">
            <p className="font-medium">No contacts match these filters</p>
            <Button className="mt-3" onClick={() => setParams({}, { replace: true })}>
              Clear filters
            </Button>
          </div>
        ) : (
          <table className="w-full table-fixed border-collapse text-[13px]" aria-label="Contacts">
            <thead className="sticky top-0 z-10 bg-surface">
              <tr className="border-b border-line text-left text-xs text-muted">
                <th scope="col" className="w-[34%] py-2 pr-3 pl-5 font-medium">Person</th>
                <th scope="col" className="w-[18%] px-3 py-2 font-medium">Company</th>
                <th scope="col" className="w-[11%] px-3 py-2 font-medium">Stage</th>
                <th scope="col" className="w-[15%] px-3 py-2 font-medium">Tags</th>
                <th scope="col" className="w-[11%] px-3 py-2 font-medium">Source</th>
                <th scope="col" className="w-[11%] py-2 pr-5 pl-3 text-right font-medium">Signed up</th>
              </tr>
            </thead>
            <tbody ref={tableRef}>
              {rows.map((c, i) => (
                <ContactRow key={c.id} c={c} index={i} selected={i === sel} onOpen={() => open(c)} onHover={() => setSelected(i)} />
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

function ContactRow({ c, index, selected, onOpen, onHover }: { c: Contact; index: number; selected: boolean; onOpen: () => void; onHover: () => void }) {
  const label = contactLabel(c);
  return (
    <tr
      data-row={index}
      aria-current={selected || undefined}
      onClick={onOpen}
      onMouseMove={selected ? undefined : onHover}
      className={cx("h-11 cursor-default border-b border-line", selected ? "bg-subtle" : "hover:bg-subtle/60")}
    >
      <td className={cx("py-1.5 pr-3 pl-5", selected && "shadow-[inset_2px_0_0_var(--accent)]")}>
        <div className="flex min-w-0 items-center gap-2.5">
          <Avatar src={c.avatar_url} label={label} size={26} />
          <div className="min-w-0">
            <Link to={`/contacts/${c.id}`} onClick={(e) => e.stopPropagation()} className="block truncate font-medium hover:underline" tabIndex={-1}>
              {label}
            </Link>
            {c.name && <div className="truncate text-xs text-muted">{c.email}</div>}
          </div>
        </div>
      </td>
      <td className="truncate px-3 text-muted">{c.company ? companyLabel(c.company) : <span className="text-faint">—</span>}</td>
      <td className="px-3">
        <Badge>{c.stage}</Badge>
      </td>
      <td className="px-3">
        <div className="flex gap-1 overflow-hidden">
          {(c.tags ?? []).slice(0, 3).map((t) => (
            <Badge key={t} tone="accent">
              {t}
            </Badge>
          ))}
          {(c.tags?.length ?? 0) > 3 && <span className="text-xs text-faint">+{c.tags!.length - 3}</span>}
        </div>
      </td>
      <td className="truncate px-3 text-xs text-muted">{(c.providers ?? []).join(", ") || <span className="text-faint">api</span>}</td>
      <td className="py-1.5 pr-5 pl-3 text-right text-xs text-muted">
        <RelativeTime iso={c.first_seen_at} />
      </td>
    </tr>
  );
}

function Toolbar({
  filters,
  setFilter,
  searchRef,
  filtered,
  onClear,
}: {
  filters: Filters;
  setFilter: (p: Partial<Record<keyof Filters, string>>) => void;
  searchRef: RefObject<HTMLInputElement | null>;
  filtered: boolean;
  onClear: () => void;
}) {
  const stages = useStages();
  const tags = useTags();
  const providers = useProviders();

  // The input is local so typing stays instant; the URL (and the fetch) follows after a pause.
  const [q, setQ] = useState(filters.q);
  useEffect(() => setQ(filters.q), [filters.q]);
  useEffect(() => {
    if (q === filters.q) return;
    const t = window.setTimeout(() => setFilter({ q }), 200);
    return () => window.clearTimeout(t);
  });

  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-line px-5 py-2.5">
      <div className="relative w-64">
        <Icon name="search" className="pointer-events-none absolute top-2 left-2.5 size-4 text-faint" />
        <input
          ref={searchRef}
          type="search"
          aria-label="Search contacts"
          placeholder="Search name, email, company"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          className={cx(inputClass, "pl-8")}
        />
      </div>
      <select aria-label="Stage" className={selectClass} value={filters.stage} onChange={(e) => setFilter({ stage: e.target.value })}>
        <option value="">All stages</option>
        {stages.data?.map((s) => (
          <option key={s} value={s}>
            {s}
          </option>
        ))}
      </select>
      <select aria-label="Tag" className={selectClass} value={filters.tag} onChange={(e) => setFilter({ tag: e.target.value })}>
        <option value="">All tags</option>
        {tags.data?.map(({ tag, count }) => (
          <option key={tag} value={tag}>
            {tag} ({count})
          </option>
        ))}
      </select>
      <select aria-label="Source" className={selectClass} value={filters.provider} onChange={(e) => setFilter({ provider: e.target.value })}>
        <option value="">All sources</option>
        {providers.data?.map((p) => (
          <option key={p.name} value={p.name}>
            {p.title}
          </option>
        ))}
      </select>
      <select aria-label="Signed up" className={selectClass} value={filters.signed} onChange={(e) => setFilter({ signed: e.target.value, after: "", before: "" })}>
        {Object.entries(ranges).map(([k, r]) => (
          <option key={k} value={k}>
            {r.label}
          </option>
        ))}
      </select>
      {filters.signed === "custom" && (
        <span className="flex items-center gap-1.5 text-xs text-muted">
          <input type="date" aria-label="Signed up on or after" className={cx(inputClass, "w-36")} value={filters.after} onChange={(e) => setFilter({ after: e.target.value })} />
          to
          <input type="date" aria-label="Signed up on or before" className={cx(inputClass, "w-36")} value={filters.before} onChange={(e) => setFilter({ before: e.target.value })} />
        </span>
      )}
      {filters.company != null && <CompanyChip id={filters.company} onRemove={() => setFilter({ company: "" })} />}
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
    <span className="inline-flex h-8 items-center gap-1 rounded-md border border-line-strong bg-subtle pr-1 pl-2.5 text-[13px]">
      <Icon name="building" className="size-3.5 text-muted" />
      {company.data ? companyLabel(company.data) : "Company"}
      <Button size="sm" variant="ghost" aria-label="Remove company filter" onClick={onRemove}>
        <Icon name="x" className="size-3.5" />
      </Button>
    </span>
  );
}
