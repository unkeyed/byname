import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type Costs, type Engagement, type SocialMention, type SocialNetwork, type SocialSettings, type SocialSettingsInput } from "../../api";
import { keys } from "../../queries";
import { cx, Icon, type IconName } from "../../ui";

/** How the app shows each network: its name, its mark, and how its authors are written. */
export const networks: Record<SocialNetwork, { title: string; icon?: IconName; mark?: string; prefix: string; byName?: boolean }> = {
  x: { title: "X", icon: "xLogo", prefix: "@" },
  reddit: { title: "Reddit", mark: "r/", prefix: "u/" },
  hackernews: { title: "Hacker News", mark: "Y", prefix: "" },
  linkedin: { title: "LinkedIn", icon: "linkedin", prefix: "", byName: true },
  youtube: { title: "YouTube", icon: "youtube", prefix: "", byName: true },
  threads: { title: "Threads", mark: "@", prefix: "@" },
  bluesky: { title: "Bluesky", mark: "bs", prefix: "@" },
};

export const networkOrder = Object.keys(networks) as SocialNetwork[];

export const settingsPath = "/settings/social";
export const leftOutPath = `${settingsPath}#left-out`;
export const tregPath = "/settings/treg";

/** Saves Settings > Social, and refreshes what it changes: the social reads and the cost estimate. */
export function useSaveSocial(onSaved?: () => void) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: SocialSettingsInput) => api.social.putSettings(body),
    onSuccess: (v) => {
      qc.setQueryData(keys.socialSettings, v);
      void qc.invalidateQueries({ queryKey: keys.social });
      void qc.invalidateQueries({ queryKey: keys.costs });
      onSaved?.();
    },
  });
}

const cents = new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: 2 });

/** What social searching costs a month at the saved settings, in a sentence or two. */
export function socialCost(s: SocialSettings, c: Costs["social"]): string {
  const month = c.treg_month_usd < 0.01 ? "less than a cent a month" : `about ${cents.format(c.treg_month_usd)} a month`;
  if (!s.token_set)
    return `The paid networks need a Treg token. Treg is the service that carries out each search and bills you for it, ${month} at these settings. Bluesky is free and needs no token.`;
  if (c.treg_networks === 0) return "The networks that are on are free.";
  return `Searching costs ${month} at these settings, billed by Treg, the service that carries out each search. Bluesky is free.`;
}

export function NetworkMark({ network, className }: { network: SocialNetwork; className?: string }) {
  const n = networks[network];
  return (
    <span
      title={n.title}
      aria-label={n.title}
      role="img"
      className={cx("inline-grid size-6 shrink-0 place-items-center rounded-control border border-line bg-bg text-[0.625rem] leading-none font-semibold text-muted", className)}
    >
      {n.icon ? <Icon name={n.icon} className="size-3" /> : n.mark}
    </span>
  );
}

/** "@ada" on X, "u/ada" on Reddit, the name on LinkedIn and YouTube. */
export function authorLabel(network: SocialNetwork, handle: string, name: string): string {
  const n = networks[network];
  if ((n.byName && name) || !handle) return name || "Unknown author";
  return n.prefix + handle;
}

const count = new Intl.NumberFormat();

/** The counts the network gave, as "31 likes · 3 replies". A count it did not give is left out, not shown as 0. */
export function engagementParts(network: SocialNetwork, e: Engagement): string[] {
  const comments = network === "reddit" || network === "hackernews" || network === "linkedin";
  const parts: [number | null, string, string][] =
    network === "youtube"
      ? [[e.views, "view", "views"]]
      : [
          [e.likes, network === "linkedin" ? "reaction" : "like", network === "linkedin" ? "reactions" : "likes"],
          [e.score, "point", "points"],
          [e.replies, comments ? "comment" : "reply", comments ? "comments" : "replies"],
          [e.reposts, "repost", "reposts"],
        ];
  return parts.filter(([n]) => n !== null).map(([n, one, many]) => `${count.format(n!)} ${n === 1 ? one : many}`);
}

export const formatCount = (n: number) => count.format(n);

/** "Nov 16 to Nov 22", or one day for a daily digest. */
export function periodLabel(from: string, to: string): string {
  const fmt = (d: string) => new Date(d + "T00:00:00Z").toLocaleDateString(undefined, { month: "short", day: "numeric", timeZone: "UTC" });
  return from === to ? fmt(from) : `${fmt(from)} to ${fmt(to)}`;
}

/** Where to read the post, and a title for the link. */
export const postLink = (m: SocialMention) => ({ href: m.url, label: `Open on ${networks[m.network].title}` });

export const weekdays = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
