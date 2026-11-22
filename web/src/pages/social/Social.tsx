import { useState } from "react";
import { Link } from "react-router";
import type { Costs, LeftOut, SocialDigest, SocialMention, SocialNetwork, SocialSettings } from "../../api";
import { money } from "../../money";
import { useCan, useCosts, useIsAdmin, useLeftOut, useSocialDigest, useSocialMentions, useSocialSettings } from "../../queries";
import { Badge, Button, buttonClass, Card, CardHeader, cx, EmptyState, ErrorNote, Icon, Pending, RelativeTime, scrollArea, selectClass, useLoading } from "../../ui";
import { ViewHeader } from "../results/Header";
import { Note } from "../search/Search";
import { authorLabel, engagementParts, formatCount, leftOutPath, NetworkMark, networkOrder, networks, periodLabel, postLink, settingsPath, socialCost, tregPath, useSaveSocial } from "./model";

const shown = 100;

export function Social() {
  const canEdit = useCan("config:write");
  const settings = useSocialSettings();
  const costs = useCosts();
  const leftOut = useLeftOut();
  return (
    <>
      <ViewHeader
        title="Social"
        subtitle="Mentions: posts that name you on X, Reddit, Hacker News, LinkedIn, YouTube, Threads and Bluesky. Posts by the people you left out are hidden, so this is what everyone else says."
        range={false}
        scope={false}
        action={
          <Link to={settingsPath} className={buttonClass({ size: "sm" })}>
            <Icon name="settings" className="size-3.5" />
            {canEdit ? "Social settings" : "Settings"}
          </Link>
        }
      />
      <div className={cx("relative min-h-0 flex-1 px-3 py-4 md:px-5 md:py-5", scrollArea)}>
        <div className="mx-auto max-w-4xl space-y-4">
          {settings.isPending || costs.isPending || leftOut.isPending ? (
            <Pending />
          ) : settings.isError ? (
            <ErrorNote error={settings.error} />
          ) : settings.data.enabled ? (
            <>
              <Notices s={settings.data} leftOut={leftOut.data} />
              <DigestCard />
              <MentionsCard s={settings.data} />
            </>
          ) : (
            <TurnOn s={settings.data} costs={costs.data?.social} />
          )}
        </div>
      </div>
    </>
  );
}

/** Social is off: what it would do and cost, and the switch for those who may change settings. */
function TurnOn({ s, costs }: { s: SocialSettings; costs: Costs["social"] | undefined }) {
  const canEdit = useCan("config:write");
  const save = useSaveSocial();
  const turnOn = () =>
    save.mutate({
      enabled: true,
      terms: s.custom_terms ? s.terms : [],
      fetch_every_hours: s.fetch_every_hours,
      digest_cadence: s.digest_cadence,
      digest_weekday: s.digest_weekday,
    });
  return (
    <EmptyState
      icon="message"
      title="Social mentions are off"
      action={
        canEdit && (
          <div className="flex flex-col items-center gap-2">
            <div className="flex flex-wrap justify-center gap-2">
              <Button variant="primary" onClick={turnOn} disabled={save.isPending}>
                Turn on social mentions
              </Button>
              <Link to={settingsPath} className={buttonClass()}>
                Choose what to search first
              </Link>
            </div>
            <ErrorNote error={save.error} />
          </div>
        )
      }
    >
      Byname can watch X, Reddit, Hacker News, LinkedIn, YouTube, Threads and Bluesky for posts that name you.
      {costs && ` ${socialCost(s, costs)}`}
      {s.last_fetch_at && " The mentions found before it was turned off are kept and show again."}
      {!canEdit && " An admin or editor can turn it on."}
    </EmptyState>
  );
}

function Notices({ s, leftOut }: { s: SocialSettings; leftOut: LeftOut[] | undefined }) {
  const admin = useIsAdmin();
  const canEdit = useCan("config:write");
  const on = s.networks.filter((n) => n.enabled);
  const unavailable = on.filter((n) => n.unavailable);
  return (
    <>
      {leftOut?.length === 0 && (
        <Note tone="info" icon="note">
          Your team&apos;s own posts are included. Leave out your team and company accounts so you see what other people say.{" "}
          {canEdit ? (
            <Link to={leftOutPath} className="font-medium underline">
              Leave out accounts
            </Link>
          ) : (
            "An admin or editor can leave them out in Settings, Social."
          )}
        </Note>
      )}
      {!s.token_set && on.some((n) => n.route === "direct") && on.some((n) => n.route === "treg") && (
        <Note tone="info" icon="note">
          Only Bluesky is searched until a Treg token is added. The other networks are searched through Treg, the service that carries out each search and bills you for it.{" "}
          {admin ? (
            <Link to={tregPath} className="font-medium underline">
              Add the Treg token
            </Link>
          ) : (
            "An admin can add it in Settings, Treg."
          )}
        </Note>
      )}
      {unavailable.map((n) => (
        <Note key={n.network} tone="warn" icon="alert">
          {n.title} search is unavailable: its public search now asks for sign-in, so no new {n.title} posts come in. The ones found so far stay.
        </Note>
      ))}
    </>
  );
}

function DigestCard() {
  const digest = useSocialDigest();
  useLoading(digest.isPending);
  if (digest.isPending) return null;
  if (digest.isError) return <ErrorNote error={digest.error} />;
  const d = digest.data;
  if (d.calls === 0 && d.mentions === 0 && d.hidden === 0) return null;
  return (
    <Card>
      <CardHeader
        title={`Latest digest: ${d.cadence === "weekly" ? "week of " : ""}${periodLabel(d.from, d.to)}`}
        description={
          <>
            New mentions first seen {d.cadence === "weekly" ? "that week" : "that day"}, the most engaged first. {searchCost(d)}
          </>
        }
      />
      <div className="space-y-4 border-t border-line px-4 py-3">
        <div className="flex flex-wrap items-baseline gap-x-6 gap-y-2">
          <Stat value={d.mentions} label={d.mentions === 1 ? "new mention" : "new mentions"} />
          <Stat value={d.new_authors.length} label={d.new_authors.length === 1 ? "new person" : "new people"} />
          {d.hidden > 0 && <Stat value={d.hidden} label="from people you left out" muted />}
        </div>
        {d.networks.length > 0 && (
          <ul aria-label="Mentions by network" className="flex flex-wrap gap-2">
            {d.networks.map((n) => (
              <li key={n.network} className="flex items-center gap-1.5 rounded-control border border-line px-2 py-1 text-xs">
                <NetworkMark network={n.network} className="size-5" />
                {n.title}
                <span className="font-semibold tabular-nums">{n.mentions}</span>
              </li>
            ))}
          </ul>
        )}
        {d.networks.length > 0 && (
          <div>
            <h3 className="mb-1 text-xs font-medium text-muted">Most engaged</h3>
            <ul className="divide-y divide-line">
              {d.networks
                .map((n) => n.top[0])
                .filter(Boolean)
                .slice(0, 3)
                .map((m) => (
                  <li key={m.id} className="py-2">
                    <MentionLine m={m} />
                  </li>
                ))}
            </ul>
          </div>
        )}
        {d.new_authors.length > 0 && (
          <p className="text-xs text-muted">
            <span className="font-medium text-fg">New people: </span>
            {d.new_authors
              .slice(0, 8)
              .map((a) => `${authorLabel(a.network, a.handle, a.name)} on ${networks[a.network].title}`)
              .join(", ")}
            {d.new_authors.length > 8 && ` and ${d.new_authors.length - 8} more`}.
          </p>
        )}
      </div>
    </Card>
  );
}

function searchCost(d: SocialDigest): string {
  const cost = `Its ${d.calls} searches cost ${money(d.spend_usd)}`;
  return d.spend_missing_calls > 0 ? `${cost}, plus ${d.spend_missing_calls} that reported no cost.` : `${cost}.`;
}

function Stat({ value, label, muted }: { value: number; label: string; muted?: boolean }) {
  return (
    <span className={cx("flex items-baseline gap-1.5", muted && "text-muted")}>
      <span className="text-2xl font-semibold tabular-nums">{formatCount(value)}</span>
      <span className="text-sm text-muted">{label}</span>
    </span>
  );
}

function MentionsCard({ s }: { s: SocialSettings }) {
  const [network, setNetwork] = useState<SocialNetwork | "">("");
  const [showHidden, setShowHidden] = useState(false);
  const mentions = useSocialMentions({ network: network || undefined, include_hidden: showHidden, limit: shown });
  const on = s.networks.filter((n) => n.enabled);
  const runnable = on.filter((n) => s.token_set || n.route === "direct");
  const l = mentions.data;

  let body;
  if (mentions.isPending) body = <Pending />;
  else if (mentions.isError) body = <div className="px-4 pb-4"><ErrorNote error={mentions.error} /></div>;
  else if (l!.total + l!.hidden === 0) body = <div className="px-4 pb-4"><Empty s={s} filtered={network !== ""} runnable={runnable.length} on={on.length} /></div>;
  else if (l!.mentions.length === 0)
    body = (
      <div className="px-4 pb-4">
        <EmptyState icon="users" title="Every mention here is by someone you left out">
          Turn on the toggle above to see them.
        </EmptyState>
      </div>
    );
  else
    body = (
      <ul aria-label="Mentions" className="divide-y divide-line border-t border-line">
        {l!.mentions.map((m) => (
          <li key={m.id} className={cx("px-4 py-3", m.hidden && "bg-subtle/50")}>
            <MentionLine m={m} full />
          </li>
        ))}
      </ul>
    );

  return (
    <Card>
      <CardHeader
        title="Mentions"
        description={
          l && l.total > 0
            ? `${l.total > l.mentions.filter((m) => !m.hidden).length ? `The newest ${shown} of ${formatCount(l.total)}` : formatCount(l.total)} ${l.total === 1 ? "post" : "posts"}, newest first. ${nextFetch(s)}`
            : nextFetch(s)
        }
      >
        <select aria-label="Network" className={cx(selectClass, "text-xs")} value={network} onChange={(e) => setNetwork(e.target.value as SocialNetwork | "")}>
          <option value="">All networks</option>
          {networkOrder.map((n) => (
            <option key={n} value={n}>
              {networks[n].title}
            </option>
          ))}
        </select>
        {l && (l.hidden > 0 || showHidden) && (
          <label className="flex h-11 items-center gap-1.5 text-xs text-muted md:h-8">
            <input type="checkbox" checked={showHidden} onChange={(e) => setShowHidden(e.target.checked)} className="size-3.5 accent-[var(--fg)]" />
            {showHidden ? "Showing" : "Show"} {l.hidden} hidden from people you left out
          </label>
        )}
      </CardHeader>
      {body}
    </Card>
  );
}

function nextFetch(s: SocialSettings): string {
  return `Searched every ${s.fetch_every_hours === 1 ? "hour" : `${s.fetch_every_hours} hours`} for ${s.terms.map((t) => `"${t}"`).join(" and ") || "your brand"}.`;
}

function Empty({ s, filtered, runnable, on }: { s: SocialSettings; filtered: boolean; runnable: number; on: number }) {
  const admin = useIsAdmin();
  const canEdit = useCan("config:write");
  if (on === 0)
    return (
      <EmptyState
        icon="message"
        title="Every network is off"
        action={
          canEdit && (
            <Link to={settingsPath} className={buttonClass({ variant: "primary" })}>
              Turn networks on
            </Link>
          )
        }
      >
        {canEdit ? "Choose the networks to search in Settings, Social." : "An admin or editor can turn networks on in Settings, Social."}
      </EmptyState>
    );
  if (runnable === 0)
    return (
      <EmptyState
        icon="key"
        title="Add a Treg token to search"
        action={
          admin && (
            <Link to={tregPath} className={buttonClass({ variant: "primary" })}>
              Add the Treg token
            </Link>
          )
        }
      >
        {`The networks that are on are searched through Treg, which needs a token. Treg is the service that carries out each search and bills you for it. ${admin ? "Add it in Settings, Treg." : "An admin can add it in Settings, Treg."} Bluesky needs none.`}
      </EmptyState>
    );
  if (filtered) return <EmptyState icon="search" title="No mentions on this network yet" />;
  return (
    <EmptyState icon="message" title="No mentions yet">
      {s.last_fetch_at
        ? `The searches so far found no posts with ${s.terms.map((t) => `"${t}"`).join(" or ")}. If that seems wrong, check the search terms in Settings, Social.`
        : `The first search starts within a minute, then repeats every ${s.fetch_every_hours === 1 ? "hour" : `${s.fetch_every_hours} hours`}.`}
    </EmptyState>
  );
}

function MentionLine({ m, full }: { m: SocialMention; full?: boolean }) {
  const link = postLink(m);
  const parts = engagementParts(m.network, m.engagement);
  return (
    <div className="flex items-start gap-3">
      <NetworkMark network={m.network} className="mt-0.5" />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs">
          <span className="font-medium text-fg [overflow-wrap:anywhere]">{authorLabel(m.network, m.author_handle, m.author_name)}</span>
          {m.author_name && !networks[m.network].byName && m.author_handle && <span className="text-muted [overflow-wrap:anywhere]">{m.author_name}</span>}
          <span className="text-faint" title={m.posted_approx ? `${networks[m.network].title} gives only the age, so this time is approximate` : undefined}>
            <RelativeTime iso={m.posted_at} />
            {m.posted_approx && ", roughly"}
          </span>
          {m.hidden && <Badge tone="outline">Left out: {m.left_out_as}</Badge>}
        </div>
        <p className={cx("mt-1 text-sm whitespace-pre-line [overflow-wrap:anywhere]", full ? "line-clamp-4" : "line-clamp-2", m.hidden && "text-muted")}>{m.text || "(no text)"}</p>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted">
          {parts.length > 0 && <span className="tabular-nums">{parts.join(" · ")}</span>}
          <a href={link.href} target="_blank" rel="noreferrer" className="inline-flex h-11 items-center gap-1 font-medium text-fg hover:underline md:h-auto">
            {link.label}
            <Icon name="external" className="size-3" />
          </a>
        </div>
      </div>
    </div>
  );
}
