import { useQuery } from "@tanstack/react-query";
import { api, type Grouping, type Line, type ResultsFilter, type Run, type Scope } from "./api";

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
  status: ["status"] as const,
  settings: ["settings"] as const,
  brands: ["brands"] as const,
  surfaces: ["surfaces"] as const,
  models: (surface: string, route: string, baseURL: string) => ["models", surface, route, baseURL] as const,
  questionSet: ["question-set"] as const,
  questionSets: ["question-sets"] as const,
  credentials: ["credentials"] as const,
  onboarding: ["onboarding"] as const,
  estimate: ["estimate"] as const,
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
  gaps: (run: number, f: ResultsFilter = {}) => ["results", "gaps", run, f.surface, f.model] as const,
  prompts: (f: ResultsFilter) => ["results", "prompts", f] as const,
  sources: (f: ResultsFilter) => ["results", "sources", f] as const,
  answers: (run: number) => ["results", "answers", run] as const,
  engine: (l: Line, since?: string) => ["results", "engine", l, since] as const,
  /** Under results, so a finished check refreshes the alert count too. */
  alerts: ["results", "alerts"] as const,
  channels: ["channels"] as const,
  deliveries: ["channels", "deliveries"] as const,
};

export const useMe = () => useQuery({ queryKey: keys.me, queryFn: api.me.get, staleTime: Infinity });

export const useAuthOptions = () => useQuery({ queryKey: keys.authOptions, queryFn: api.auth.options, staleTime: 60_000 });

export const useAccount = () => useQuery({ queryKey: keys.account, queryFn: api.account.get });

export const useStatus = () => useQuery({ queryKey: keys.status, queryFn: api.status, staleTime: 60_000 });
export const useSettings = () => useQuery({ queryKey: keys.settings, queryFn: api.settings.get });
export const useBrands = () => useQuery({ queryKey: keys.brands, queryFn: api.brands.list, staleTime: 60_000 });
export const useSurfaces = () => useQuery({ queryKey: keys.surfaces, queryFn: api.surfaces.list, staleTime: 60_000 });
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

/** The alerts list and unread count, checked every minute. */
export const useAlerts = () => useQuery({ queryKey: keys.alerts, queryFn: () => api.alerts.list({ limit: 100 }), refetchInterval: 60_000 });

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
