import { queryOptions, useQuery, useQueryClient, type InfiniteData, type QueryClient } from "@tanstack/react-query";
import { api, type Company, type CompanyDetail, type CompanyID, type Contact, type ContactDetail, type ContactID, type JobState, type Page } from "./api";

/** Query keys in one place, so a mutation can invalidate exactly what it touched. */
export const keys = {
  me: ["me"] as const,
  contacts: ["contacts"] as const,
  contact: (id: ContactID) => ["contact", id] as const,
  companies: ["companies"] as const,
  company: (id: CompanyID) => ["company", id] as const,
  stages: ["stages"] as const,
  tags: ["tags"] as const,
  providers: ["providers"] as const,
  status: ["status"] as const,
  keys: ["keys"] as const,
  apps: ["apps"] as const,
  consent: (request: string) => ["consent", request] as const,
  users: ["users"] as const,
  account: ["account"] as const,
  authOptions: ["auth-options"] as const,
  secondFactor: ["second-factor"] as const,
  invite: (token: string) => ["invite", token] as const,
  setup: (token: string) => ["setup", token] as const,
};

export const useMe = () => useQuery({ queryKey: keys.me, queryFn: api.me.get, staleTime: Infinity });

export const useAuthOptions = () => useQuery({ queryKey: keys.authOptions, queryFn: api.auth.options, staleTime: 60_000 });

export const useAccount = () => useQuery({ queryKey: keys.account, queryFn: api.account.get });

export const useStages = () =>
  useQuery({ queryKey: keys.stages, queryFn: async () => (await api.stages.list()) ?? [], staleTime: 60_000 });

export const useTags = () =>
  useQuery({
    queryKey: keys.tags,
    queryFn: async () =>
      Object.entries((await api.tags()) ?? {})
        .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
        .map(([tag, count]) => ({ tag, count })),
  });

export const useProviders = (opts: { poll?: boolean } = {}) =>
  useQuery({
    queryKey: keys.providers,
    queryFn: async () => (await api.providers.list()) ?? [],
    refetchInterval: opts.poll ? 5000 : false,
  });

export const useStatus = (opts: { poll?: boolean } = {}) =>
  useQuery({ queryKey: keys.status, queryFn: api.status, refetchInterval: opts.poll ? 10_000 : false });

/** The records every cached page under a key prefix holds, paged lists and single pages alike. */
function cached<T>(qc: QueryClient, prefix: readonly unknown[]): T[] {
  return qc.getQueriesData<InfiniteData<Page<T>> | Page<T>>({ queryKey: prefix }).flatMap(([, d]) => (!d ? [] : "pages" in d ? d.pages.flatMap((p) => p.items ?? []) : (d.items ?? [])));
}

/**
 * A contact as a list or company page already has it, so its page draws at
 * once. Notes, sources and activity are empty until the detail arrives;
 * isPlaceholderData says so.
 */
function seedContact(qc: QueryClient, id: ContactID): ContactDetail | undefined {
  const fromCompanies = qc.getQueriesData<CompanyDetail>({ queryKey: ["company"] }).flatMap(([, d]) => d?.contacts ?? []);
  const c = [...cached<Contact>(qc, keys.contacts), ...fromCompanies].find((c) => c.id === id);
  return c && { ...c, identities: [], notes: [], activity: [] };
}

function seedCompany(qc: QueryClient, id: CompanyID): CompanyDetail | undefined {
  const c = cached<Company>(qc, keys.companies).find((c) => c.id === id);
  return c && { ...c, contacts: [], activity: [] };
}

export const contactQuery = (qc: QueryClient, id: ContactID) =>
  queryOptions({ queryKey: keys.contact(id), queryFn: () => api.contacts.get(id), placeholderData: () => seedContact(qc, id) });

export const companyQuery = (qc: QueryClient, id: CompanyID) =>
  queryOptions({ queryKey: keys.company(id), queryFn: () => api.companies.get(id), placeholderData: () => seedCompany(qc, id) });

const inFlight = (s: JobState | undefined) => s === "pending" || s === "running";

/** Re-reads a record every 2s while its enrichment is queued or running, so the page fills in by itself. */
function useEnrichmentPoll() {
  const status = useStatus();
  const live = Boolean(status.data?.enrichment_enabled && !status.data.enrichment_halt);
  return (q: { state: { data?: { enrichment: JobState } } }) => (live && inFlight(q.state.data?.enrichment) ? 2000 : false);
}

export function useContact(id: ContactID) {
  const qc = useQueryClient();
  return useQuery({ ...contactQuery(qc, id), refetchInterval: useEnrichmentPoll() });
}

export function useCompany(id: CompanyID) {
  const qc = useQueryClient();
  return useQuery({ ...companyQuery(qc, id), refetchInterval: useEnrichmentPoll() });
}

/** Starts loading a record when the pointer or focus reaches its row, so opening it is instant. */
export function usePrefetch() {
  const qc = useQueryClient();
  return {
    contact: (id: ContactID) => void qc.prefetchQuery(contactQuery(qc, id)),
    company: (id: CompanyID) => void qc.prefetchQuery(companyQuery(qc, id)),
  };
}
