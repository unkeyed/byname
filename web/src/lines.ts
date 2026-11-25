import type { Fidelity, PairRef, Series, Surface } from "./api";

export const fidelityInfo: Record<Fidelity, { label: string; detail: string }> = {
  app: { label: "Real app answer", detail: "Exactly what a person sees in the app." },
  model_match: { label: "Same model, not the full tool", detail: "The model the tool uses, with the same web search, but without the tool's own instructions. Close to the tool, not the tool itself." },
  approximation: { label: "Stand-in (not the real app)", detail: "The closest model Byname can ask. The real app may answer differently, so it is charted apart." },
};

/** A line's identity: its pair, or its surface when the line combines a surface's models. */
export const lineKey = (s: Series) => `${s.pair.id ? `p${s.pair.id}` : s.pair.surface}|${s.endpoint}|${s.fidelity}`;

/** Colour follows the line's surface or pair, in setup order, so a filter never repaints the lines that remain. */
export function palette(surfaces: Surface[] | undefined) {
  const used = (surfaces ?? []).filter((s) => s.pairs.length > 0);
  const surfaceSlot = new Map(used.map((s, i) => [s.key, i]));
  const pairSlot = new Map(used.flatMap((s) => s.pairs).map((p, i) => [p.id, i]));
  return (pair: PairRef) => {
    const slot = pair.id ? pairSlot.get(pair.id) : surfaceSlot.get(pair.surface);
    // Eight hues tell lines apart reliably; past that a line goes grey and leans on its label.
    return slot !== undefined && slot < 8 ? `var(--series-${slot + 1})` : "var(--faint)";
  };
}

export function useLabels(surfaces: Surface[] | undefined) {
  const titles = new Map((surfaces ?? []).map((s) => [s.key, s.title]));
  const surfaceTitle = (key: string) => titles.get(key) ?? key;
  return {
    surfaceTitle,
    /** "Claude Code · claude-sonnet-4-5", "Claude Code · 2 models" for a combined line, or "ChatGPT app". */
    line: (pair: PairRef, models: string[] = []) => {
      const model = pair.model || (models.length === 1 ? models[0] : models.length > 1 ? `${models.length} models` : "");
      return model ? `${surfaceTitle(pair.surface)} · ${model}` : surfaceTitle(pair.surface);
    },
  };
}

const pctFmt = new Intl.NumberFormat(undefined, { style: "percent", maximumFractionDigits: 0 });
const pct1Fmt = new Intl.NumberFormat(undefined, { style: "percent", maximumFractionDigits: 1 });
export const pct = (v: number | null | undefined) => (v == null ? "–" : v > 0 && v < 0.1 ? pct1Fmt.format(v) : pctFmt.format(v));

/** How often chance alone gives a move as large, from its p-value, as notifications say it: "about 62 times in 100". */
export function chance(p: number): string {
  const n = Math.round(p * 100);
  if (p < 0.01) return "less than 1 time in 100";
  if (n === 1) return "about 1 time in 100";
  if (n >= 100) return "almost every time";
  return `about ${n} times in 100`;
}

const dayFmt = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
export const shortDay = (iso: string) => dayFmt.format(new Date(iso));

/** A page's address without its scheme, www. or trailing slash, such as northwind.example/blog/x. */
export const shortUrl = (u: string) => u.replace(/^https?:\/\/(www\.)?/, "").replace(/\/$/, "");

/** Names joined as a sentence does: "a", "a and b", "a, b and c". */
export const andList = (xs: string[]) => (xs.length <= 1 ? xs.join("") : `${xs.slice(0, -1).join(", ")} and ${xs[xs.length - 1]}`);
