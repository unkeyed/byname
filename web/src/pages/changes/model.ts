import { useMemo } from "react";
import type { Change, ChangeDeltas, ChangeKind, ChangeMetric, ChangeOutcome, ChangeStage, ChangeStepKind, ChangeThresholds, RepoStatus } from "../../api";
import type { IconName, Tone } from "../../ui";
import { useSettings } from "../../queries";
import { fullDay } from "../search/model";

export const kindInfo: Record<ChangeKind, string> = { new_page: "New page", edit: "Edit", removed: "Removed" };
export const changeKinds = Object.keys(kindInfo) as ChangeKind[];

export const outcomeInfo: Record<ChangeOutcome, { label: string; tone: Tone; detail: string }> = {
  improved: { label: "Improved", tone: "success", detail: "At least one metric passed its threshold for the better against the control pages, and none for the worse." },
  worsened: { label: "Worsened", tone: "danger", detail: "At least one metric passed its threshold for the worse against the control pages, and none for the better." },
  no_change: { label: "No change", tone: "neutral", detail: "No metric passed its threshold against the control pages, or they pointed different ways." },
  insufficient_data: { label: "Not enough data", tone: "warn", detail: "Byname could not compare the page with the control pages." },
};

/** Before the final read, the stage is what a change shows; after it, the verdict. */
export const stageInfo: Record<ChangeStage, { label: string; tone: Tone; detail: (c: Change) => string }> = {
  awaiting_crawl: {
    label: "Waiting for Google",
    tone: "outline",
    detail: () => "Google has not crawled the page since the change. Byname asks Search Console's URL Inspection every day.",
  },
  awaiting_interim: { label: "Interim read due", tone: "info", detail: (c) => `Google has crawled the page. The interim read is due ${c.interim_on ? fullDay(c.interim_on) : "soon"}.` },
  awaiting_verdict: { label: "Verdict due", tone: "info", detail: (c) => `The interim read is in. The final read is due ${c.verdict_on ? fullDay(c.verdict_on) : "soon"}.` },
  decided: { label: "Decided", tone: "neutral", detail: () => "The final read is in." },
};

export const changeStatus = (c: Change) => {
  if (c.verdict) return { label: outcomeInfo[c.verdict].label, tone: outcomeInfo[c.verdict].tone, detail: outcomeInfo[c.verdict].detail };
  const s = stageInfo[c.stage];
  return { label: s.label, tone: s.tone, detail: s.detail(c) };
};

const signedFmt = new Intl.NumberFormat(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2, signDisplay: "exceptZero" });
const ratioFmt = new Intl.NumberFormat(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });

/** The three differences in differences, each read against its threshold. */
export const deltaMetrics: {
  metric: ChangeMetric;
  label: string;
  value: (d: ChangeDeltas) => number;
  format: (v: number) => string;
  threshold: (t: ChangeThresholds) => string;
  reading: string;
}[] = [
  {
    metric: "position",
    label: "Position",
    value: (d) => d.position,
    format: (v) => `${signedFmt.format(v)} places`,
    threshold: (t) => `${t.position} places either way`,
    reading: "Negative is better: the page moved up against the controls.",
  },
  {
    metric: "ctr",
    label: "CTR",
    value: (d) => d.ctr_points,
    format: (v) => `${signedFmt.format(v)} pts`,
    threshold: (t) => `${t.ctr_points} pts either way`,
    reading: "Percentage points, against the controls.",
  },
  {
    metric: "impressions",
    label: "Impressions",
    value: (d) => d.impressions_ratio,
    format: (v) => `×${ratioFmt.format(v)}`,
    threshold: (t) => `×${ratioFmt.format(t.impressions_ratio)} or ×${ratioFmt.format(1 / t.impressions_ratio)}`,
    reading: "The page's after over before, divided by the controls'. Above 1 means it gained on them.",
  },
];

export const stepInfo: Record<ChangeStepKind, { label: (c: Change) => string; icon: IconName }> = {
  merged: { label: (c) => (c.source === "manual" ? "Logged" : "Merged"), icon: "merge" },
  inspected: { label: () => "URL Inspection", icon: "search" },
  crawled: { label: () => "Google crawled it", icon: "globe" },
  interim: { label: () => "Interim read", icon: "chart" },
  verdict: { label: () => "Final read", icon: "check" },
};

/** Where changes come from, which decides what an empty list asks for. */
export type Feed = "none" | "disconnected" | "no_rules" | "watching";

export function feed(r: RepoStatus | undefined): Feed {
  if (!r?.repo) return "none";
  if (!r.connected) return "disconnected";
  if (r.rules.length === 0) return "no_rules";
  return "watching";
}

export const siteChangesPath = "/settings/site-changes";

/** A page's path and query, for a column that has no room for the host. */
export function pagePath(url: string): string {
  try {
    const u = new URL(url);
    return u.pathname + u.search || "/";
  } catch {
    return url;
  }
}

function validZone(tz: string): string {
  try {
    new Intl.DateTimeFormat(undefined, { timeZone: tz });
    return tz;
  } catch {
    return "UTC";
  }
}

/** Times in the instance's time zone, which places interim and verdict days too. */
export function useZone() {
  const settings = useSettings();
  const zone = validZone(settings.data?.time_zone || "UTC");
  return useMemo(() => {
    const at = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: "numeric", hour: "numeric", minute: "2-digit", timeZoneName: "short", timeZone: zone });
    const date = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: "numeric", timeZone: zone });
    return {
      zone,
      ready: !settings.isPending,
      /** "Nov 19, 2026, 3:04 PM EST". */
      at: (iso: string) => at.format(new Date(iso)),
      /** "Nov 19, 2026", the day it fell on in the zone. */
      date: (iso: string) => date.format(new Date(iso)),
    };
  }, [zone, settings.isPending]);
}

export type Zone = ReturnType<typeof useZone>;

/** Today in the instance's time zone, as YYYY-MM-DD. */
export function todayIn(zone: string): string {
  const parts = new Intl.DateTimeFormat("en-CA", { year: "numeric", month: "2-digit", day: "2-digit", timeZone: zone }).format(new Date());
  return parts.slice(0, 10);
}
