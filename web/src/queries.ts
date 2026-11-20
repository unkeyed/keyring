import { useQuery } from "@tanstack/react-query";
import { api, type CompanyID, type ContactID } from "./api";

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
