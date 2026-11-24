import type { ReactNode } from "react";
import { Link } from "react-router";
import type { Alert, EventKindName, EventPayloads, GoalMetric, Run, SocialDigestPayload, SocialNetwork, Surface } from "../../api";
import { andList, pct } from "../../lines";
import { cx, Icon, RelativeTime, type IconName } from "../../ui";
import { formatCount, NetworkMark, networks } from "../social/model";

/** What a card needs beyond its payload: earlier checks for the normal range, and AI app names. */
export type CardContext = { runs: Run[]; surfaces: Surface[] };

/**
 * One notification as a card: what it is about, one sentence, at most two
 * short lines, and the words of its one link. The link goes to the
 * notification's path, the same page its email links to.
 */
type CardView = {
  icon: IconName;
  tone: CardTone;
  /** What the notification is about, such as "Check #12", above the title. */
  about: string;
  title: string;
  lines: ReactNode[];
  /** Shown under the lines, for the social digest's networks and posts. Its links call onOpen. */
  extra?: (onOpen: () => void) => ReactNode;
  action: string;
};

type Cards = { [K in EventKindName]: (p: EventPayloads[K], ctx: CardContext) => CardView };

/** Every event kind's card. A kind added to EventPayloads fails to compile until it has one here. */
const cards: Cards = {
  check_completed: (p, ctx) => {
    const about = `Check #${p.run_id}`;
    if (p.answered === 0)
      return { icon: "sparkles", tone: "neutral", about, title: "The check finished with no answers that count in scores", lines: [], action: "Open This week" };
    const lines: ReactNode[] = [];
    const band = normalRange(p.run_id, ctx.runs);
    const rate = p.named / p.answered;
    if (band) lines.push(`${rate < band.low ? "Below" : rate > band.high ? "Above" : "Inside"} your normal range of ${rangeLabel(band)} over the ${band.checks} checks before.`);
    if (p.previous_run_id === 0) lines.push("Your first check. The next one shows which answers start or stop naming you.");
    else lines.push(changeLine(p.gained, p.lost));
    return {
      icon: "sparkles",
      tone: band && rate < band.low ? "warn" : "info",
      about,
      title: `AI apps named you in ${p.named} of ${p.answered} answers (${pct(rate)})`,
      lines,
      action: p.previous_run_id === 0 ? "Open This week" : "See what changed",
    };
  },
  check_refused: (p) => ({
    icon: "alert",
    tone: "danger",
    about: `Check #${p.run_id}`,
    title: "Treg refused a request, so the check stopped",
    lines: [
      capitalize(p.message),
      p.code === "insufficient_balance"
        ? "Add money to your Treg balance, then press Check now. Treg runs each question and bills you per answer."
        : "Fix what Treg reports, then press Check now. Treg runs each question and bills you per answer.",
    ],
    action: "Open check history",
  }),
  check_failed: (p) => ({ icon: "alert", tone: "danger", about: `Check #${p.run_id}`, title: "The check failed", lines: [capitalize(p.message)], action: "Open check history" }),
  spend_cap_reached: (p) => ({
    icon: "alert",
    tone: "warn",
    about: `Check #${p.run_id}`,
    title: "The check stopped at its spend cap",
    lines: [capitalize(p.message), "The answers so far are kept. Raise the cap to ask every question next time."],
    action: "Open schedule and limits",
  }),
  pair_paused: (p, ctx) => ({
    icon: "bot",
    tone: "warn",
    about: "AI apps",
    title: `Byname stopped asking ${p.model} on ${ctx.surfaces.find((s) => s.key === p.surface)?.title ?? p.surface}`,
    lines: [p.reason, "Pick another model to keep this AI app in your checks."],
    action: "Open AI apps settings",
  }),
  goal_reached: (p) => ({
    icon: "check",
    tone: "success",
    about: `${p.engine} search`,
    title: `Goal reached: ${formatCount(p.value)} ${metricWords(p.engine, p.metric)} in ${p.window_days} days`,
    lines: [`Your target was ${formatCount(p.target)} by ${dayLabel(p.due_on)}.`],
    action: `Open ${p.engine} traffic`,
  }),
  search_moved: (p) => {
    const change = p.before > 0 ? Math.round((Math.abs(p.after - p.before) / p.before) * 100) : null;
    const fell = p.after < p.before;
    return {
      icon: "search",
      tone: fell ? "warn" : "success",
      about: `${p.engine} search`,
      title: `${capitalize(metricWords(p.engine, p.metric))} ${fell ? "fell" : "rose"}${change === null ? "" : ` ${change}%`} over the last ${p.window_days} days`,
      lines: [`${formatCount(p.after)}, against ${formatCount(p.before)} in the ${p.window_days} days before.`],
      action: `Open ${p.engine} traffic`,
    };
  },
  change_verdict: (p) => ({
    icon: "merge",
    tone: p.verdict === "improved" ? "success" : p.verdict === "worsened" ? "danger" : "neutral",
    about: "Site change",
    title: verdictTitles[p.verdict],
    lines: [shortURL(p.url), ...(p.detail ? [p.detail] : [])],
    action: "Open the change",
  }),
  token_expiring: (p) => ({
    icon: "key",
    tone: "warn",
    about: "Site changes",
    title: `The ${p.provider} token for ${p.repo} expires ${dayLabel(p.expires_at)}`,
    lines: ["Paste a new token before then, or site changes stop updating."],
    action: "Open site change settings",
  }),
  social_digest: (p) => ({
    icon: "message",
    tone: p.mentions > 0 ? "info" : "neutral",
    about: p.cadence === "daily" ? "Daily social digest" : "Weekly social digest",
    title: `${p.mentions === 0 ? "No new posts" : `${formatCount(p.mentions)} new ${p.mentions === 1 ? "post" : "posts"}`} named you${p.cadence === "daily" ? ` on ${dayLabel(p.from)}` : `, ${rangeOfDays(p.from, p.to)}`}`,
    lines: [],
    extra: (onOpen) => <DigestBody p={p} onOpen={onOpen} />,
    action: p.mentions > 0 ? "See all posts on Social" : "Open Social",
  }),
  page_linked: (p) => ({
    icon: "link",
    tone: "success",
    about: "Handled questions",
    title: `${andList(p.apps)} linked to ${shortURL(p.url)}`,
    lines: [`${p.answers} ${p.answers === 1 ? "answer" : "answers"} linked to it, the first since you marked it as answering “${p.question_text}”.`],
    action: "Open handled questions",
  }),
};

export function cardFor(a: Alert, ctx: CardContext): CardView {
  // TypeScript cannot narrow a lookup keyed by a union, so the kind-to-payload pairing is checked where Cards is defined.
  return (cards[a.kind] as (p: Alert["payload"], ctx: CardContext) => CardView)(a.payload, ctx);
}

type CardTone = "neutral" | "success" | "warn" | "danger" | "info";

const iconTones: Record<CardTone, string> = {
  neutral: "bg-subtle text-muted",
  success: "bg-success-soft text-success",
  warn: "bg-warn-soft text-warn",
  danger: "bg-danger-soft text-danger",
  info: "bg-info-soft text-info",
};

/**
 * A notification as a card. The whole card opens its page, through a link
 * laid over it for the pointer; the action link is the one keyboard and
 * screen readers use, and links inside the card stay clickable above the
 * cover. Opening any of them marks the notification read.
 */
export function NotificationCard({ alert: a, ctx, onOpen }: { alert: Alert; ctx: CardContext; onOpen: () => void }) {
  const c = cardFor(a, ctx);
  return (
    <li className="relative rounded-surface border border-line bg-surface transition-colors hover:border-line-hover">
      <Link to={a.path} onClick={onOpen} tabIndex={-1} aria-hidden="true" className="absolute inset-0 z-[1] rounded-surface" />
      <div className="flex gap-3 px-4 py-3">
        <span aria-hidden="true" className={cx("mt-0.5 grid size-8 shrink-0 place-items-center rounded-control", iconTones[c.tone])}>
          <Icon name={c.icon} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5 text-xs text-muted">
            <span className="min-w-0 truncate">{c.about}</span>
            <span aria-hidden="true">·</span>
            <RelativeTime iso={a.created_at} className="shrink-0" />
            <span aria-hidden="true" className={cx("ml-auto size-2 shrink-0 rounded-full bg-info", a.read && "invisible")} />
          </div>
          <h3 className={cx("mt-0.5 text-sm text-balance [overflow-wrap:anywhere]", a.read ? "font-medium text-fg" : "font-semibold text-fg")}>
            {c.title}
            {!a.read && <span className="sr-only"> (unread)</span>}
          </h3>
          {c.lines.slice(0, 2).map((l, i) => (
            <p key={i} title={typeof l === "string" ? l : undefined} className="mt-0.5 line-clamp-2 text-sm text-muted [overflow-wrap:anywhere]">
              {l}
            </p>
          ))}
          {c.extra && <div className="mt-2">{c.extra(onOpen)}</div>}
          <Link to={a.path} onClick={onOpen} className="relative z-[2] mt-1 inline-flex h-11 items-center gap-1 text-sm font-medium text-fg underline-offset-2 hover:underline md:h-8">
            {c.action}
            <Icon name="right" className="size-3.5" />
          </Link>
        </div>
      </div>
    </li>
  );
}

/** The social digest's networks, new people, top three posts and left-out note. */
function DigestBody({ p, onOpen }: { p: SocialDigestPayload; onOpen: () => void }) {
  const active = p.networks.filter((n) => n.mentions > 0);
  const top = p.networks
    .flatMap((n) => n.top.map((post) => ({ ...post, network: n.network })))
    .sort((a, b) => b.engagement - a.engagement)
    .slice(0, 3);
  return (
    <div className="space-y-2 text-sm">
      {active.length > 0 && (
        <ul aria-label="Posts by network" className="flex flex-wrap gap-1.5">
          {active.map((n) => (
            <li key={n.network} className="flex items-center gap-1.5 rounded-control border border-line py-0.5 pr-2 pl-0.5 text-xs">
              <NetworkMark network={n.network} className="size-5 border-0" />
              <span className="text-muted">{n.title}</span>
              <span className="font-semibold tabular-nums">{formatCount(n.mentions)}</span>
            </li>
          ))}
        </ul>
      )}
      {p.new_authors_count > 0 && (
        <p className="text-muted">
          {formatCount(p.new_authors_count)} {p.new_authors_count === 1 ? "person" : "people"} posted about you for the first time.
        </p>
      )}
      {top.length > 0 && (
        <ul aria-label="The posts people engaged with most" className="space-y-1">
          {top.map((post) => (
            <li key={post.url} className="min-w-0">
              <a href={post.url} target="_blank" rel="noreferrer" title={post.text} onClick={onOpen} className="group relative z-[2] flex min-h-11 flex-col justify-center md:min-h-0">
                <span className="truncate text-fg group-hover:underline">“{post.text}”</span>
                <span className="truncate text-xs text-muted">
                  {post.author} on {networks[post.network].title}
                  {post.engagement > 0 && `, ${engagement(post.network, post.engagement)}`}
                </span>
              </a>
            </li>
          ))}
        </ul>
      )}
      {p.hidden > 0 && (
        <p className="text-xs text-muted">
          {p.hidden === 1 ? "1 post from an account you left out is not counted." : `${formatCount(p.hidden)} posts from accounts you left out are not counted.`}
        </p>
      )}
    </div>
  );
}

/** What a post's engagement count adds up on each network, as the digest email says it. */
const engagementNouns: Partial<Record<SocialNetwork, [string, string]>> = {
  youtube: ["view", "views"],
  hackernews: ["point or comment", "points and comments"],
  reddit: ["upvote or comment", "upvotes and comments"],
  linkedin: ["reaction or comment", "reactions and comments"],
};

function engagement(network: SocialNetwork, n: number): string {
  const [one, many] = engagementNouns[network] ?? ["like, reply or repost", "likes, replies and reposts"];
  return `${formatCount(n)} ${n === 1 ? one : many}`;
}

/** At least this many earlier checks make a normal range, and at most this many count, as on This week. */
const rangeMin = 3;
const rangeMax = 5;

type Band = { low: number; high: number; checks: number };

/** The spread of "named you" over the checks before this one, from the checks list. */
function normalRange(runID: number, runs: Run[]): Band | null {
  const xs = runs
    .filter((r) => r.id < runID && r.scored > 0)
    .sort((a, b) => b.id - a.id)
    .slice(0, rangeMax)
    .map((r) => r.mentioned / r.scored);
  if (xs.length < rangeMin) return null;
  return { low: Math.min(...xs), high: Math.max(...xs), checks: xs.length };
}

const rangeLabel = (b: Band) => (Math.round(b.low * 100) === Math.round(b.high * 100) ? pct(b.low) : `${Math.round(b.low * 100)} to ${pct(b.high)}`);

function changeLine(gained: number, lost: number): string {
  if (gained === 0 && lost === 0) return "No answer started or stopped naming you since the check before.";
  const parts = [];
  if (gained > 0) parts.push(`${gained} ${gained === 1 ? "answer" : "answers"} started naming you`);
  if (lost > 0) parts.push(gained > 0 ? `${lost} stopped` : `${lost} ${lost === 1 ? "answer" : "answers"} stopped naming you`);
  return `${parts.join(", ")}.`;
}

/** A search metric on one engine, such as "Bing clicks". */
const metricWords = (engine: string, m: GoalMetric) =>
  ({ clicks: `${engine} clicks`, impressions: `appearances in ${engine} results`, non_brand_clicks: `clicks from ${engine} searches without your name` })[m];

const verdictTitles: Record<EventPayloads["change_verdict"]["verdict"], string> = {
  improved: "A site change helped",
  worsened: "A site change hurt",
  no_change: "A site change made no clear difference",
  insufficient_data: "Not enough Google data to judge a site change",
};

const capitalize = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

/** The page without its scheme, such as northwind.example/pricing. */
const shortURL = (url: string) => url.replace(/^https?:\/\//, "").replace(/\/$/, "");

const dayFmt = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", timeZone: "UTC" });

/** A day such as 2026-11-09, or the date of a timestamp, as "Nov 9". */
const dayLabel = (day: string) => dayFmt.format(new Date(day.length === 10 ? `${day}T00:00:00Z` : day));

/** "Nov 9 to 15", or "Nov 28 to Dec 4" across a month's end. */
function rangeOfDays(from: string, to: string): string {
  const f = new Date(`${from}T00:00:00Z`);
  const t = new Date(`${to}T00:00:00Z`);
  return f.getUTCMonth() === t.getUTCMonth() ? `${dayLabel(from)} to ${t.getUTCDate()}` : `${dayLabel(from)} to ${dayLabel(to)}`;
}
