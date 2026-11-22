import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type GscStatus, type SearchTotals } from "../../api";
import { dataVersion, keys } from "../../queries";
import { Badge, Button, Card, CardHeader, cx, ErrorNote, Icon, Pending, ScrollX } from "../../ui";
import { count, ctr, dayRange, figureInfo, position } from "./model";

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

export function TopPages({ status }: { status: GscStatus }) {
  const [limit, setLimit] = useState(shortLimit);
  const q = useQuery({ queryKey: keys.searchPages(limit, dataVersion(status)), queryFn: () => api.search.pages(limit) });
  return (
    <TopCard
      title="Top pages"
      what="page"
      range={q.data && dayRange(q.data.from, q.data.to)}
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

export function TopQueries({ status, nonBrand }: { status: GscStatus; nonBrand: boolean }) {
  const [limit, setLimit] = useState(shortLimit);
  const q = useQuery({ queryKey: keys.searchQueries(limit, nonBrand, dataVersion(status)), queryFn: () => api.search.queries(limit, nonBrand) });
  return (
    <TopCard
      title={nonBrand ? "Top searches without your name" : "Top searches"}
      what="search"
      range={q.data && dayRange(q.data.from, q.data.to)}
      note={nonBrand ? `${figureInfo.non_brand.hint} Google hides rare searches, so these undercount.` : undefined}
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
  range,
  note,
  rows,
  error,
  dim,
  limit,
  setLimit,
}: {
  title: string;
  what: string;
  range?: string;
  note?: string;
  rows?: { key: string; name: ReactNode; totals: SearchTotals }[];
  error: unknown;
  dim: boolean;
  limit: number;
  setLimit: (n: number) => void;
}) {
  let body;
  if (error) body = <div className="px-4 pb-4"><ErrorNote error={error} /></div>;
  else if (!rows) body = <Pending />;
  else if (rows.length === 0) body = <p className="px-4 pb-6 text-sm text-muted">No {what === "page" ? "pages" : "searches"} had appearances in these 28 days.</p>;
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
                <th className="py-1.5 pr-3 text-right font-medium">{figureInfo.impressions.label}</th>
                <th className="py-1.5 pr-3 text-right font-medium">{figureInfo.ctr.label}</th>
                <th className="py-1.5 pr-4 text-right font-medium">{figureInfo.position.label}</th>
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
      <CardHeader title={title} description={[range ? `Latest 28 days, ${range}, by clicks.` : "Latest 28 days, by clicks.", note].filter(Boolean).join(" ")}>
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
