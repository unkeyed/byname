import { useSearchParams } from "react-router";
import type { Grouping, ResultsFilter, Run } from "./api";
import { useAnsweredRuns } from "./queries";

/** Date range presets. days counts back from today; latest is the newest run alone. */
export const ranges = [
  { id: "latest", label: "Latest run" },
  { id: "7d", label: "7 days", days: 7 },
  { id: "14d", label: "14 days", days: 14 },
  { id: "30d", label: "30 days", days: 30 },
  { id: "90d", label: "90 days", days: 90 },
  { id: "all", label: "All time" },
] as const;
export type RangeID = (typeof ranges)[number]["id"];

/**
 * Every piece of view state that lives in the URL, with its default. A
 * param at its default is left out, so a plain link opens the default view.
 * Filters (range, surface, by, model) follow you between pages.
 */
const params = {
  range: "30d",
  surface: "",
  /** "model" splits each surface into a line per model; otherwise a surface's models are combined. */
  by: "",
  model: "",
  metric: "mention",
  /** The question drawer, the answer it shows, and "raw" for the answer's text over the analysis. */
  q: "",
  qa: "",
  qv: "",
  /** The engine drawer's line, as engineParam writes it. */
  eng: "",
  /** A page's tab, and "outreach" to list only outreach targets among cited pages. */
  tab: "",
  only: "",
  search: "",
  group: "",
  result: "",
  sort: "",
  dir: "",
  before: "",
  after: "",
  show: "",
  /** The Search page's chart span, in days or "all". */
  span: "90",
} as const;
export type ViewParam = keyof typeof params;
export const filterParams: ViewParam[] = ["range", "surface", "by", "model"];

export function useView() {
  const [search, setSearch] = useSearchParams();
  const get = (k: ViewParam): string => search.get(k) ?? params[k];
  /** Filters replace the history entry; opening a drawer pushes one, so Back closes it. */
  const set = (patch: Partial<Record<ViewParam, string | number | null>>, opts: { push?: boolean } = {}) =>
    setSearch(
      (prev) => {
        const next = new URLSearchParams(prev);
        for (const [k, v] of Object.entries(patch) as [ViewParam, string | number | null][]) {
          const s = v == null ? "" : String(v);
          if (s === "" || s === params[k]) next.delete(k);
          else next.set(k, s);
        }
        return next;
      },
      { replace: !opts.push, preventScrollReset: true },
    );
  /** The query string with only the filters, for links to other pages. */
  const filterSearch = () => {
    const q = new URLSearchParams();
    for (const k of filterParams) if (search.has(k)) q.set(k, search.get(k)!);
    const s = q.toString();
    return s ? `?${s}` : "";
  };
  return { get, set, filterSearch };
}

const dayMs = 86_400_000;

/** A date as YYYY-MM-DD, so the query key holds still all day. */
const isoDay = (t: number) => new Date(t).toISOString().slice(0, 10);

export const runTime = (r: Run) => r.started_at ?? r.queued_at;

/**
 * The results filter the header's controls describe. ready is false until
 * the latest run is known, which the Latest run preset needs.
 */
export function useFilter(): { filter: ResultsFilter; by: Grouping; ready: boolean; range: RangeID } {
  const view = useView();
  const runs = useAnsweredRuns();
  const range = (ranges.find((r) => r.id === view.get("range"))?.id ?? "30d") as RangeID;
  const preset = ranges.find((r) => r.id === range)!;
  let since: string | undefined;
  let ready = true;
  if (range === "latest") {
    const latest = runs.data?.[0];
    since = latest ? runTime(latest) : undefined;
    ready = !runs.isPending;
  } else if ("days" in preset) {
    since = isoDay(Date.now() - preset.days * dayMs);
  }
  const model = view.get("model");
  const filter: ResultsFilter = { since, surface: view.get("surface") || undefined, model: model || undefined };
  return { filter, by: view.get("by") === "model" || model ? "pair" : "surface", ready, range };
}
