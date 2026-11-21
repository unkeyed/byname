import type { Fidelity, PairRef, Series, Surface } from "./api";

export const fidelityInfo: Record<Fidelity, { label: string; detail: string }> = {
  app: { label: "App answer", detail: "What a person sees in the app, read by a scraping provider." },
  model_match: { label: "Same model and search", detail: "An API call with the model the tool uses and the same web search." },
  approximation: { label: "Approximation", detail: "The closest stand-in available, not what the surface itself shows." },
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

export function usd(v: number): string {
  return new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: v !== 0 && Math.abs(v) < 1 ? 4 : 2 }).format(v);
}

const dayFmt = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
export const shortDay = (iso: string) => dayFmt.format(new Date(iso));
