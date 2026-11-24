import type { ReactNode } from "react";
import { Link } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { api, type SearchPeriod, type SearchStatus } from "../../api";
import { keys, useMe, useSearchProviders } from "../../queries";
import { buttonClass, Card, CardHeader, cx, EmptyState, ErrorNote, Icon, Pending, RelativeTime, scrollArea, Spinner, useLoading } from "../../ui";
import { useView } from "../../view";
import { Segmented, ViewHeader } from "../results/Header";
import { BrandCard } from "./BrandCard";
import { DailyChart } from "./DailyChart";
import { GoalsCard } from "./Goals";
import {
  change,
  dayRange,
  figureInfo,
  fullDay,
  headline,
  lastDay,
  searchState,
  shownEngines,
  siteName,
  spanFrom,
  spans,
  spanWords,
  useSearchDaily,
  useSearchSummary,
  type Figure,
  type HeadlineFigure,
  type SearchState,
} from "./model";
import { TopPages, TopQueries } from "./Tops";

export const settingsPath = "/settings/search-engines";

/** One search engine at a time: their days and what they count differ, so the page never adds them together. */
export function Search() {
  const providers = useSearchProviders();
  const me = useMe();
  const view = useView();
  const canEdit = !!me.data?.scopes.includes("config:write");
  const admin = me.data?.role === "admin";

  let body;
  let pick: ReactNode = null;
  if (providers.isPending) body = <Pending />;
  else if (providers.isError) body = <ErrorNote error={providers.error} />;
  else {
    const shown = shownEngines(providers.data);
    const status = shown.find((s) => s.provider === view.get("provider")) ?? shown[0];
    if (!status) body = <NoEngine admin={admin} />;
    else {
      const state = searchState(status);
      body = state === "ready" ? <Ready key={status.provider} status={status} canEdit={canEdit} admin={admin} /> : <NotReady state={state} status={status} admin={admin} />;
    }
    if (shown.length > 1)
      pick = <Segmented label="Search engine" value={status.provider} options={shown.map((s) => ({ id: s.provider, label: s.title }))} onChange={(v) => view.set({ provider: v })} />;
  }
  return (
    <>
      <ViewHeader title="Traffic" range={false} scope={false} action={pick} />
      <div className={cx("relative min-h-0 flex-1 px-3 py-4 md:px-5 md:py-5", scrollArea)}>
        <div className="mx-auto max-w-6xl">{body}</div>
      </div>
    </>
  );
}

const toSettings = (label: string) => (
  <Link to={settingsPath} className={buttonClass({ variant: "primary" })}>
    <Icon name="plug" className="size-3.5" />
    {label}
  </Link>
);

/** No engine is connected and none has numbers kept. */
export function NoEngine({ admin }: { admin: boolean }) {
  return (
    <EmptyState icon="globe" title="No search engine is connected" action={admin ? toSettings("Connect a search engine") : undefined}>
      Google Search Console and Bing Webmaster Tools are free reports of how often your site shows up in their results and how many people click.{" "}
      {admin ? "Connect one or both to see that next to your AI results." : "Ask an admin to connect one, and you'll see that next to your AI results."}
    </EmptyState>
  );
}

/** Why there is nothing to show yet for one engine, and who can fix it. */
export function NotReady({ state, status: s, admin }: { state: Exclude<SearchState, "ready">; status: SearchStatus; admin: boolean }) {
  if (state === "disconnected") return <NoEngine admin={admin} />;
  if (state === "no_site")
    return (
      <EmptyState icon="globe" title="Pick your site" action={admin ? toSettings("Pick your site") : undefined}>
        {admin
          ? `${s.product} is connected. Pick which of its sites to read, and Byname starts copying its numbers within a minute.`
          : `${s.product} is connected, but nobody has picked which site to read yet. Ask an admin to pick one in Settings.`}
      </EmptyState>
    );
  if (state === "failed")
    return (
      <div className="space-y-3">
        <EmptyState icon="alert" title={`Byname couldn't read ${s.product}`} action={admin ? toSettings("Open search engine settings") : undefined}>
          {admin ? "Check the connection and the site in Settings. Byname tries again within the hour." : "Byname tries again within the hour. If it keeps failing, ask an admin to check the connection."}
        </EmptyState>
        <ErrorNote error={new Error(s.last_sync_error)} />
      </div>
    );
  return (
    <EmptyState icon="refresh" title={`Copying your numbers from ${s.title}`} action={<Spinner />}>
      Byname is copying {s.backfill ? `every day ${s.title} keeps, about ${s.backfill_months} months,` : s.backfill_months > 0 ? "the last 28 days" : `everything ${s.title} keeps`} for {siteName(s.site)}. This page fills in when it finishes.
    </EmptyState>
  );
}

function Ready({ status, canEdit, admin }: { status: SearchStatus; canEdit: boolean; admin: boolean }) {
  const view = useView();
  const span = spans.find((s) => s.id === view.get("span")) ?? spans[1];
  const from = spanFrom(status, span.days);
  return (
    <div className="space-y-4">
      <Notices status={status} admin={admin} />
      <GoalsCard status={status} canEdit={canEdit} />
      <Headline status={status} />
      <div className="flex flex-col gap-2 pt-2 sm:flex-row sm:items-center sm:justify-between">
        <h2 className="text-sm font-semibold">Over time</h2>
        <Segmented label="Chart span" value={span.id} options={spans.map((s) => ({ id: s.id, label: s.label }))} onChange={(v) => view.set({ span: v })} />
      </div>
      <DailyCard status={status} from={from} />
      <BrandCard status={status} from={from} canEdit={canEdit} />
      <div className="space-y-2 pt-2">
        <h2 className="text-sm font-semibold">Top pages and searches</h2>
        <FigureLegend engine={status.title} figures={["impressions", "ctr", "position"]} />
      </div>
      <TopPages status={status} />
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <TopQueries status={status} nonBrand={false} />
        <TopQueries status={status} nonBrand />
      </div>
    </div>
  );
}

/** What each search figure means, shown beside a table whose column heads have no room for it. */
export function FigureLegend({ engine, figures }: { engine: string; figures: Figure[] }) {
  const info = figureInfo(engine);
  return (
    <dl className="grid grid-cols-1 gap-x-4 gap-y-1 text-xs sm:grid-cols-2 lg:grid-cols-3">
      {figures.map((f) => (
        <div key={f}>
          <dt className="inline font-medium">{info[f].label}:</dt> <dd className="inline text-muted">{info[f].hint}</dd>
        </div>
      ))}
    </dl>
  );
}

export function Note({ tone, icon, children }: { tone: "info" | "warn" | "danger"; icon: "alert" | "refresh" | "note"; children: ReactNode }) {
  return (
    <div className={cx("flex items-start gap-2 rounded-surface px-3 py-2 text-xs", { info: "bg-info-soft text-info", warn: "bg-warn-soft text-warn", danger: "bg-danger-soft text-danger" }[tone])}>
      <Icon name={icon} className={cx("mt-px size-3.5", icon === "refresh" && "animate-spin")} />
      <p className="min-w-0 [overflow-wrap:anywhere]">{children}</p>
    </div>
  );
}

function Notices({ status: s, admin }: { status: SearchStatus; admin: boolean }) {
  return (
    <>
      {!s.connected && (
        <Note tone="warn" icon="alert">
          {s.product} is disconnected, so nothing new comes in. These are the numbers Byname already has for {siteName(s.data_site)}.{" "}
          {admin ? (
            <Link to={settingsPath} className="font-medium underline">
              Reconnect
            </Link>
          ) : (
            "Ask an admin to reconnect it."
          )}
        </Note>
      )}
      {s.connected && s.site && s.site !== s.data_site && (
        <Note tone="info" icon="note">
          Showing {siteName(s.data_site)} until Byname has copied {siteName(s.site)}.
        </Note>
      )}
      {s.syncing && (
        <Note tone="info" icon="refresh">
          Copying new numbers from {s.title} now. The page updates when it finishes.
        </Note>
      )}
      {s.last_sync_error && !s.syncing && (
        <Note tone="danger" icon="alert">
          Byname couldn&apos;t read {s.product} last time: {s.last_sync_error}
          {admin && (
            <>
              {" "}
              <Link to={settingsPath} className="font-medium underline">
                Check the connection
              </Link>
            </>
          )}
        </Note>
      )}
      <div className="space-y-1 text-xs text-muted">
        <p>
          Numbers for {siteName(s.data_site)} from {s.product}. Totals go up to {fullDay(s.latest_final_day)},{" "}
          {s.step_days > 1 ? `the end of the latest week ${s.title} has finished counting.` : `the latest day ${s.title} has finished counting.`}
          {s.partial_days.length > 0 && <> Days {s.title} is still counting are shaded on the daily chart, and no total includes them.</>}
          {s.last_sync_at && (
            <>
              {" "}
              Last synced <RelativeTime iso={s.last_sync_at} />.
            </>
          )}
        </p>
        {s.notes.length > 0 && (
          <details>
            <summary className="cursor-pointer py-3.5 font-medium hover:text-fg md:py-0">How {s.title} counts</summary>
            <ul className="mt-1 list-disc space-y-0.5 pl-5">
              {s.notes.map((n) => (
                <li key={n}>{n}</li>
              ))}
            </ul>
          </details>
        )}
      </div>
    </>
  );
}

function Headline({ status }: { status: SearchStatus }) {
  const summary = useSearchSummary(status);
  if (summary.isPending) return <Pending />;
  if (summary.isError) return <ErrorNote error={summary.error} />;
  const cur = summary.data.current;
  if (!cur) return null;
  const prev = summary.data.previous;
  const span = spanWords(cur.days, status.step_days);
  const figures = headline(status);
  return (
    <section aria-label={`Latest ${span}`}>
      <div className="mb-2 flex flex-wrap items-baseline justify-between gap-x-3">
        <h2 className="text-sm font-semibold">
          Latest {span}, {dayRange(cur.from, cur.to)}
        </h2>
        <span className="text-xs text-muted">{prev ? `Compared with ${dayRange(prev.from, prev.to)}` : `No earlier ${span} to compare with yet`}</span>
      </div>
      <div className={cx("grid grid-cols-2 gap-3 sm:grid-cols-3", figures.length === 5 ? "xl:grid-cols-5" : "xl:grid-cols-4")}>
        {figures.map((h) => (
          <HeadlineTile key={h.key} label={h.label} hint={h.hint} value={h.format(h.value(cur))} delta={<Change cur={cur} prev={prev} h={h} />} />
        ))}
      </div>
      {cur.days_with_data < cur.days && (
        <p className="mt-2 text-xs text-muted">
          Only {cur.days_with_data} of these {cur.days} days have data.
        </p>
      )}
    </section>
  );
}

function HeadlineTile({ label, hint, value, delta }: { label: string; hint: string; value: string; delta: ReactNode }) {
  return (
    <Card className="flex flex-col gap-1 px-3 py-3 sm:px-4">
      <h3 className="text-xs font-medium text-muted">{label}</h3>
      <p className="text-xs text-faint">{hint}</p>
      <span className="mt-auto text-2xl font-semibold">{value}</span>
      <div className="min-h-4 text-xs">{delta}</div>
    </Card>
  );
}

function Change({ cur, prev, h }: { cur: SearchPeriod; prev: SearchPeriod | null; h: HeadlineFigure }) {
  const c = change(h.value(cur), prev ? h.value(prev) : undefined, h.change, h.lowerIsBetter);
  if (!c) return <span className="text-faint">No comparison</span>;
  if (c.flat) return <span className="text-faint">No change</span>;
  const up = h.lowerIsBetter ? !c.good : c.good;
  return (
    <span className={cx("inline-flex items-center gap-0.5 font-medium", c.good ? "text-success" : "text-danger")}>
      <Icon name={up ? "up" : "down"} className="size-3" />
      {c.text} {h.lowerIsBetter ? (c.good ? "better" : "worse") : up ? "up" : "down"}
    </span>
  );
}

/** Site changes are Google's alone, and their markers fall on Google's Pacific days, so only Google's chart shows them. */
function DailyCard({ status, from }: { status: SearchStatus; from: string }) {
  const daily = useSearchDaily(status, from);
  const to = lastDay(status);
  const google = status.provider === "google";
  const markers = useQuery({ queryKey: keys.changeMarkers(from, to), queryFn: () => api.changes.markers(from, to), placeholderData: (p) => p, enabled: google });
  useLoading(google && markers.isPending);
  let body;
  if (daily.isPending) body = <Pending />;
  else if (daily.isError) body = <div className="px-4 pb-4"><ErrorNote error={daily.error} /></div>;
  else
    body = (
      <div className={cx(daily.isPlaceholderData && "opacity-60")}>
        <DailyChart engine={status.title} days={daily.data.days} from={from} latestFinal={daily.data.latest_final_day} markers={google ? markers.data : undefined} />
      </div>
    );
  return (
    <Card>
      <CardHeader title="Clicks and appearances per day" description="Each on its own scale. Appearances often rise and fall with how many people search, not with where you rank." />
      {body}
    </Card>
  );
}
