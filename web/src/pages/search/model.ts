import { useQuery } from "@tanstack/react-query";
import { api, type Day, type GoalMetric, type GoalWindow, type GscStatus, type SearchDay, type SearchPeriod } from "../../api";
import { dataVersion, keys } from "../../queries";

/** What the Search page can show, read from the connection status. Stored data shows even after a disconnect. */
export type SearchState = "ready" | "disconnected" | "no_property" | "failed" | "syncing";

export function searchState(s: GscStatus): SearchState {
  if (s.first_day) return "ready";
  if (!s.connected) return "disconnected";
  if (!s.property) return "no_property";
  if (s.last_sync_error && !s.syncing) return "failed";
  return "syncing";
}

const dayMs = 86_400_000;
const parse = (d: Day) => Date.parse(`${d}T00:00:00Z`);
export const addDays = (d: Day, n: number): Day => new Date(parse(d) + n * dayMs).toISOString().slice(0, 10);
const maxDay = (a: Day, b: Day) => (a > b ? a : b);

/** The newest stored day, partial or not. */
export const lastDay = (s: GscStatus): Day => s.partial_days[s.partial_days.length - 1] ?? s.latest_final_day;

/** The first day of a span of days ending on the newest stored day, never before the first stored one. */
export const spanFrom = (s: GscStatus, days: number): Day => maxDay(s.first_day, addDays(lastDay(s), 1 - days));

/** Chart spans on the Search page. The API serves at most 500 days a read. */
export const spans = [
  { id: "28", label: "28 days", days: 28 },
  { id: "90", label: "3 months", days: 90 },
  { id: "365", label: "12 months", days: 365 },
  { id: "all", label: "All", days: 480 },
] as const;

/** Every day from the first to the last, with days Google sent no row for as zero: no row means no impressions. */
export function fillDays(days: SearchDay[], from: Day, latestFinal: Day): SearchDay[] {
  if (days.length === 0) return [];
  const byDate = new Map(days.map((d) => [d.date, d]));
  const out: SearchDay[] = [];
  const end = days[days.length - 1].date;
  for (let d = from < days[0].date ? from : days[0].date; d <= end; d = addDays(d, 1)) {
    out.push(byDate.get(d) ?? { date: d, clicks: 0, impressions: 0, ctr: 0, position: 0, final: d <= latestFinal });
  }
  return out;
}

const dayFmt = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", timeZone: "UTC" });
const longDayFmt = new Intl.DateTimeFormat(undefined, { weekday: "short", month: "short", day: "numeric", year: "numeric", timeZone: "UTC" });
const fullDayFmt = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" });
/** "Nov 18". */
export const day = (d: Day) => dayFmt.format(parse(d));
/** "Wed, Nov 18, 2026". */
export const longDay = (d: Day) => longDayFmt.format(parse(d));
/** "Nov 18, 2026". */
export const fullDay = (d: Day) => fullDayFmt.format(parse(d));
/** "Oct 22 to Nov 18". */
export const dayRange = (from: Day, to: Day) => `${day(from)} to ${day(to)}`;
/** "Nov 19 and Nov 20", "Nov 18, Nov 19 and Nov 20". */
export const dayList = (ds: Day[]) => (ds.length < 2 ? ds.map(day).join("") : `${ds.slice(0, -1).map(day).join(", ")} and ${day(ds[ds.length - 1])}`);

const countFmt = new Intl.NumberFormat();
const compactFmt = new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 });
const ctrFmt = new Intl.NumberFormat(undefined, { style: "percent", maximumFractionDigits: 2 });
const positionFmt = new Intl.NumberFormat(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
export const count = (v: number) => countFmt.format(v);
export const compact = (v: number) => compactFmt.format(v);
export const ctr = (v: number) => ctrFmt.format(v);
export const position = (v: number) => (v > 0 ? positionFmt.format(v) : "–");

export const metricInfo: Record<GoalMetric, string> = {
  clicks: "Clicks",
  impressions: "Impressions",
  non_brand_clicks: "Non-brand clicks",
};
export const windowInfo: Record<GoalWindow, string> = { 28: "28 days", 90: "3 months" };

/**
 * The latest-28-day headline. change says how a move against the previous
 * 28 days reads: relative for counts, in points for CTR, and as a plain
 * difference for position, where lower is better.
 */
export const headline: { key: string; label: string; value: (p: SearchPeriod) => number; format: (v: number) => string; change: "relative" | "points" | "difference"; lowerIsBetter?: boolean }[] = [
  { key: "clicks", label: "Clicks", value: (p) => p.clicks, format: count, change: "relative" },
  { key: "impressions", label: "Impressions", value: (p) => p.impressions, format: count, change: "relative" },
  { key: "ctr", label: "CTR", value: (p) => p.ctr, format: ctr, change: "points" },
  { key: "position", label: "Average position", value: (p) => p.position, format: position, change: "difference", lowerIsBetter: true },
  { key: "non_brand", label: "Non-brand clicks", value: (p) => p.non_brand_clicks, format: count, change: "relative" },
];

/** A move against the previous window, or null when there is nothing to compare. */
export function change(now: number, before: number | undefined, kind: "relative" | "points" | "difference", lowerIsBetter = false): { text: string; good: boolean; flat: boolean } | null {
  if (before == null) return null;
  let d: number;
  let text: string;
  if (kind === "relative") {
    if (before === 0) return null;
    d = (now - before) / before;
    text = `${Math.abs(d * 100) < 10 ? Math.abs(d * 100).toFixed(1) : Math.round(Math.abs(d * 100))}%`;
  } else if (kind === "points") {
    d = (now - before) * 100;
    text = `${Math.abs(d).toFixed(2)} pts`;
  } else {
    if (now === 0 || before === 0) return null;
    d = now - before;
    text = Math.abs(d).toFixed(2);
  }
  const flat = Math.abs(d) < 0.0005;
  return { text, good: lowerIsBetter ? d < 0 : d > 0, flat };
}

export const useSearchSummary = (s: GscStatus | undefined) =>
  useQuery({ queryKey: keys.searchSummary(s ? dataVersion(s) : ""), queryFn: api.search.summary, enabled: !!s?.first_day });

export const useSearchDaily = (s: GscStatus | undefined, from: Day | undefined) =>
  useQuery({ queryKey: keys.searchDaily(from, s ? dataVersion(s) : ""), queryFn: () => api.search.daily(from), enabled: !!s?.first_day, placeholderData: (p) => p });

export const useSearchBrand = (s: GscStatus | undefined, from: Day | undefined) =>
  useQuery({ queryKey: keys.searchBrand(from, s ? dataVersion(s) : ""), queryFn: () => api.search.brand(from), enabled: !!s?.first_day, placeholderData: (p) => p });
