import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type SearchStatus, type SearchTotals, type TopList } from "../../api";
import { dataVersion, keys } from "../../queries";
import { Badge, Button, Card, CardHeader, cx, ErrorNote, Icon, Pending, ScrollX } from "../../ui";
import { count, ctr, dayRange, figureInfo, position, spanWords } from "./model";

const shortLimit = 10;
const longLimit = 50;

/** A page's address without its scheme and host when it is on the property's own site. */
function pagePath(url: string): string {
  try {
    const u = new URL(url);
    return u.pathname + u.search || "/";
  } catch {
    return url;
  }
}

/** The window a list covers, as "Latest 4 weeks, Oct 17 to Nov 13". */
function windowLabel(list: TopList | undefined, s: SearchStatus): string {
  if (!list) return "Latest 28 days";
  const days = Math.round((Date.parse(list.to) - Date.parse(list.from)) / 86_400_000) + 1;
  return `Latest ${spanWords(days, s.step_days)}, ${dayRange(list.from, list.to)}`;
}

/** Weekly engines list their top rows a week at a time, so their lists are sums of weekly lists. */
const weeklyNote = (s: SearchStatus) => (s.step_days > 1 ? `Sums of ${s.title}'s weekly top lists.` : undefined);

export function TopPages({ status }: { status: SearchStatus }) {
  const [limit, setLimit] = useState(shortLimit);
  const q = useQuery({ queryKey: keys.searchPages(status.provider, limit, dataVersion(status)), queryFn: () => api.search.pages(status.provider, limit) });
  return (
    <TopCard
      title="Top pages"
      what="page"
      engine={status.title}
      span={windowLabel(q.data, status)}
      note={weeklyNote(status)}
      rows={q.data?.pages.map((p) => ({
        key: p.page,
        totals: p,
        name: (
          <a href={p.page} target="_blank" rel="noreferrer" className="group inline-flex h-11 max-w-full min-w-11 items-center gap-1 hover:underline md:h-auto md:min-w-0" title={p.page}>
            <span className="truncate">{pagePath(p.page)}</span>
            <Icon name="external" className="size-3 text-faint group-hover:text-fg" />
          </a>
        ),
      }))}
      error={q.error}
      dim={q.isPlaceholderData}
      limit={limit}
      setLimit={setLimit}
    />
  );
}

export function TopQueries({ status, nonBrand }: { status: SearchStatus; nonBrand: boolean }) {
  const [limit, setLimit] = useState(shortLimit);
  const q = useQuery({
    queryKey: keys.searchQueries(status.provider, limit, nonBrand, dataVersion(status)),
    queryFn: () => api.search.queries(status.provider, limit, nonBrand),
  });
  const undercount = status.step_days > 1 ? `${status.title} lists only its top searches each week, so these undercount.` : `${status.title} hides rare searches, so these undercount.`;
  return (
    <TopCard
      title={nonBrand ? "Top searches without your name" : "Top searches"}
      what="search"
      engine={status.title}
      span={windowLabel(q.data, status)}
      note={nonBrand ? `${figureInfo(status.title).non_brand.hint} ${undercount}` : weeklyNote(status)}
      rows={q.data?.queries.map((r) => ({
        key: r.query,
        totals: r,
        name: (
          <span className="flex min-w-0 items-center gap-1.5">
            <span className="truncate" title={r.query}>
              {r.query}
            </span>
            {!nonBrand && r.branded && <Badge className="shrink-0">Your name</Badge>}
          </span>
        ),
      }))}
      error={q.error}
      dim={q.isPlaceholderData}
      limit={limit}
      setLimit={setLimit}
    />
  );
}

function TopCard({
  title,
  what,
  engine,
  span,
  note,
  rows,
  error,
  dim,
  limit,
  setLimit,
}: {
  title: string;
  what: string;
  engine: string;
  span: string;
  note?: string;
  rows?: { key: string; name: ReactNode; totals: SearchTotals }[];
  error: unknown;
  dim: boolean;
  limit: number;
  setLimit: (n: number) => void;
}) {
  const info = figureInfo(engine);
  let body;
  if (error) body = <div className="px-4 pb-4"><ErrorNote error={error} /></div>;
  else if (!rows) body = <Pending />;
  else if (rows.length === 0) body = <p className="px-4 pb-6 text-sm text-muted">No {what === "page" ? "pages" : "searches"} had appearances in this window.</p>;
  else
    body = (
      <div className={cx("pb-2", dim && "opacity-60")}>
        <ScrollX label={title}>
          <table className="w-full min-w-[32rem] table-fixed text-left text-sm">
            <colgroup>
              <col />
              <col className="w-18" />
              <col className="w-26" />
              <col className="w-22" />
              <col className="w-22" />
            </colgroup>
            <thead className="text-xs text-muted">
              <tr className="border-y border-line">
                <th className="py-1.5 pr-3 pl-4 font-medium capitalize">{what}</th>
                <th className="py-1.5 pr-3 text-right font-medium">Clicks</th>
                <th className="py-1.5 pr-3 text-right font-medium">{info.impressions.label}</th>
                <th className="py-1.5 pr-3 text-right font-medium">{info.ctr.label}</th>
                <th className="py-1.5 pr-4 text-right font-medium">{info.position.label}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line tabular-nums">
              {rows.map((r) => (
                <tr key={r.key}>
                  <td className="max-w-0 py-1.5 pr-3 pl-4">{r.name}</td>
                  <td className="py-1.5 pr-3 text-right font-medium">{count(r.totals.clicks)}</td>
                  <td className="py-1.5 pr-3 text-right text-muted">{count(r.totals.impressions)}</td>
                  <td className="py-1.5 pr-3 text-right text-muted">{ctr(r.totals.ctr)}</td>
                  <td className="py-1.5 pr-4 text-right text-muted">{position(r.totals.position)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </ScrollX>
      </div>
    );
  return (
    <Card className="min-w-0">
      <CardHeader title={title} description={[`${span}, by clicks.`, note].filter(Boolean).join(" ")}>
        {rows && (rows.length >= limit || limit > shortLimit) && (
          <Button size="sm" variant="ghost" onClick={() => setLimit(limit > shortLimit ? shortLimit : longLimit)}>
            <Icon name={limit > shortLimit ? "up" : "down"} className="size-3.5" />
            {limit > shortLimit ? "Show fewer" : `Show up to ${longLimit}`}
          </Button>
        )}
      </CardHeader>
      {body}
    </Card>
  );
}
