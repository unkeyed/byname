import { keepPreviousData, useQuery, type QueryClient } from "@tanstack/react-query";
import { useLoading } from "./ui";
import { api, type Grouping, type Line, type ResultsFilter, type Run, type Scope, type SearchStatus, type SocialMentionQuery } from "./api";

/** Query keys in one place, so a mutation can invalidate exactly what it touched. */
export const keys = {
  me: ["me"] as const,
  entities: ["entities"] as const,
  keys: ["keys"] as const,
  apps: ["apps"] as const,
  consent: (request: string) => ["consent", request] as const,
  users: ["users"] as const,
  mcpServer: ["mcp-server"] as const,
  mcpServerGrade: ["mcp-server", "grade"] as const,
  account: ["account"] as const,
  authOptions: ["auth-options"] as const,
  secondFactor: ["second-factor"] as const,
  invite: (token: string) => ["invite", token] as const,
  setup: (token: string) => ["setup", token] as const,
  settings: ["settings"] as const,
  brands: ["brands"] as const,
  surfaces: ["surfaces"] as const,
  models: (surface: string, route: string, baseURL: string) => ["models", surface, route, baseURL] as const,
  questionSet: ["question-set"] as const,
  questionSets: ["question-sets"] as const,
  credentials: ["credentials"] as const,
  onboarding: ["onboarding"] as const,
  estimate: ["estimate"] as const,
  /** Under results, so a finished check refreshes the month's spend. */
  costs: ["results", "costs"] as const,
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
  brief: (question: number, run: number, f: ResultsFilter = {}) => ["results", "brief", question, run, f.surface, f.model] as const,
  gaps: (run: number, f: ResultsFilter = {}) => ["results", "gaps", run, f.surface, f.model] as const,
  /** Every gaps query, whatever its run and filter. */
  allGaps: ["results", "gaps"] as const,
  handled: ["results", "handled"] as const,
  prompts: (f: ResultsFilter) => ["results", "prompts", f] as const,
  sources: (f: ResultsFilter) => ["results", "sources", f] as const,
  answers: (run: number) => ["results", "answers", run] as const,
  engine: (l: Line, since?: string) => ["results", "engine", l, since] as const,
  /** Under results, so a finished check refreshes the unread count too. */
  notifications: ["results", "notifications"] as const,
  channels: ["channels"] as const,
  deliveries: ["channels", "deliveries"] as const,
  /**
   * Search engine reads sit under this prefix. Each carries its engine's
   * stored data version, so a finished sync refetches them without a timer.
   */
  search: ["search"] as const,
  searchProviders: ["search", "providers"] as const,
  searchSites: (p: string) => ["search", "providers", p, "sites"] as const,
  searchSummary: (p: string, v: string) => ["search", p, "summary", v] as const,
  searchDaily: (p: string, from: string | undefined, v: string) => ["search", p, "daily", from, v] as const,
  searchBrand: (p: string, from: string | undefined, v: string) => ["search", p, "brand", from, v] as const,
  searchPages: (p: string, limit: number, v: string) => ["search", p, "pages", limit, v] as const,
  searchQueries: (p: string, limit: number, nonBrand: boolean, v: string) => ["search", p, "queries", limit, nonBrand, v] as const,
  goals: (v: string) => ["search", "goals", v] as const,
  /** Site changes, control pages, the repo and the read settings sit under this prefix. */
  changes: ["changes"] as const,
  changeList: ["changes", "list"] as const,
  change: (id: number) => ["changes", "detail", id] as const,
  changeMarkers: (from: string, to: string) => ["changes", "markers", from, to] as const,
  changeSettings: ["changes", "settings"] as const,
  controlPages: ["changes", "control-pages"] as const,
  repo: ["changes", "repo"] as const,
  /** Social mentions, digests, settings, the left-out list and the filters sit under this prefix. */
  social: ["social"] as const,
  socialMentions: (q: SocialMentionQuery) => ["social", "mentions", q] as const,
  socialDigest: (day?: string) => ["social", "digest", day] as const,
  socialSettings: ["social", "settings"] as const,
  leftOut: ["social", "left-out"] as const,
  socialFilters: ["social", "filters"] as const,
  /** Under results, so a finished check refreshes the list. */
  otherBrands: (f: ResultsFilter) => ["results", "other-brands", f] as const,
  hiddenBrands: ["results", "other-brands", "hidden"] as const,
  otherBrandsSettings: ["other-brands-settings"] as const,
  brandBackfill: ["results", "brand-backfill"] as const,
  logos: ["logos"] as const,
};

export const useSearchSites = (p: string, enabled = true) => useQuery({ queryKey: keys.searchSites(p), queryFn: () => api.search.sites(p), enabled });

/** Changes whenever a sync or a disconnect changes what the engine's read API returns. */
export const dataVersion = (s: SearchStatus) => [s.provider, s.data_site, s.last_sync_at, s.latest_final_day, s.partial_days.join()].join("|");

/** A picked site waits for its first sync, which starts within a minute. */
const awaitingSync = (s: SearchStatus) => s.syncing || (s.connected && s.site !== "" && !s.last_sync_at && !s.last_sync_error);

/** Puts one engine's new status, as a connect or site change answers, into the providers list. */
export function storeEngine(qc: QueryClient, st: SearchStatus) {
  qc.setQueryData<{ providers: SearchStatus[] }>(keys.searchProviders, (old) => old && { providers: old.providers.map((p) => (p.provider === st.provider ? st : p)) });
}

/** Every search engine, connected or not, in the order to show them. Polls while a sync is due or running. */
export const useSearchProviders = () =>
  useQuery({
    queryKey: keys.searchProviders,
    queryFn: api.search.providers,
    select: (d) => d.providers,
    refetchInterval: (q) => (q.state.data?.providers.some(awaitingSync) ? 4000 : false),
  });

export const useChanges = () => useQuery({ queryKey: keys.changeList, queryFn: api.changes.list });
export const useChangeSettings = () => useQuery({ queryKey: keys.changeSettings, queryFn: api.changes.settings, staleTime: 60_000 });
export const useControlPages = () => useQuery({ queryKey: keys.controlPages, queryFn: api.controlPages.list });
export const useRepo = () => useQuery({ queryKey: keys.repo, queryFn: api.repo.get });
/** Polls while a review of the tool docs is queued or running. */
export const useMcpServer = () =>
  useQuery({
    queryKey: keys.mcpServer,
    queryFn: api.mcpServer.get,
    refetchInterval: (q) => (q.state.data && ["queued", "running"].includes(q.state.data.review.status) ? 3000 : false),
  });

export const useSocialMentions = (q: SocialMentionQuery) => useQuery({ queryKey: keys.socialMentions(q), queryFn: () => api.social.mentions(q) });
/** Keeps the shown period while another loads, so the digest card never empties under the reader. */
export const useSocialDigest = (day?: string) => useQuery({ queryKey: keys.socialDigest(day), queryFn: () => api.social.digest(day), placeholderData: keepPreviousData });
export const useSocialSettings = () => useQuery({ queryKey: keys.socialSettings, queryFn: api.social.settings });
export const useLeftOut = () => useQuery({ queryKey: keys.leftOut, queryFn: api.social.leftOut });
export const useSocialFilters = () => useQuery({ queryKey: keys.socialFilters, queryFn: api.social.filters });

/** Keeps the previous range's list while a new range loads, so the page never collapses and regrows. */
export const useOtherBrands = (f: ResultsFilter, enabled = true) =>
  useQuery({ queryKey: keys.otherBrands(f), queryFn: () => api.otherBrands.list(f), enabled, placeholderData: (prev) => prev });
export const useOtherBrandsSettings = () => useQuery({ queryKey: keys.otherBrandsSettings, queryFn: api.otherBrands.settings });
/** Polls while a pass over past answers is queued or running. */
export const useBrandBackfill = () =>
  useQuery({
    queryKey: keys.brandBackfill,
    queryFn: api.otherBrands.backfill,
    refetchInterval: (q) => (q.state.data?.latest && ["queued", "running"].includes(q.state.data.latest.status) ? 3000 : false),
  });
/** Every stored logo by brand id. A brand missing here draws its monogram. */
export const useLogos = () =>
  useQuery({ queryKey: keys.logos, queryFn: async () => new Map((await api.logos()).map((l) => [l.brand_id, l])), staleTime: 60_000 });

export const useMe = () => useQuery({ queryKey: keys.me, queryFn: api.me.get, staleTime: Infinity });

export const useEntities = () => useQuery({ queryKey: keys.entities, queryFn: api.entities.list, staleTime: 60_000 });

export const useAuthOptions = () => useQuery({ queryKey: keys.authOptions, queryFn: api.auth.options, staleTime: 60_000 });

export const useAccount = () => useQuery({ queryKey: keys.account, queryFn: api.account.get });

export const useSettings = () => useQuery({ queryKey: keys.settings, queryFn: api.settings.get });
export const useBrands = () => useQuery({ queryKey: keys.brands, queryFn: api.brands.list, staleTime: 60_000 });
export const useSurfaces = () => useQuery({ queryKey: keys.surfaces, queryFn: api.surfaces.list, staleTime: 60_000 });
/**
 * GET /v1/costs. It holds the nearest LoadBoundary on its first load. Its key
 * sits under results, so a finished check refreshes it; a setup change that
 * moves a cost invalidates keys.costs.
 */
export function useCosts() {
  const costs = useQuery({ queryKey: keys.costs, queryFn: api.costs });
  useLoading(costs.isPending);
  return costs;
}
export const useOnboarding = () => useQuery({ queryKey: keys.onboarding, queryFn: api.onboarding.get });

/**
 * The current question set, or null before the first one is saved. It asks
 * for the version list first, because the current set is a 404 until then.
 */
export const useQuestions = () =>
  useQuery({
    queryKey: keys.questionSet,
    queryFn: async () => ((await api.questions.versions())?.length ? api.questions.current() : null),
  });

/** Whether the signed-in person holds a scope. Viewers see no write actions. */
export const useCan = (scope: Scope) => useMe().data?.scopes.includes(scope) ?? false;
export const useIsAdmin = () => useMe().data?.role === "admin";

/** The notifications list and unread count, checked every minute. */
export const useNotifications = () => useQuery({ queryKey: keys.notifications, queryFn: () => api.alerts.list({ limit: 100 }), refetchInterval: 60_000 });

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
