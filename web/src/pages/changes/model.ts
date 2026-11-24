import { useMemo } from "react";
import type { Change, ChangeDeltas, ChangeKind, ChangeMetric, ChangeOutcome, ChangeStage, ChangeStepKind, ChangeThresholds, RepoStatus } from "../../api";
import type { IconName, Tone } from "../../ui";
import { useSettings } from "../../queries";
import { figureInfo, fullDay } from "../search/model";

export const kindInfo: Record<ChangeKind, string> = { new_page: "New page", edit: "Edit", removed: "Removed" };
export const changeKinds = Object.keys(kindInfo) as ChangeKind[];

export const outcomeInfo: Record<ChangeOutcome, { label: string; tone: Tone; detail: string }> = {
  improved: { label: "Helped", tone: "success", detail: "Appearances, click rate or position moved for the better by more than its margin, compared with the comparison pages, and none moved for the worse." },
  worsened: { label: "Hurt", tone: "danger", detail: "Appearances, click rate or position moved for the worse by more than its margin, compared with the comparison pages, and none moved for the better." },
  no_change: { label: "No clear change", tone: "neutral", detail: "Nothing moved by more than its margin compared with the comparison pages, or the numbers pointed different ways." },
  insufficient_data: { label: "Not enough data", tone: "warn", detail: "Byname couldn't compare the page with the comparison pages." },
};

/** Before the final result, the stage is what a change shows; after it, the result. */
export const stageInfo: Record<ChangeStage, { label: string; tone: Tone; detail: (c: Change) => string }> = {
  awaiting_crawl: {
    label: "Waiting for Google",
    tone: "outline",
    detail: () => "Google hasn't looked at the page since the change. Byname asks Google about it every day.",
  },
  awaiting_interim: { label: "First result due", tone: "info", detail: (c) => `Google saw the change. The first result is due ${c.interim_on ? fullDay(c.interim_on) : "soon"}.` },
  awaiting_verdict: { label: "Final result due", tone: "info", detail: (c) => `The first result is in. The final result is due ${c.verdict_on ? fullDay(c.verdict_on) : "soon"}.` },
  decided: { label: "Done", tone: "neutral", detail: () => "The final result is in." },
};

export const changeStatus = (c: Change) => {
  if (c.verdict) return { label: outcomeInfo[c.verdict].label, tone: outcomeInfo[c.verdict].tone, detail: outcomeInfo[c.verdict].detail };
  const s = stageInfo[c.stage];
  return { label: s.label, tone: s.tone, detail: s.detail(c) };
};

const signedFmt = new Intl.NumberFormat(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2, signDisplay: "exceptZero" });
const ratioFmt = new Intl.NumberFormat(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });

/** The three differences in differences, each read against its margin. */
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
    label: figureInfo("Google").position.label,
    value: (d) => d.position,
    format: (v) => `${signedFmt.format(v)} places`,
    threshold: (t) => `${t.position} places either way`,
    reading: "Negative is better: the page moved up compared with the comparison pages.",
  },
  {
    metric: "ctr",
    label: figureInfo("Google").ctr.label,
    value: (d) => d.ctr_points,
    format: (v) => `${signedFmt.format(v)} points`,
    threshold: (t) => `${t.ctr_points} points either way`,
    reading: "Percentage points, compared with the comparison pages.",
  },
  {
    metric: "impressions",
    label: figureInfo("Google").impressions.label,
    value: (d) => d.impressions_ratio,
    format: (v) => `×${ratioFmt.format(v)}`,
    threshold: (t) => `×${ratioFmt.format(t.impressions_ratio)} or ×${ratioFmt.format(1 / t.impressions_ratio)}`,
    reading: "The page's appearances after over before, divided by the comparison pages'. Above 1 means it gained on them.",
  },
];

export const stepInfo: Record<ChangeStepKind, { label: (c: Change) => string; icon: IconName }> = {
  merged: { label: (c) => (c.source === "manual" ? "Logged" : "Merged"), icon: "merge" },
  inspected: { label: () => "Asked Google about the page", icon: "search" },
  crawled: { label: () => "Google saw it", icon: "globe" },
  interim: { label: () => "First result", icon: "chart" },
  verdict: { label: () => "Final result", icon: "check" },
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

/** Times in the instance's time zone, which places first and final result days too. */
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
