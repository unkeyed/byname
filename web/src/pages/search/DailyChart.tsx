import type { Day, SearchDay } from "../../api";
import { LineChart, LineKey, niceTicks, type ChartMarker } from "../../charts";
import { cx, scrollArea, ScrollX } from "../../ui";
import { compact, count, ctr, day, figureInfo, fillDays, longDay, position } from "./model";

type Measure = "clicks" | "impressions";
const colors: Record<Measure, string> = { clicks: "var(--series-1)", impressions: "var(--series-7)" };

/** A dated event to mark on the chart, such as a merged change. */
export interface DayMarker {
  date: Day;
  label: string;
}

/**
 * Daily clicks and appearances from engine, such as Google, as small
 * multiples on their own scales, since one axis cannot hold both. Days the
 * engine is still counting are dotted on a shaded band.
 */
export function DailyChart({
  engine,
  days: raw,
  from,
  latestFinal,
  show = ["clicks", "impressions"],
  markers = [],
  height = 170,
}: {
  engine: string;
  days: SearchDay[];
  from: Day;
  latestFinal: Day;
  show?: Measure[];
  markers?: DayMarker[];
  height?: number;
}) {
  const days = fillDays(raw, from, latestFinal);
  const info = figureInfo(engine);
  if (days.length === 0) return <p className="px-4 pb-6 text-sm text-muted">No days in this range.</p>;
  const xs = days.map((d) => ({ key: d.date, label: day(d.date), title: longDay(d.date) }));
  const firstPartial = days.findIndex((d) => !d.final);
  const at = new Map(days.map((d, i) => [d.date, i]));
  const chartMarkers: ChartMarker[] = markers.flatMap((m) => (at.has(m.date) ? [{ at: at.get(m.date)!, label: m.label }] : []));
  return (
    <div className="space-y-4 px-4 pb-4">
      {show.map((m) => {
        const values = days.map((d) => d[m]);
        const measure = { ...info[m], color: colors[m] };
        const total = days.filter((d) => d.final).reduce((s, d) => s + d[m], 0);
        return (
          <div key={m}>
            <div className="mb-1 flex flex-wrap items-baseline justify-between gap-x-2 text-xs">
              <span>
                <span className="font-medium">{measure.label} per day</span> <span className="text-faint">{measure.hint}</span>
              </span>
              <span className="text-muted tabular-nums">{count(total)} in total, counted days only</span>
            </div>
            <LineChart
              xs={xs}
              lines={[{ key: m, label: measure.label, color: measure.color, approx: false, values }]}
              ticks={niceTicks(Math.max(...values))}
              format={compact}
              height={height}
              partialFrom={firstPartial >= 0 ? firstPartial : undefined}
              partialNote={`${engine} is still counting this day`}
              markers={chartMarkers}
              label={`${measure.label} per day, ${day(days[0].date)} to ${day(days[days.length - 1].date)}`}
            />
          </div>
        );
      })}
      {(firstPartial >= 0 || chartMarkers.length > 0) && (
        <ul aria-label="Chart key" className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
          {firstPartial >= 0 && (
            <li className="flex items-center gap-1.5">
              <LineKey color="var(--muted)" dashed />
              Dotted on grey: days {engine} is still counting
            </li>
          )}
          {chartMarkers.length > 0 && (
            <li className="flex items-center gap-1.5">
              <svg viewBox="0 0 8 8" className="size-2 shrink-0" aria-hidden="true">
                <circle cx="4" cy="4" r="3.5" fill="var(--muted)" />
              </svg>
              Line with a dot: a site change went live
            </li>
          )}
        </ul>
      )}
      <details className="text-xs">
        <summary className="cursor-pointer py-3.5 text-muted hover:text-fg md:py-0">Show as a table</summary>
        <div className={cx("mt-2 max-h-72", scrollArea)}>
          <ScrollX label="daily table">
            <table className="w-full min-w-max text-left tabular-nums">
              <thead className="sticky top-0 bg-surface text-muted">
                <tr>
                  <th className="py-1 pr-4 font-medium">Day</th>
                  <th className="py-1 pr-4 text-right font-medium">Clicks</th>
                  <th className="py-1 pr-4 text-right font-medium">{info.impressions.label}</th>
                  <th className="py-1 pr-4 text-right font-medium">{info.ctr.label}</th>
                  <th className="py-1 pr-4 text-right font-medium">{info.position.label}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {[...days].reverse().map((d) => (
                  <tr key={d.date}>
                    <td className="py-1 pr-4">
                      {longDay(d.date)}
                      {!d.final && <span className="text-warn"> (still being counted)</span>}
                    </td>
                    <td className="py-1 pr-4 text-right">{count(d.clicks)}</td>
                    <td className="py-1 pr-4 text-right">{count(d.impressions)}</td>
                    <td className="py-1 pr-4 text-right">{ctr(d.ctr)}</td>
                    <td className="py-1 pr-4 text-right">{position(d.position)}</td>
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
