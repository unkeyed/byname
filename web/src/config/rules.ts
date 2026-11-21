// The setup rules the server enforces (internal/config), checked in the
// browser first so a mistake shows beside its field. The server's error
// still shows when the two disagree.

import type { Settings, SettingsView } from "../api";

const hostnamePattern = /^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/;

export type HostParse = { host: string; trimmed: boolean } | { error: string };

/** A pasted URL becomes its host; trimmed says something was cut so the form can say so. */
export function parseHost(raw: string): HostParse {
  const typed = raw.trim().toLowerCase();
  const host = typed
    .replace(/^[a-z][a-z0-9+.-]*:\/\//, "")
    .replace(/[/?#].*$/, "")
    .replace(/:\d+$/, "")
    .replace(/\.$/, "")
    .replace(/^www\./, "");
  if (!hostnamePattern.test(host)) return { error: `"${raw.trim()}" is not a hostname. Give the host only, such as northwind.com.` };
  return { host, trimmed: host !== typed };
}

/** Comma-separated names, trimmed, blank and repeated ones dropped, order kept. */
export function splitList(raw: string): string[] {
  const out: string[] = [];
  for (const part of raw.split(",")) {
    const w = part.trim().replace(/\s+/g, " ");
    if (w && !out.includes(w)) out.push(w);
  }
  return out;
}

export type HostsParse = { hosts: string[]; trimmed: string[] } | { error: string };

/** Hostnames from a comma-separated list, deduplicated, with the ones that lost a scheme or path. */
export function parseHosts(raw: string): HostsParse {
  const hosts: string[] = [];
  const trimmed: string[] = [];
  for (const part of splitList(raw)) {
    const p = parseHost(part);
    if ("error" in p) return p;
    if (p.trimmed) trimmed.push(p.host);
    if (!hosts.includes(p.host)) hosts.push(p.host);
  }
  return { hosts, trimmed };
}

/**
 * Everyday English words that brands borrow. An alias like this matches
 * sentences that are not about you, so the form warns before it is saved.
 */
const commonWords = new Set(
  `able access air apple arc atlas base beacon bee bird blue bolt box bridge bright build buzz cast cell chain chat check clear click cloud code
  core craft cube dash data deck delta dot drive drop echo edge element engine fast field fire flash flow fly focus force forge form frame
  fresh fuel fusion gate gem glass go grid ground guard harbor hive home hub ice ink iron jet key kit lab labs land launch layer leaf level
  light line link lion live loop magnet map mark matter mesh mind mint mode motion nest net next node north nova oak ocean one open orbit
  origin page path peak pilot pine pixel place plan planet play plus point pulse quest rain range ray reach ready relay ridge ring rise river
  rock root route run sage scale scout seed shape shield shift signal simple sky slate snap solid space spark sphere spring square stack star
  station stone storm stream summit sun swift sync table task terra thread tide time token tower track trail tree true unit vault vector
  view vision volt wave way well wind wing wire wise works yard zen zero zone`.split(/\s+/),
);

/** Aliases that are everyday words, as typed. */
export const commonWordAliases = (aliases: string[]) => aliases.filter((a) => commonWords.has(a.toLowerCase()));

/** A question's identity for deduplication and carrying over, the server's rule: lowercase letters and digits only. */
export const normalizeQuestion = (text: string) => text.toLowerCase().replace(/[^\p{L}\p{N}]/gu, "");

const escapeRegExp = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/** The names in text, whole word and ignoring case. */
export function namesIn(text: string, names: string[]): string[] {
  return names.filter((n) => n.trim() && new RegExp(`(?<![\\p{L}\\p{N}])${escapeRegExp(n.trim())}(?![\\p{L}\\p{N}])`, "iu").test(text));
}

export const questionLength = { min: 10, max: 300 };
export const groupMax = 80;

export function questionError(group: string, text: string): string | null {
  const t = text.trim().replace(/\s+/g, " ");
  const g = group.trim();
  if (!g) return "Give it a group. A question can be a group of one.";
  if (g.length > groupMax) return `Groups are at most ${groupMax} characters.`;
  if (t.length < questionLength.min) return `Questions are at least ${questionLength.min} characters.`;
  if (t.length > questionLength.max) return `Questions are at most ${questionLength.max} characters (this one is ${t.length}).`;
  return null;
}

/** RFC 4180 rows: quoted fields may hold commas, newlines and doubled quotes. */
export function parseCSV(input: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let quoted = false;
  for (let i = 0; i < input.length; i++) {
    const c = input[i];
    if (quoted) {
      if (c === '"' && input[i + 1] === '"') {
        field += '"';
        i++;
      } else if (c === '"') quoted = false;
      else field += c;
    } else if (c === '"' && field === "") quoted = true;
    else if (c === ",") {
      row.push(field);
      field = "";
    } else if (c === "\n" || c === "\r") {
      if (c === "\r" && input[i + 1] === "\n") i++;
      row.push(field);
      rows.push(row);
      row = [];
      field = "";
    } else field += c;
  }
  if (field !== "" || row.length) {
    row.push(field);
    rows.push(row);
  }
  return rows.filter((r) => r.some((f) => f.trim()));
}

export interface DraftQuestion {
  group: string;
  text: string;
}

/**
 * Questions from CSV with group and text columns, the header optional. A
 * row with one column is a question in the fallback group.
 */
export function questionsFromCSV(input: string, fallbackGroup: string): DraftQuestion[] {
  const rows = parseCSV(input);
  const head = rows[0]?.map((f) => f.trim().toLowerCase());
  let groupCol = 0;
  let textCol = 1;
  if (head && head.includes("text")) {
    textCol = head.indexOf("text");
    groupCol = head.indexOf("group");
    rows.shift();
  }
  return rows.map((r) =>
    r.length === 1 ? { group: fallbackGroup, text: r[0].trim() } : { group: (groupCol >= 0 ? r[groupCol] : fallbackGroup)?.trim() || fallbackGroup, text: r[textCol]?.trim() ?? "" },
  );
}

export const questionsFromLines = (input: string, group: string): DraftQuestion[] =>
  input
    .split(/\r?\n/)
    .map((l) => l.replace(/^\s*(?:[-*•]|\d+[.)])\s+/, "").trim())
    .filter(Boolean)
    .map((text) => ({ group, text }));

/** Splits incoming questions into those to add and those that repeat one already there or earlier in the batch. */
export function dedupe<T extends DraftQuestion>(existing: DraftQuestion[], incoming: T[]): { added: T[]; dropped: T[] } {
  const seen = new Set(existing.map((q) => normalizeQuestion(q.text)));
  const added: T[] = [];
  const dropped: T[] = [];
  for (const q of incoming) {
    const key = normalizeQuestion(q.text);
    if (!key || seen.has(key)) dropped.push(q);
    else {
      seen.add(key);
      added.push(q);
    }
  }
  return { added, dropped };
}

/** The numeric settings, each with the server's bounds. optional fields may be left empty, which saves null. */
export const numberFields = {
  check_every_days: { label: "Run AI checks every", unit: "days", min: 1, max: 30, int: true },
  spend_cap_run_usd: { label: "Spend cap per check", unit: "USD", above: 0 },
  spend_cap_month_usd: { label: "Monthly spend cap", unit: "USD", above: 0, optional: true },
  concurrency: { label: "Concurrency", unit: "calls at once", min: 1, max: 32, int: true },
  retries: { label: "Retries", unit: "per call", min: 0, max: 3, int: true },
  timeout_s: { label: "Timeout", unit: "seconds per call", min: 30, max: 600, int: true },
  raw_retention_days: { label: "Keep raw responses for", unit: "days", min: 1, int: true, optional: true },
  interim_days: { label: "Interim read", unit: "days after the first crawl", min: 7, max: 90, int: true },
  verdict_days: { label: "Final read", unit: "days after the first crawl", min: 14, max: 180, int: true },
  position: { label: "Position", unit: "places", above: 0, max: 50 },
  ctr_points: { label: "CTR", unit: "percentage points", above: 0, max: 50 },
  impressions_ratio: { label: "Impressions", unit: "times, against the controls", above: 1, max: 100 },
} satisfies Record<string, NumberRule>;

interface NumberRule {
  label: string;
  unit: string;
  min?: number;
  max?: number;
  above?: number;
  int?: boolean;
  optional?: boolean;
}

export type NumberKey = keyof typeof numberFields;

export function parseNumber(key: NumberKey, raw: string): { value: number | null } | { error: string } {
  const r: NumberRule = numberFields[key];
  const s = raw.trim();
  if (s === "") return r.optional ? { value: null } : { error: "Required." };
  const n = Number(s);
  const range =
    r.above !== undefined
      ? `more than ${r.above}${r.max !== undefined ? ` and at most ${r.max}` : ""}`
      : r.max !== undefined
        ? `${r.min} to ${r.max}`
        : `at least ${r.min}`;
  if (!Number.isFinite(n) || (r.int && !Number.isInteger(n))) return { error: `Must be ${r.int ? "a whole number, " : "a number, "}${range}.` };
  if ((r.above !== undefined && !(n > r.above)) || (r.min !== undefined && n < r.min) || (r.max !== undefined && n > r.max)) return { error: `Must be ${range}.` };
  return { value: n };
}

export const formatNumber = (v: number | null) => (v == null ? "" : String(v));

/** The editable part of the settings, for a PUT that changes only some fields. */
export function editable(v: SettingsView): Settings {
  return {
    brand: v.brand,
    match_mode: v.match_mode,
    market: v.market,
    language: v.language,
    time_zone: v.time_zone,
    check_every_days: v.check_every_days,
    check_time: v.check_time,
    spend_cap_run_usd: v.spend_cap_run_usd,
    spend_cap_month_usd: v.spend_cap_month_usd,
    concurrency: v.concurrency,
    retries: v.retries,
    timeout_s: v.timeout_s,
    raw_retention_days: v.raw_retention_days,
  };
}

export const browserTimeZone = () => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";

export function timeZones(current: string): string[] {
  const all = typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : [];
  return [...new Set([current, "UTC", ...all])].filter(Boolean).sort((a, b) => (a === "UTC" ? -1 : b === "UTC" ? 1 : a.localeCompare(b)));
}

const regionNames = new Intl.DisplayNames(undefined, { type: "region" });
const languageNames = new Intl.DisplayNames(undefined, { type: "language" });

const marketCodes = `US GB CA AU NZ IE IN SG ZA DE AT CH FR BE NL LU ES PT IT SE NO DK FI PL CZ HU RO GR TR IL AE SA BR MX AR CL CO JP KR CN TW HK ID MY PH TH VN`.split(" ");
const languageCodes = `en de fr es pt it nl sv no da fi pl cs hu ro el tr he ar hi ja ko zh id ms th vi uk ru`.split(" ");

const labelled = (codes: string[], current: string, name: (c: string) => string | undefined) =>
  [...new Set([current, ...codes])]
    .filter(Boolean)
    .map((code) => ({ code, label: `${name(code) ?? code} (${code})` }))
    .sort((a, b) => a.label.localeCompare(b.label));

export const markets = (current: string) => labelled(marketCodes, current, (c) => regionNames.of(c));
export const languages = (current: string) => labelled(languageCodes, current, (c) => languageNames.of(c));
