import type { ReactNode } from "react";
import { Link } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { api, type GscStatus, type SearchPeriod } from "../../api";
import { keys, useGsc, useMe } from "../../queries";
import { buttonClass, Card, CardHeader, cx, EmptyState, ErrorNote, Icon, Pending, RelativeTime, scrollArea, Spinner, useLoading } from "../../ui";
import { useView } from "../../view";
import { Segmented, ViewHeader } from "../results/Header";
import { BrandCard } from "./BrandCard";
import { DailyChart } from "./DailyChart";
import { GoalsCard } from "./Goals";
import { change, day, dayRange, figureInfo, fullDay, headline, lastDay, propertyName, searchState, spanFrom, spans, useSearchDaily, useSearchSummary, type Figure, type SearchState } from "./model";
import { TopPages, TopQueries } from "./Tops";

export const settingsPath = "/settings/search-console";

export function Search() {
  const gsc = useGsc();
  const me = useMe();
  const canEdit = !!me.data?.scopes.includes("config:write");
  const admin = me.data?.role === "admin";

  let body;
  if (gsc.isPending) body = <Pending />;
  else if (gsc.isError) body = <ErrorNote error={gsc.error} />;
  else {
    const state = searchState(gsc.data);
    body = state === "ready" ? <Ready status={gsc.data} canEdit={canEdit} admin={admin} /> : <NotReady state={state} status={gsc.data} admin={admin} />;
  }
  return (
    <>
      <ViewHeader
        title="Traffic"
        subtitle="Search Console is Google's free report of how often your site appears in Google results and how many people click. Google data arrives about 3 days late."
        range={false}
        scope={false}
      />
      <div className={cx("relative min-h-0 flex-1 px-3 py-4 md:px-5 md:py-5", scrollArea)}>
        <div className="mx-auto max-w-6xl">{body}</div>
      </div>
    </>
  );
}

/** Why there is nothing to show yet, and who can fix it. */
export function NotReady({ state, status, admin }: { state: Exclude<SearchState, "ready">; status: GscStatus; admin: boolean }) {
  const toSettings = (label: string) => (
    <Link to={settingsPath} className={buttonClass({ variant: "primary" })}>
      <Icon name="plug" className="size-3.5" />
      {label}
    </Link>
  );
  if (state === "disconnected")
    return (
      <EmptyState icon="globe" title="Search Console is not connected" action={admin ? toSettings("Connect Search Console") : undefined}>
        Search Console is Google&apos;s free report of how often your site shows up in Google results and how many people click. {admin ? "Connect it to see that next to your AI results." : "Ask an admin to connect it, and you'll see that next to your AI results."}
      </EmptyState>
    );
  if (state === "no_property")
    return (
      <EmptyState icon="globe" title="Pick your site" action={admin ? toSettings("Pick your site") : undefined}>
        {admin
          ? "Search Console is connected. Pick which of its sites to read, and Byname starts copying its numbers within a minute."
          : "Search Console is connected, but nobody has picked which site to read yet. Ask an admin to pick one in Settings."}
      </EmptyState>
    );
  if (state === "failed")
    return (
      <div className="space-y-3">
        <EmptyState icon="alert" title="Byname couldn't read Search Console" action={admin ? toSettings("Open Search Console settings") : undefined}>
          {admin ? "Check the connection and the site in Settings. Byname tries again tomorrow." : "Byname tries again tomorrow. If it keeps failing, ask an admin to check the connection."}
        </EmptyState>
        <ErrorNote error={new Error(status.last_sync_error)} />
      </div>
    );
  return (
    <EmptyState icon="refresh" title="Copying your numbers from Google" action={<Spinner />}>
      Byname is copying {status.backfill ? "every day Google keeps, about 16 months," : "the last 28 days"} for {propertyName(status.property)}. This page fills in when it finishes.
    </EmptyState>
  );
}

function Ready({ status, canEdit, admin }: { status: GscStatus; canEdit: boolean; admin: boolean }) {
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
        <FigureLegend figures={["impressions", "ctr", "position"]} />
      </div>
      <TopPages status={status} />
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <TopQueries status={status} nonBrand={false} />
        <TopQueries status={status} nonBrand />
      </div>
    </div>
  );
}

/** What each Search Console figure means, shown beside a table whose column heads have no room for it. */
export function FigureLegend({ figures }: { figures: Figure[] }) {
  return (
    <dl className="grid grid-cols-1 gap-x-4 gap-y-1 text-xs sm:grid-cols-2 lg:grid-cols-3">
      {figures.map((f) => (
        <div key={f}>
          <dt className="inline font-medium">{figureInfo[f].label}:</dt> <dd className="inline text-muted">{figureInfo[f].hint}</dd>
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

function Notices({ status: s, admin }: { status: GscStatus; admin: boolean }) {
  return (
    <>
      {!s.connected && (
        <Note tone="warn" icon="alert">
          Search Console is disconnected, so nothing new comes in. These are the numbers Byname already has for {propertyName(s.data_property)}.{" "}
          {admin ? (
            <Link to={settingsPath} className="font-medium underline">
              Reconnect
            </Link>
          ) : (
            "Ask an admin to reconnect it."
          )}
        </Note>
      )}
      {s.connected && s.property && s.property !== s.data_property && (
        <Note tone="info" icon="note">
          Showing {propertyName(s.data_property)} until Byname has copied {propertyName(s.property)}.
        </Note>
      )}
      {s.syncing && (
        <Note tone="info" icon="refresh">
          Copying new numbers from Google now. The page updates when it finishes.
        </Note>
      )}
      {s.last_sync_error && !s.syncing && (
        <Note tone="danger" icon="alert">
          Byname couldn&apos;t read Search Console last time: {s.last_sync_error}
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
      <p className="text-xs text-muted">
        Numbers for {propertyName(s.data_property)}. Totals go up to {fullDay(s.latest_final_day)}, the latest day Google has finished counting.
        {s.partial_days.length > 0 && <> Days after {day(s.latest_final_day)} are still being counted by Google, so the daily chart shades them and no total includes them.</>}
        {s.last_sync_at && (
          <>
            {" "}
            Last synced <RelativeTime iso={s.last_sync_at} />.
          </>
        )}
      </p>
    </>
  );
}

function Headline({ status }: { status: GscStatus }) {
  const summary = useSearchSummary(status);
  if (summary.isPending) return <Pending />;
  if (summary.isError) return <ErrorNote error={summary.error} />;
  const cur = summary.data.current;
  if (!cur) return null;
  const prev = summary.data.previous;
  return (
    <section aria-label="Latest 28 days">
      <div className="mb-2 flex flex-wrap items-baseline justify-between gap-x-3">
        <h2 className="text-sm font-semibold">Latest 28 days, {dayRange(cur.from, cur.to)}</h2>
        <span className="text-xs text-muted">{prev ? `Compared with ${dayRange(prev.from, prev.to)}` : "No earlier 28 days to compare with yet"}</span>
      </div>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-5">
        {headline.map((h) => (
          <HeadlineTile key={h.key} label={h.label} hint={h.hint} value={h.format(h.value(cur))} delta={<Change cur={cur} prev={prev} h={h} />} />
        ))}
      </div>
      {cur.days_with_data < 28 && <p className="mt-2 text-xs text-muted">Only {cur.days_with_data} of these 28 days have data.</p>}
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

function Change({ cur, prev, h }: { cur: SearchPeriod; prev: SearchPeriod | null; h: (typeof headline)[number] }) {
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

function DailyCard({ status, from }: { status: GscStatus; from: string }) {
  const daily = useSearchDaily(status, from);
  const to = lastDay(status);
  const markers = useQuery({ queryKey: keys.changeMarkers(from, to), queryFn: () => api.changes.markers(from, to), placeholderData: (p) => p });
  useLoading(markers.isPending);
  let body;
  if (daily.isPending) body = <Pending />;
  else if (daily.isError) body = <div className="px-4 pb-4"><ErrorNote error={daily.error} /></div>;
  else
    body = (
      <div className={cx(daily.isPlaceholderData && "opacity-60")}>
        <DailyChart days={daily.data.days} from={from} latestFinal={daily.data.latest_final_day} markers={markers.data} />
      </div>
    );
  return (
    <Card>
      <CardHeader title="Clicks and appearances per day" description="Each on its own scale. Appearances often rise and fall with how many people search, not with where you rank." />
      {body}
    </Card>
  );
}
