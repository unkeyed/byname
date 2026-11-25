import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, type Change, type Competitors, type Costs, type Gap, type Grouping, type LeftOut, type OtherBrands, type QuestionSet, type ResultsFilter, type RunSummary, type SearchStatus, type Series, type SettingsView, type SocialSettings, type Sources } from "../../api";
import { BrandMark } from "../../brandmark";
import { LineChart, LineKey, Sparkline, type ChartLine } from "../../charts";
import { nextCheckDay, nextCheckPhrase } from "../../checks";
import { money } from "../../money";
import { fidelityInfo, lineKey, palette, pct, shortDay, useLabels } from "../../lines";
import { keys, useAnsweredRuns, useChanges, useCosts, useLeftOut, useMe, useOtherBrands, useQuestions, useRuns, useSearchProviders, useSettings, useSocialSettings, useSurfaces } from "../../queries";
import { Card, CardHeader, cx, EmptyState, ErrorNote, Icon, Pending, scrollArea, ScrollX, useLoading, type IconName } from "../../ui";
import { runTime, useFilter, useView, type RangeID } from "../../view";
import { DailyChart } from "../search/DailyChart";
import { day, figureInfo, readyEngines, spanFrom, useSearchDaily } from "../search/model";
import { settingsPath as searchEnginesSettings } from "../search/Search";
import { useBrandActions } from "./AlsoNamed";
import { useOpenEngine } from "./EngineDrawer";
import { Segmented, ViewHeader } from "./Header";
import { QuestionsTable } from "./QuestionsTable";
import { RunNowButton, useCanRun } from "./RunNow";

/**
 * This week: what the latest check found, whether it is normal, and what to
 * do next. Every number and every to-do is built from data Byname holds; a
 * to-do with nothing behind it does not show.
 */
export function ThisWeek() {
  const settings = useSettings();
  const runs = useRuns();
  const answered = useAnsweredRuns();
  const canRun = useCanRun();
  const brand = settings.data?.brand.name || "your brand";
  useLoading(settings.isPending);

  let body;
  if (runs.isPending) body = <Pending />;
  else if (runs.isError) body = <ErrorNote error={runs.error} />;
  else if (!answered.data?.length)
    body = (
      <EmptyState icon="calendar" title="No checks yet" action={canRun && runs.data.length === 0 ? <RunNowButton /> : undefined}>
        {runs.data.length > 0
          ? "A check is under way. Results appear here a few minutes after it starts."
          : canRun
            ? `Check now to ask every question in every AI app you chose and see how often ${brand} is named.`
            : "Nothing to show until someone with the Editor or Admin role starts a check."}
      </EmptyState>
    );
  else body = <Week />;

  return (
    <>
      <ViewHeader
        title="This week"
        range={false}
        scope={false}
        action={canRun && answered.data?.length ? <RunNowButton variant="secondary" /> : undefined}
      />
      <div className={cx("relative min-h-0 flex-1 px-3 py-4 md:px-5 md:py-5", scrollArea)}>
        <div className="mx-auto max-w-6xl">{body}</div>
      </div>
    </>
  );
}

/** At least this many earlier checks make a normal range, and at most this many count. */
const rangeMin = 3;
const rangeMax = 5;

type Band = { low: number; high: number; checks: number };

/** The spread of a metric over the checks before the latest, when there are enough of them to call it normal. */
function normalRange(earlier: (number | null)[]): Band | null {
  const xs = earlier.filter((v): v is number => v != null).slice(-rangeMax);
  if (xs.length < rangeMin) return null;
  return { low: Math.min(...xs), high: Math.max(...xs), checks: xs.length };
}

const range = (b: Band) => (Math.round(b.low * 100) === Math.round(b.high * 100) ? pct(b.low) : `${Math.round(b.low * 100)} to ${pct(b.high)}`);

/** Everything This week reads, loaded together so the page appears in one piece. */
type WeekData = {
  settings: SettingsView;
  summary: RunSummary[];
  gaps: Gap[];
  sources: Sources | undefined;
  latest: Competitors | undefined;
  lastTwo: Competitors | undefined;
  /** The brands the latest check named that you don't track. */
  others: OtherBrands | undefined;
  changes: Change[];
  social: SocialSettings | undefined;
  socialNew: number;
  leftOut: LeftOut[] | undefined;
  questions: QuestionSet | null | undefined;
  costs: Costs | undefined;
  /** The search engines whose numbers are in, each with its own card. */
  searchEngines: SearchStatus[];
  admin: boolean;
  canEdit: boolean;
};

function Week() {
  const me = useMe();
  const settings = useSettings();
  const answered = useAnsweredRuns();
  const { filter, by } = useFilter();
  const runs = answered.data!;
  const latestStart = runTime(runs[0]);
  const prevStart = runs[1] ? runTime(runs[1]) : undefined;
  const weekAgo = new Date(Date.now() - 7 * 86_400_000).toISOString();
  const summary = useQuery({ queryKey: keys.summary({}), queryFn: () => api.metrics.summary({}) });
  const gaps = useQuery({ queryKey: keys.gaps(0), queryFn: () => api.gaps() });
  const sources = useQuery({ queryKey: keys.sources(filter), queryFn: () => api.sources(filter) });
  const latest = useQuery({ queryKey: keys.competitors({ since: latestStart }, "surface"), queryFn: () => api.metrics.competitors({ since: latestStart }, "surface") });
  const lastTwo = useQuery({ queryKey: keys.competitors({ since: prevStart }, "surface"), queryFn: () => api.metrics.competitors({ since: prevStart }, "surface"), enabled: !!prevStart });
  const others = useOtherBrands({ since: latestStart });
  const changes = useChanges();
  const social = useSocialSettings();
  const socialNew = useQuery({ queryKey: keys.socialMentions({ since: weekAgo.slice(0, 10), limit: 1 }), queryFn: () => api.social.mentions({ since: weekAgo.slice(0, 10), limit: 1 }), enabled: !!social.data?.enabled });
  const leftOut = useLeftOut();
  const questions = useQuestions();
  const costs = useCosts();
  const engines = useSearchProviders();
  const pending = [summary, gaps, sources, latest, others, changes, social, leftOut, questions, costs, engines].some((q) => q.isPending) || (!!prevStart && lastTwo.isPending) || (!!social.data?.enabled && socialNew.isPending);
  useLoading(pending);
  if (pending || !settings.data) return null;
  if (summary.isError) return <ErrorNote error={summary.error} />;

  const d: WeekData = {
    settings: settings.data,
    summary: summary.data ?? [],
    gaps: gaps.data?.gaps ?? [],
    sources: sources.data,
    latest: latest.data,
    lastTwo: lastTwo.data,
    others: others.data,
    changes: changes.data ?? [],
    social: social.data,
    socialNew: socialNew.data?.total ?? 0,
    leftOut: leftOut.data,
    questions: questions.data,
    costs: costs.data,
    searchEngines: readyEngines(engines.data ?? []),
    admin: me.data?.role === "admin",
    canEdit: me.data?.scopes.includes("config:write") ?? false,
  };
  const todos = todoRules.flatMap((rule) => rule(d));
  const setup = setupRules.flatMap((rule) => rule(d));

  return (
    <div className="space-y-4">
      {runs.length === 1 && <FirstResults settings={d.settings} />}
      <Headline d={d} />
      <div className={cx("grid grid-cols-1 items-start gap-4", setup.length > 0 && "lg:grid-cols-2")}>
        <ItemList title="To do this week" description="Built from your latest check. Each item links to where you act on it." items={todos} empty="Nothing needs you this week. AI apps named you in every question, and nothing moved against you." />
        {setup.length > 0 && <ItemList title="Finish setting up" description="These go away once each is done." items={setup} />}
      </div>
      <Footer d={d} />
      <RatesCard filter={filter} by={by} />
      <div className={cx("grid grid-cols-1 gap-4", d.searchEngines.length > 0 && "lg:grid-cols-2")}>
        <ShareCard filter={filter} />
        {d.searchEngines.map((s) => (
          <SearchClicksCard key={s.provider} s={s} />
        ))}
      </div>
      <Card>
        <CardHeader title="Questions" description="Each question in each AI app in the latest check. Open one to see its history and read the answers." />
        <QuestionsTable run={runs[0]} filter={filter} />
      </Card>
    </div>
  );
}

function FirstResults({ settings: s }: { settings: SettingsView }) {
  return (
    <div className="flex items-start gap-3 rounded-surface border border-line bg-info-soft px-4 py-3 text-sm">
      <Icon name="sparkles" className="mt-0.5 size-4 text-info" />
      <p>
        <span className="font-semibold">Your first results are in.</span>{" "}
        {s.next_check ? `A second check runs ${nextCheckDay(s.next_check)} so you can see what moves.` : "Check again later to see what moves."} Until then, numbers have nothing to compare with, so
        nothing reads as up or down.
      </p>
    </div>
  );
}

/** One sentence for the latest check, then the three numbers with their normal ranges. */
function Headline({ d }: { d: WeekData }) {
  const all = d.summary;
  const cur = all[all.length - 1];
  if (!cur) return <Card className="px-4 py-3 text-sm text-muted">The latest check has no answers that count in scores yet.</Card>;
  const earlier = all.slice(0, -1);
  const brand = d.settings.brand.name || "you";
  const band = normalRange(earlier.map((r) => r.mention_rate));
  const lostTo = d.gaps.filter((g) => g.question.counts_in_scores && g.named_on_miss.length > 0).length;
  const domain = d.settings.brand.domains[0];
  return (
    <Card className="space-y-4 px-4 py-4">
      <p className="text-base text-balance">
        AI apps named {brand} in{" "}
        <span className="font-semibold tabular-nums">
          {cur.mentioned} of {cur.answered}
        </span>{" "}
        answers in the latest check ({pct(cur.mention_rate)}
        {cur.mention_band && `, likely ${pct(cur.mention_band.low)} to ${pct(cur.mention_band.high)}`}).
        {band && ` Your normal range over the ${band.checks} checks before it is ${range(band)}.`}
        {lostTo > 0 && ` A competitor was named instead of you on ${lostTo} ${lostTo === 1 ? "question" : "questions"}.`}
      </p>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <Score label="Named you" hint="How often an answer said your name." value={cur.mention_rate} detail={`${cur.mentioned} of ${cur.answered} answers`} history={all.map((r) => r.mention_rate)} />
        <Score
          label="Linked to your site"
          hint={`How often an answer linked to ${domain || "your site"}.`}
          value={cur.citation_rate}
          detail={`${cur.cited} of ${cur.answered} answers`}
          history={all.map((r) => r.citation_rate)}
        />
        <Score label="Share of mentions" hint="Of all the times AI named you or a competitor, how often it was you." value={cur.share} detail={`Of ${cur.mentions} mentions`} history={all.map((r) => r.share)} />
      </div>
    </Card>
  );
}

/**
 * A metric with its normal range. A swing inside the range of the checks
 * before is called normal, because answers drift from check to check with
 * nothing changed; only a move outside it reads as up or down.
 */
function Score({ label, hint, value, detail, history }: { label: string; hint: string; value: number | null; detail: string; history: (number | null)[] }) {
  const earlier = history.slice(0, -1);
  const prev = [...earlier].reverse().find((v) => v != null);
  const band = normalRange(earlier);
  let verdict: ReactNode;
  if (value == null) verdict = <span className="text-faint">No answers counted</span>;
  else if (prev == null) verdict = <span className="text-faint">First check</span>;
  else if (!band) {
    const moved = Math.round((value - prev) * 100);
    verdict = (
      <span className="text-muted">
        {moved === 0 ? "Same as the last check" : `${Math.abs(moved)} points ${moved > 0 ? "up" : "down"} on the last check`}. A normal range shows after {rangeMin + 1} checks.
      </span>
    );
  } else if (value < band.low)
    verdict = (
      <span className="inline-flex items-center gap-1 font-medium text-danger">
        <Icon name="down" className="size-3" />
        Below your normal range of {range(band)}
      </span>
    );
  else if (value > band.high)
    verdict = (
      <span className="inline-flex items-center gap-1 font-medium text-success">
        <Icon name="up" className="size-3" />
        Above your normal range of {range(band)}
      </span>
    );
  else verdict = <span className="text-muted">Normal: inside your range of {range(band)}</span>;
  return (
    <div className="flex flex-col gap-1 rounded-surface border border-line px-3 py-3">
      <div className="flex items-start justify-between gap-2">
        <h3 className="text-xs font-medium text-muted">{label}</h3>
        {history.filter((v) => v != null).length > 1 && <Sparkline values={history.slice(-12)} />}
      </div>
      <span className="text-2xl font-semibold tabular-nums">{pct(value)}</span>
      <span className="text-xs text-muted">{detail}</span>
      <RangeBar value={value} band={band} />
      <span className="min-h-8 text-xs">{verdict}</span>
      <span className="text-xs text-faint">{hint}</span>
    </div>
  );
}

/** 0 to 100% with the normal range shaded and the latest value marked. */
function RangeBar({ value, band }: { value: number | null; band: Band | null }) {
  return (
    <div className="relative my-1 h-1.5 rounded-full bg-subtle" aria-hidden="true">
      {band && <div className="absolute inset-y-0 rounded-full bg-subtle-strong" style={{ left: `${band.low * 100}%`, width: `${Math.max((band.high - band.low) * 100, 1)}%` }} />}
      {value != null && <div className="absolute top-1/2 size-2.5 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-surface bg-fg" style={{ left: `${value * 100}%` }} />}
    </div>
  );
}

/** A to-do. track is a brand you don't track, shown with its logo and a Track button. */
type Item = { key: string; icon: IconName; text: string; to: string; action: string; track?: { id: number; name: string } };

const quote = (s: string) => `“${s.length > 70 ? `${s.slice(0, 68).trimEnd()}…` : s}”`;
const names = (xs: string[]) => (xs.length <= 1 ? xs.join("") : `${xs.slice(0, -1).join(", ")} and ${xs[xs.length - 1]}`);

/** Each rule turns data into to-dos, or none when there is nothing behind it. Order is the order shown. */
const todoRules: ((d: WeekData) => Item[])[] = [
  (d) => {
    const never = d.gaps.filter((g) => g.named === 0 && g.question.counts_in_scores);
    const shown = never.slice(0, 3).map((g) => ({
      key: `never-${g.question.id}`,
      icon: "alert" as const,
      text: `No AI app named you for ${quote(g.question.text)}.${g.named_on_miss.length ? ` They named ${names(g.named_on_miss.slice(0, 3))}.` : ""}`,
      to: `/?q=${g.question.id}`,
      action: "Read the answers",
    }));
    if (never.length > 3) shown.push({ key: "never-more", icon: "alert", text: `${never.length - 3} more ${never.length === 4 ? "question" : "questions"} where no AI app named you.`, to: "/ai/missed", action: "See all missed questions" });
    return shown;
  },
  (d) => {
    const n = d.sources?.pages.filter((p) => p.outreach).length ?? 0;
    if (!n) return [];
    return [{ key: "pitch", icon: "mail", text: `${n} ${n === 1 ? "page AI links to names" : "pages AI links to name"} a competitor and never you. Ask them to include you.`, to: "/ai/missed?tab=sources&only=outreach", action: "See pages to pitch" }];
  },
  (d) => {
    if (!d.latest || !d.lastTwo) return [];
    const before = new Map(d.lastTwo.brands.map((b) => [b.brand_id, b.mentions]));
    const gains = d.latest.brands
      .filter((b) => b.kind === "competitor")
      .map((b) => ({ name: b.name, gain: b.mentions - ((before.get(b.brand_id) ?? 0) - b.mentions) }))
      .filter((b) => b.gain >= 2)
      .sort((a, b) => b.gain - a.gain)
      .slice(0, 2);
    return gains.map((g) => ({ key: `gain-${g.name}`, icon: "users" as const, text: `${g.name} was named in ${g.gain} more answers than in the check before.`, to: "/ai/competitors", action: "See competitors" }));
  },
  (d) => {
    // Named in at least 3 answers and a fifth of the check: often enough that you likely compete with it.
    const o = d.others;
    if (!o) return [];
    const often = Math.max(3, Math.ceil(o.answered / 5));
    return o.brands
      .filter((b) => b.latest >= often)
      .slice(0, 2)
      .map((b) => ({
        key: `other-${b.brand_id}`,
        icon: "users" as const,
        text: `${b.name} was named in ${b.latest} ${b.latest === 1 ? "answer" : "answers"} and you don't track it.`,
        to: "/ai/competitors#also-named",
        action: "See other brands",
        track: { id: b.brand_id, name: b.name },
      }));
  },
  (d) => {
    const today = new Date().toISOString().slice(0, 10);
    const soon = new Date(Date.now() + 7 * 86_400_000).toISOString().slice(0, 10);
    const weekAgo = new Date(Date.now() - 7 * 86_400_000).toISOString().slice(0, 10);
    const page = (c: Change) => c.url.replace(/^https?:\/\/[^/]+/, "") || "/";
    return d.changes.flatMap((c): Item[] => {
      const to = `/search/site-changes?chg=${c.id}`;
      if (c.final && c.final.read_on >= weekAgo) return [{ key: `chg-${c.id}`, icon: "pencil", text: `The final result for your change to ${page(c)} is in.`, to, action: "See the result" }];
      if (c.interim && c.interim.read_on >= weekAgo) return [{ key: `chg-${c.id}`, icon: "pencil", text: `The first result for your change to ${page(c)} is in.`, to, action: "See the result" }];
      if (!c.verdict && c.verdict_on && c.verdict_on >= today && c.verdict_on <= soon) return [{ key: `chg-${c.id}`, icon: "pencil", text: `The final result for your change to ${page(c)} is due ${shortDay(c.verdict_on)}.`, to, action: "See the change" }];
      return [];
    });
  },
  (d) =>
    d.social?.enabled && d.socialNew > 0
      ? [{ key: "social", icon: "message", text: `${d.socialNew} new ${d.socialNew === 1 ? "post" : "posts"} named you on social networks in the last 7 days.`, to: "/social", action: "Read them" }]
      : [],
];

/** Setup that makes the numbers better, shown until each is done. */
const setupRules: ((d: WeekData) => Item[])[] = [
  (d) =>
    d.searchEngines.length === 0 && d.admin
      ? [
          {
            key: "search",
            icon: "plug",
            text: "Connect a search engine. Google Search Console and Bing Webmaster Tools are free reports of how often your site shows up in their results and how many people click, to see next to your AI results.",
            to: searchEnginesSettings,
            action: "Connect one",
          },
        ]
      : [],
  (d) =>
    d.social?.enabled && d.leftOut?.length === 0 && d.canEdit
      ? [{ key: "left-out", icon: "users", text: "Your team's own posts count as mentions. Leave out your team and company accounts so you see what other people say.", to: "/settings/social#left-out", action: "Leave out accounts" }]
      : [],
  (d) =>
    d.settings.spend_cap_month_usd == null && d.canEdit
      ? [
          {
            key: "cap",
            icon: "key",
            text: `Add a monthly spend cap, so checks and social searches stop when they reach it.${d.costs ? ` You can expect ${money(d.costs.month_estimate_usd, "about")} a month.` : ""}`,
            to: "/settings/schedule",
            action: "Add a cap",
          },
        ]
      : [],
  (d) => {
    const brand = d.settings.brand.name || "your brand";
    const naming = d.questions?.questions.filter((q) => q.names_brand && !q.count_in_scores) ?? [];
    if (!naming.length || !d.canEdit) return [];
    const first = naming[0];
    return [
      {
        key: "naming",
        icon: "pencil",
        text: `${quote(first.text)}${naming.length > 1 ? ` and ${naming.length - 1} more` : ""} ${naming.length > 1 ? "name" : "names"} ${brand}, so AI names you because you asked, and ${naming.length > 1 ? "they are" : "it is"} left out of your scores. Rewrite ${naming.length > 1 ? "them" : "it"} the way a buyer who hasn't heard of you would ask.`,
        to: "/settings/questions",
        action: "Edit questions",
      },
    ];
  },
];

function ItemList({ title, description, items, empty }: { title: string; description: string; items: Item[]; empty?: string }) {
  const actions = useBrandActions();
  const canEdit = useMe().data?.scopes.includes("config:write") ?? false;
  return (
    <Card>
      <CardHeader title={title} description={description} />
      {items.length === 0 ? (
        <p className="border-t border-line px-4 py-4 text-sm text-muted">{empty}</p>
      ) : (
        <ul className="divide-y divide-line border-t border-line">
          {items.map((it) => (
            <li key={it.key} className="flex items-start gap-3 px-4 py-3">
              {it.track ? <BrandMark id={it.track.id} name={it.track.name} size={16} /> : <Icon name={it.icon} className="mt-0.5 size-4 text-muted" />}
              <div className="min-w-0 flex-1 text-sm">
                <p className="[overflow-wrap:anywhere]">{it.text}</p>
                <div className="flex flex-wrap items-center gap-x-4">
                  <Link to={it.to} className="inline-flex h-11 items-center gap-1 text-xs font-medium text-fg hover:underline md:h-auto md:pt-1">
                    {it.action}
                    <Icon name="right" className="size-3" />
                  </Link>
                  {it.track && canEdit && (
                    <button
                      type="button"
                      disabled={actions.track.isPending}
                      onClick={() => actions.track.mutate(it.track!.id)}
                      aria-label={`Track ${it.track.name} as a competitor`}
                      className="inline-flex h-11 items-center gap-1 text-xs font-medium text-fg hover:underline disabled:opacity-50 md:h-auto md:pt-1"
                    >
                      <Icon name="plus" className="size-3" />
                      Track
                    </button>
                  )}
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
      {items.some((it) => it.track) && (
        <div className="min-h-5 px-4 pb-2">
          <ErrorNote error={actions.track.error} />
        </div>
      )}
    </Card>
  );
}

/** When the next check runs, and this month's spend against the cap. */
function Footer({ d }: { d: WeekData }) {
  const s = d.settings;
  const m = d.costs?.month;
  return (
    <div className="flex flex-col gap-x-6 gap-y-1 rounded-surface border border-line bg-surface px-4 py-2.5 text-xs text-muted sm:flex-row sm:flex-wrap sm:items-center">
      <span>
        Next check{" "}
        <span className="font-medium text-fg">{s.checks_paused ? "paused" : s.next_check ? nextCheckPhrase(s.next_check) : "not scheduled yet"}</span>
      </span>
      {m && (
        <span>
          Spent <span className="font-medium text-fg tabular-nums">{money(m.spent_usd)}</span>
          {m.cap_usd != null ? ` of your ${money(m.cap_usd)} cap` : ""} this month
          {d.costs && `, ${money(d.costs.month_estimate_usd, "about")} expected`}
        </span>
      )}
      <Link to="/settings/schedule" className="inline-flex h-11 items-center gap-1 font-medium text-fg hover:underline sm:ml-auto md:h-auto">
        Schedule and spending <Icon name="right" className="size-3" />
      </Link>
    </div>
  );
}

/** Spans for the date range presets; the newest check alone maps to the 28-day headline window. */
const searchSpan: Record<RangeID, number> = { latest: 28, "7d": 7, "14d": 14, "30d": 30, "90d": 90, all: 480 };

/** One search engine's clicks per day over the page's range, for an engine whose numbers are in. */
function SearchClicksCard({ s }: { s: SearchStatus }) {
  const { range } = useFilter();
  const from = spanFrom(s, searchSpan[range]);
  const daily = useSearchDaily(s, from);
  const clicks = figureInfo(s.title).clicks.hint;
  let body;
  if (daily.isPending) body = <Pending />;
  else if (daily.isError) body = <div className="px-4 pb-4"><ErrorNote error={daily.error} /></div>;
  else
    body = (
      <div className={cx(daily.isPlaceholderData && "opacity-60")}>
        <DailyChart engine={s.title} days={daily.data.days} from={from} latestFinal={daily.data.latest_final_day} show={["clicks"]} height={200} />
      </div>
    );
  const partial = s.partial_days.length > 0 ? ` Days after ${day(daily.data?.latest_final_day ?? s.latest_final_day)} are still being counted by ${s.title}.` : "";
  return (
    <Card className="min-w-0">
      <CardHeader title={`${s.title} clicks`} description={`${clicks} Per day, a few days late.${partial}`}>
        <Link to={`/search/traffic?provider=${s.provider}`} className="inline-flex h-11 shrink-0 items-center gap-1 text-xs font-medium text-muted hover:text-fg md:h-auto">
          Traffic <Icon name="right" className="size-3" />
        </Link>
      </CardHeader>
      {body}
    </Card>
  );
}

function RatesCard({ filter, by }: { filter: ResultsFilter; by: Grouping }) {
  const view = useView();
  const settings = useSettings();
  const next = settings.data?.next_check;
  const openEngine = useOpenEngine();
  const surfaces = useSurfaces();
  const labels = useLabels(surfaces.data);
  const color = palette(surfaces.data);
  const metric = view.get("metric") === "citation" ? "citation" : "mention";
  const rates = useQuery({ queryKey: keys.rates(filter, by), queryFn: () => api.metrics.rates(filter, by) });

  let body;
  if (rates.isPending) body = <Pending />;
  else if (rates.isError) body = <ErrorNote error={rates.error} />;
  else {
    const runIDs = [...new Set(rates.data.flatMap((s) => s.points.map((p) => p.run_id)))].sort((a, b) => a - b);
    const at = new Map(rates.data.flatMap((s) => s.points.map((p) => [p.run_id, p.run_started_at] as const)));
    const xs = runIDs.map((id) => ({ key: String(id), label: `#${id}`, title: `Check #${id}, ${shortDay(at.get(id)!)}` }));
    const lines: (ChartLine & { fidelity: keyof typeof fidelityInfo; latest: number | null; models: string[]; series: Series })[] = rates.data.map((s) => {
      const byRun = new Map(s.points.map((p) => [p.run_id, metric === "mention" ? p.mention_rate : p.citation_rate]));
      const values = runIDs.map((id) => byRun.get(id) ?? null);
      return {
        key: lineKey(s),
        label: labels.line(s.pair, s.models),
        color: color(s.pair),
        approx: s.fidelity === "approximation",
        values,
        fidelity: s.fidelity,
        latest: values.reduce<number | null>((l, v) => (v == null ? l : v), null),
        models: s.models,
        series: s,
      };
    });
    body =
      lines.length === 0 ? (
        <p className="px-4 py-10 text-center text-sm text-muted">No answers in this range.</p>
      ) : (
        <div className={cx("grid grid-cols-[minmax(0,1fr)] gap-4 px-4 pb-4 lg:grid-cols-[minmax(0,1fr)_16rem]", rates.isPlaceholderData && "opacity-60")}>
          {xs.length === 1 && (
            <p className="order-last text-xs text-muted lg:col-span-2">
              One check so far, so each AI app is a single dot. Lines start after your second check{next ? `, ${nextCheckDay(next)}` : ""}.
            </p>
          )}
          <LineChart xs={xs} lines={lines} format={(v) => pct(v)} label={`${metric === "mention" ? "Named you" : "Linked to your site"} per check, one line per ${by === "surface" ? "AI app" : "AI app and model"}`} />
          <ul aria-label="AI apps, with each one's latest value. Open one for its detail." className={cx("-mx-1.5 space-y-0.5 self-start text-xs lg:max-h-60", scrollArea)}>
            {lines.map((l) => (
              <li key={l.key}>
                <button type="button" onClick={() => openEngine(l.series)} title={`${l.models.length ? `${l.models.join(", ")}. ` : ""}Open ${l.label}`} className="flex w-full items-start gap-2 rounded-control px-1.5 py-1.5 text-left hover:bg-subtle md:py-1">
                  <LineKey color={l.color} dashed={l.approx} className="mt-1.5" />
                  <span className="min-w-0 flex-1">
                    <span className="block font-medium [overflow-wrap:anywhere]">{l.label}</span>
                    <span className={cx("block", l.approx ? "text-warn" : "text-faint")}>{fidelityInfo[l.fidelity].label}</span>
                  </span>
                  <span className="text-sm font-semibold tabular-nums">{pct(l.latest)}</span>
                  <Icon name="right" className="mt-0.5 size-3.5 text-faint" />
                </button>
              </li>
            ))}
          </ul>
          <details className="text-xs lg:col-span-2">
            <summary className="cursor-pointer py-3.5 text-muted hover:text-fg md:py-0">Show as a table</summary>
            <div className="mt-2">
              <ScrollX label="rates table">
                <table className="w-full min-w-max text-left tabular-nums">
                  <thead className="text-muted">
                    <tr>
                      <th className="sticky left-0 bg-surface py-1 pr-4 font-medium">AI app</th>
                      {xs.map((x) => (
                        <th key={x.key} className="py-1 pr-4 font-medium" title={x.title}>
                          {x.label}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-line">
                    {lines.map((l) => (
                      <tr key={l.key}>
                        <td data-sticky className="sticky left-0 max-w-48 bg-surface py-1 pr-4">
                          {l.label} <span className="text-faint">({fidelityInfo[l.fidelity].label})</span>
                        </td>
                        {l.values.map((v, i) => (
                          <td key={i} className="py-1 pr-4">
                            {pct(v)}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </ScrollX>
            </div>
          </details>
        </div>
      );
  }

  return (
    <Card>
      <CardHeader
        title={`${metric === "mention" ? "Named you" : "Linked to your site"}, by check`}
        description="One line per AI app. Stand-ins for an app are dashed. Open an AI app for its detail."
      >
        <Segmented size="sm" label="Lines" value={by === "surface" ? "surface" : "model"} options={[{ id: "surface", label: "By AI app" }, { id: "model", label: "By model" }]} onChange={(v) => view.set(v === "model" ? { by: "model" } : { by: null, model: null })} />
        <Segmented size="sm" label="Measure" value={metric} options={[{ id: "mention", label: "Named you" }, { id: "citation", label: "Linked" }]} onChange={(v) => view.set({ metric: v })} />
      </CardHeader>
      {body}
    </Card>
  );
}

function ShareCard({ filter }: { filter: ResultsFilter }) {
  const view = useView();
  const comp = useQuery({ queryKey: keys.competitors(filter, "surface"), queryFn: () => api.metrics.competitors(filter, "surface") });
  if (comp.isPending) return <Pending />;
  if (comp.isError) return <ErrorNote error={comp.error} />;
  const self = comp.data.brands.find((b) => b.kind === "self");
  const top = comp.data.brands.filter((b) => b.mentions > 0).slice(0, 6);
  if (self && !top.includes(self)) top.push(self);
  const max = Math.max(...top.map((b) => b.share), 0.01);
  return (
    <Card>
      <CardHeader title="Share of mentions against competitors" description={`${comp.data.mentions} mentions over ${comp.data.runs} ${comp.data.runs === 1 ? "check" : "checks"}. Technologies are left out.`}>
        <Link to={`/ai/competitors${view.filterSearch()}`} className="inline-flex h-11 shrink-0 items-center gap-1 text-xs font-medium text-muted hover:text-fg md:h-auto">
          Competitors <Icon name="right" className="size-3" />
        </Link>
      </CardHeader>
      {top.length === 0 ? (
        <p className="px-4 pb-6 text-sm text-muted">No brand was named in this range.</p>
      ) : (
        <ul className="space-y-2 px-4 pb-4">
          {top.map((b) => (
            <li key={b.brand_id} className="grid grid-cols-[minmax(0,1fr)_3rem] items-center gap-x-3 gap-y-1 text-xs sm:grid-cols-[minmax(6rem,10rem)_minmax(0,1fr)_3rem]">
              <span className={cx("flex min-w-0 items-center gap-1.5", b.kind === "self" ? "font-semibold" : "text-muted")}>
                <BrandMark id={b.brand_id} name={b.name} size={16} />
                <span className="truncate">
                  {b.name}
                  {b.kind === "self" && " (you)"}
                </span>
              </span>
              <span className="col-span-2 row-start-2 h-2 rounded-full bg-subtle sm:col-span-1 sm:row-start-auto">
                <span className="block h-2 rounded-full" style={{ width: `${(b.share / max) * 100}%`, background: b.kind === "self" ? "var(--you)" : "var(--line-hover)" }} />
              </span>
              <span className="col-start-2 row-start-1 text-right font-medium tabular-nums sm:col-start-auto sm:row-start-auto">{pct(b.share)}</span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
