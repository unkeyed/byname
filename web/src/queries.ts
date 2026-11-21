import { useQuery } from "@tanstack/react-query";
import { api, type Grouping, type GscStatus, type ResultsFilter, type Run } from "./api";

/** Query keys in one place, so a mutation can invalidate exactly what it touched. */
export const keys = {
  me: ["me"] as const,
  keys: ["keys"] as const,
  apps: ["apps"] as const,
  consent: (request: string) => ["consent", request] as const,
  users: ["users"] as const,
  account: ["account"] as const,
  authOptions: ["auth-options"] as const,
  secondFactor: ["second-factor"] as const,
  invite: (token: string) => ["invite", token] as const,
  setup: (token: string) => ["setup", token] as const,
  settings: ["settings"] as const,
  brands: ["brands"] as const,
  surfaces: ["surfaces"] as const,
  /** Everything a check changes sits under this prefix, so a finished run refreshes it in one call. */
  results: ["results"] as const,
  runs: ["results", "runs"] as const,
  run: (id: number) => ["results", "run", id] as const,
  jobs: ["results", "jobs"] as const,
  rates: (f: ResultsFilter, by: Grouping) => ["results", "rates", f, by] as const,
  summary: (f: ResultsFilter) => ["results", "summary", f] as const,
  competitors: (f: ResultsFilter, by: Grouping) => ["results", "competitors", f, by] as const,
  questions: (run: number, f: ResultsFilter) => ["results", "questions", run, f.surface, f.model] as const,
  history: (question: number, since?: string) => ["results", "history", question, since] as const,
  answer: (id: number) => ["answer", id] as const,
  compare: (before: number, after: number) => ["results", "compare", before, after] as const,
  gaps: (run: number) => ["results", "gaps", run] as const,
  /**
   * Search Console reads sit under this prefix. Each carries the stored
   * data's version, so a finished sync refetches them without a timer.
   */
  search: ["search"] as const,
  gsc: ["search", "gsc"] as const,
  gscProperties: ["search", "gsc", "properties"] as const,
  searchSummary: (v: string) => ["search", "summary", v] as const,
  searchDaily: (from: string | undefined, v: string) => ["search", "daily", from, v] as const,
  searchBrand: (from: string | undefined, v: string) => ["search", "brand", from, v] as const,
  searchPages: (limit: number, v: string) => ["search", "pages", limit, v] as const,
  searchQueries: (limit: number, nonBrand: boolean, v: string) => ["search", "queries", limit, nonBrand, v] as const,
  goals: (v: string) => ["search", "goals", v] as const,
};

/** Changes whenever a sync or a disconnect changes what the read API returns. */
export const dataVersion = (s: GscStatus) => [s.data_property, s.last_sync_at, s.latest_final_day, s.partial_days.join()].join("|");

/** A picked property waits for its first sync, which starts within a minute. */
const awaitingSync = (s: GscStatus) => s.syncing || (s.connected && s.property !== "" && !s.last_sync_at && !s.last_sync_error);

/** The Search Console connection. Polls while a sync is due or running. */
export const useGsc = () =>
  useQuery({
    queryKey: keys.gsc,
    queryFn: api.gsc.status,
    refetchInterval: (q) => (q.state.data && awaitingSync(q.state.data) ? 4000 : false),
  });

export const useMe = () => useQuery({ queryKey: keys.me, queryFn: api.me.get, staleTime: Infinity });

export const useAuthOptions = () => useQuery({ queryKey: keys.authOptions, queryFn: api.auth.options, staleTime: 60_000 });

export const useAccount = () => useQuery({ queryKey: keys.account, queryFn: api.account.get });

export const useSettings = () => useQuery({ queryKey: keys.settings, queryFn: api.settings });
export const useBrands = () => useQuery({ queryKey: keys.brands, queryFn: api.brands, staleTime: 60_000 });
export const useSurfaces = () => useQuery({ queryKey: keys.surfaces, queryFn: api.surfaces, staleTime: 60_000 });

export const isOpen = (r: Run) => r.status === "queued" || r.status === "running";

/** Newest first. Polls while a check is queued or running, so every page sees it finish. */
export const useRuns = () =>
  useQuery({
    queryKey: keys.runs,
    queryFn: () => api.runs.list(100),
    refetchInterval: (q) => (q.state.data?.some(isOpen) ? 2000 : false),
  });

/** Runs that recorded answers, newest first: the ones results can show. */
export const useAnsweredRuns = () => {
  const runs = useRuns();
  return { ...runs, data: runs.data?.filter((r) => r.calls > 0) };
};
