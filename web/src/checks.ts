import type { Costs, SettingsView } from "./api";

type NextCheck = NonNullable<SettingsView["next_check"]>;

const weekday = new Intl.DateTimeFormat(undefined, { weekday: "long" });
const monthDay = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });

/** "tomorrow at 06:00", "on Wednesday at 06:00" or "on Nov 30 at 06:00", in the instance's own time zone. */
export function nextCheckDay(n: NextCheck): string {
  const date = new Date(`${n.local_date}T12:00:00`);
  const day = n.days_away === 0 ? "today" : n.days_away === 1 ? "tomorrow" : n.days_away < 7 ? `on ${weekday.format(date)}` : `on ${monthDay.format(date)}`;
  return `${day} at ${n.local_time}`;
}

/** "tomorrow at 06:00, then every 3 days". */
export function nextCheckPhrase(n: NextCheck): string {
  return `${nextCheckDay(n)}, then ${n.then_every_days === 1 ? "every day" : `every ${n.then_every_days} days`}`;
}

/** When the next check runs, in a sentence that fits after "Next check". */
export function scheduleLine(s: SettingsView | undefined): string {
  if (!s) return "";
  if (s.checks_paused) return "Scheduled checks are paused.";
  if (!s.next_check) return "No check is scheduled yet.";
  return `Next check ${nextCheckPhrase(s.next_check)}.`;
}

const cents = new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", minimumFractionDigits: 2, maximumFractionDigits: 2 });

/** A sum of money a person reads at a glance: whole cents, and "under $0.01" for less. Per-answer prices keep usd's precision. */
export const money = (v: number) => (v > 0 && v < 0.005 ? `under ${cents.format(0.01)}` : cents.format(v));

/** One check's price in words: "about $0.38", with what it leaves out when some AI apps have no price yet. */
export function checkPrice(c: Costs): { amount: string; unknown: string | null } {
  const e = c.checks.check;
  return {
    amount: `about ${money(e.known_usd)}`,
    unknown: e.unknown_pairs > 0 ? `${e.unknown_pairs === 1 ? "One AI app has" : `${e.unknown_pairs} AI apps have`} no price yet, so it may cost more.` : null,
  };
}

/** Distinct AI apps in a check, since an app that runs several models counts once. */
export const appCount = (c: Costs) => new Set(c.checks.check.rows.map((r) => r.surface)).size;
