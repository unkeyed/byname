import type { Day, SearchDay } from "../../api";
import { LineChart, niceTicks, type ChartMarker } from "../../charts";
import { ScrollX } from "../../ui";
import { compact, count, ctr, day, fillDays, longDay, position } from "./model";

type Measure = "clicks" | "impressions";
const measures: Record<Measure, { label: string; color: string }> = {
  clicks: { label: "Clicks", color: "var(--series-1)" },
  impressions: { label: "Impressions", color: "var(--series-7)" },
};

/** A dated event to mark on the chart, such as a merged change. */
export interface DayMarker {
  date: Day;
  label: string;
}

/**
 * Daily clicks and impressions as small multiples on their own scales,
 * since one axis cannot hold both. Days Google still revises are dashed on
 * a shaded band.
 */
export function DailyChart({
  days: raw,
  from,
  latestFinal,
  show = ["clicks", "impressions"],
  markers = [],
  height = 170,
}: {
  days: SearchDay[];
  from: Day;
  latestFinal: Day;
  show?: Measure[];
  markers?: DayMarker[];
  height?: number;
}) {
  const days = fillDays(raw, from, latestFinal);
  if (days.length === 0) return <p className="px-4 pb-6 text-sm text-muted">No days in this range.</p>;
  const xs = days.map((d) => ({ key: d.date, label: day(d.date), title: longDay(d.date) }));
  const firstPartial = days.findIndex((d) => !d.final);
  const at = new Map(days.map((d, i) => [d.date, i]));
  const chartMarkers: ChartMarker[] = markers.flatMap((m) => (at.has(m.date) ? [{ at: at.get(m.date)!, label: m.label }] : []));
  return (
    <div className="space-y-4 px-4 pb-4">
      {show.map((m) => {
        const values = days.map((d) => d[m]);
        const total = days.filter((d) => d.final).reduce((s, d) => s + d[m], 0);
        return (
          <div key={m}>
            <div className="mb-1 flex items-baseline justify-between gap-2 text-xs">
              <span className="font-medium">{measures[m].label} per day</span>
              <span className="text-muted tabular-nums">
                {count(total)} over the final days
              </span>
            </div>
            <LineChart
              xs={xs}
              lines={[{ key: m, label: measures[m].label, color: measures[m].color, approx: false, values }]}
              ticks={niceTicks(Math.max(...values))}
              format={compact}
              height={height}
              partialFrom={firstPartial >= 0 ? firstPartial : undefined}
              partialNote="Partial day: Google may still revise it"
              markers={chartMarkers}
              label={`${measures[m].label} per day, ${day(days[0].date)} to ${day(days[days.length - 1].date)}`}
            />
          </div>
        );
      })}
      <details className="text-xs">
        <summary className="cursor-pointer text-muted hover:text-fg">Show as a table</summary>
        <div className="mt-2 max-h-72 overflow-y-auto">
          <ScrollX label="daily table">
            <table className="w-full min-w-max text-left tabular-nums">
              <thead className="sticky top-0 bg-surface text-muted">
                <tr>
                  <th className="py-1 pr-4 font-medium">Day</th>
                  <th className="py-1 pr-4 text-right font-medium">Clicks</th>
                  <th className="py-1 pr-4 text-right font-medium">Impressions</th>
                  <th className="py-1 pr-4 text-right font-medium">CTR</th>
                  <th className="py-1 pr-4 text-right font-medium">Position</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {[...days].reverse().map((d) => (
                  <tr key={d.date}>
                    <td className="py-1 pr-4">
                      {longDay(d.date)}
                      {!d.final && <span className="text-warn"> (partial)</span>}
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
