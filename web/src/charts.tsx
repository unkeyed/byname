import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { cx } from "./ui";

export function useWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setWidth(Math.floor(e.contentRect.width)));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, width] as const;
}

/** A line's key: a short stroke of its colour, dashed for an approximation, as in the chart. */
export function LineKey({ color, dashed, className }: { color: string; dashed?: boolean; className?: string }) {
  return (
    <svg viewBox="0 0 16 4" className={cx("h-1 w-4 shrink-0", className)} aria-hidden="true">
      <line x1="1" y1="2" x2="15" y2="2" stroke={color} strokeWidth="2.5" strokeLinecap="round" strokeDasharray={dashed ? "3 3" : undefined} />
    </svg>
  );
}

export interface ChartLine {
  key: string;
  label: string;
  color: string;
  /** Drawn dashed and named as an approximation. */
  approx: boolean;
  /** One per x, null where the line has no value. */
  values: (number | null)[];
}

export interface ChartX {
  key: string;
  label: string;
  title: string;
}


/** An event on the x axis, such as a merged change, drawn as a hairline with a flag. */
export interface ChartMarker {
  /** The index into xs. */
  at: number;
  label: string;
}

const M = { top: 10, right: 14, bottom: 24, left: 40 };
const rateTicks = [0, 0.25, 0.5, 0.75, 1];

/** Round steps from 0 that cover max, for counts. */
export function niceTicks(max: number, count = 4): number[] {
  if (!(max > 0)) return [0, 1];
  const raw = max / count;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = ([1, 2, 2.5, 5, 10].find((s) => s * mag >= raw) ?? 10) * mag;
  const out: number[] = [];
  for (let t = 0; t < max + step * 1e-9; t += step) out.push(t);
  if (out[out.length - 1] < max) out.push(out[out.length - 1] + step);
  return out;
}

/**
 * Lines over an x axis of runs or days, on rates from 0 to 1 unless ticks
 * set the scale. A crosshair snaps to the nearest x and the tooltip lists
 * every line there; arrow keys move it too. Values from partialFrom on are
 * provisional: drawn dashed and faded on a shaded band.
 */
export function LineChart({
  xs,
  lines,
  format,
  height = 240,
  label,
  ticks = rateTicks,
  partialFrom,
  partialNote = "Partial: may still change",
  markers = [],
}: {
  xs: ChartX[];
  lines: ChartLine[];
  format: (v: number) => string;
  height?: number;
  label: string;
  ticks?: number[];
  partialFrom?: number;
  partialNote?: string;
  markers?: ChartMarker[];
}) {
  const [ref, width] = useWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const w = Math.max(width, 200);
  const iw = w - M.left - M.right;
  const ih = height - M.top - M.bottom;
  const lo = ticks[0];
  const hi = ticks[ticks.length - 1];
  const x = (i: number) => M.left + (xs.length === 1 ? iw / 2 : (i / (xs.length - 1)) * iw);
  const y = (v: number) => M.top + (1 - (v - lo) / (hi - lo || 1)) * ih;
  const partial = partialFrom != null && partialFrom < xs.length ? partialFrom : null;

  const path = (values: (number | null)[], from = 0, to = values.length - 1) => {
    let d = "";
    let pen = false;
    values.forEach((v, i) => {
      if (v == null || i < from || i > to) {
        pen = false;
        return;
      }
      d += `${pen ? "L" : "M"}${x(i).toFixed(1)},${y(v).toFixed(1)}`;
      pen = true;
    });
    return d;
  };

  const pick = (clientX: number, el: Element) => {
    const left = el.getBoundingClientRect().left;
    const rel = clientX - left - M.left;
    const i = xs.length === 1 ? 0 : Math.round((rel / iw) * (xs.length - 1));
    setHover(Math.max(0, Math.min(xs.length - 1, i)));
  };

  const tickEvery = Math.max(1, Math.ceil(xs.length / Math.max(2, Math.floor(iw / 72))));
  const rows = hover == null ? [] : lines.filter((l) => l.values[hover] != null).sort((a, b) => (b.values[hover] ?? 0) - (a.values[hover] ?? 0));
  const tipLeft = hover == null ? 0 : x(hover);
  const bandLeft = partial == null ? 0 : partial === 0 ? M.left : (x(partial - 1) + x(partial)) / 2;
  const here = hover == null ? [] : markers.filter((m) => m.at === hover);

  return (
    <div ref={ref} className="relative">
      <svg
        width={w}
        height={height}
        role="img"
        aria-label={label}
        tabIndex={0}
        className="block touch-pan-y outline-none focus-visible:outline-2 focus-visible:outline-ring"
        onPointerMove={(e) => pick(e.clientX, e.currentTarget)}
        onPointerLeave={() => setHover(null)}
        onBlur={() => setHover(null)}
        onKeyDown={(e) => {
          if (e.key === "ArrowRight") setHover((h) => Math.min(xs.length - 1, (h ?? -1) + 1));
          else if (e.key === "ArrowLeft") setHover((h) => Math.max(0, (h ?? xs.length) - 1));
          else if (e.key === "Escape") setHover(null);
          else return;
          e.preventDefault();
        }}
      >
        {partial != null && (
          <g>
            <rect x={bandLeft} y={M.top} width={w - M.right - bandLeft} height={ih} fill="var(--subtle)" />
            {w - M.right - bandLeft >= 44 && (
              <text x={w - M.right - 4} y={M.top + 10} textAnchor="end" className="fill-faint text-2xs">
                Partial
              </text>
            )}
          </g>
        )}
        {ticks.map((t) => (
          <g key={t}>
            <line x1={M.left} x2={w - M.right} y1={y(t)} y2={y(t)} stroke={t === lo ? "var(--line-strong)" : "var(--line)"} strokeWidth="1" />
            <text x={M.left - 8} y={y(t)} dy="0.32em" textAnchor="end" className="fill-faint text-2xs tabular-nums">
              {format(t)}
            </text>
          </g>
        ))}
        {xs.map((p, i) =>
          i % tickEvery === 0 || i === xs.length - 1 ? (
            <text key={p.key} x={x(i)} y={height - 6} textAnchor={xs.length === 1 ? "middle" : i === 0 ? "start" : i === xs.length - 1 ? "end" : "middle"} className="fill-faint text-2xs">
              {p.label}
            </text>
          ) : null,
        )}
        {markers.map((m) => (
          <g key={`${m.at}-${m.label}`}>
            <line x1={x(m.at)} x2={x(m.at)} y1={M.top} y2={M.top + ih} stroke="var(--muted)" strokeOpacity="0.6" strokeWidth="1" />
            <circle cx={x(m.at)} cy={M.top} r="3.5" fill="var(--muted)" stroke="var(--surface)" strokeWidth="1.5">
              <title>{m.label}</title>
            </circle>
          </g>
        ))}
        {hover != null && <line x1={tipLeft} x2={tipLeft} y1={M.top} y2={M.top + ih} stroke="var(--line-hover)" strokeWidth="1" />}
        {lines.map((l) => {
          const last = l.values.reduce<number>((at, v, i) => (v == null ? at : i), -1);
          const dash = l.approx ? "5 4" : undefined;
          return (
            <g key={l.key}>
              {partial == null ? (
                <path d={path(l.values)} fill="none" stroke={l.color} strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" strokeDasharray={dash} />
              ) : (
                <>
                  <path d={path(l.values, 0, partial - 1)} fill="none" stroke={l.color} strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" strokeDasharray={dash} />
                  <path d={path(l.values, Math.max(0, partial - 1))} fill="none" stroke={l.color} strokeOpacity="0.55" strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" strokeDasharray="2 4" />
                </>
              )}
              {l.values.map((v, i) => {
                const isolated = v != null && l.values[i - 1] == null && l.values[i + 1] == null;
                return v != null && (i === last || isolated || i === hover) ? (
                  <circle key={i} cx={x(i)} cy={y(v)} r="4" fill={l.color} fillOpacity={partial != null && i >= partial ? 0.55 : 1} stroke="var(--surface)" strokeWidth="2" />
                ) : null;
              })}
            </g>
          );
        })}
      </svg>
      {hover != null && (rows.length > 0 || here.length > 0) && (
        <div
          role="status"
          className="pointer-events-none absolute top-1 z-10 max-w-64 min-w-44 rounded-surface bg-surface px-3 py-2 text-xs shadow-floating"
          style={tipLeft > w / 2 ? { right: w - tipLeft + 12 } : { left: tipLeft + 12 }}
        >
          <div className="mb-1.5 text-muted">{xs[hover].title}</div>
          <ul className="space-y-1">
            {rows.map((l) => (
              <li key={l.key} className="flex items-center gap-2">
                <LineKey color={l.color} dashed={l.approx} />
                <span className="min-w-10 font-semibold tabular-nums">{format(l.values[hover]!)}</span>
                <span className="min-w-0 truncate text-muted">
                  {l.label}
                  {l.approx && " (approx.)"}
                </span>
              </li>
            ))}
          </ul>
          {partial != null && hover >= partial && <div className="mt-1.5 text-warn">{partialNote}</div>}
          {here.map((m) => (
            <div key={m.label} className="mt-1.5 flex items-start gap-1.5 text-fg">
              <span className="mt-1 size-2 shrink-0 rounded-full bg-muted" aria-hidden="true" />
              <span className="min-w-0 [overflow-wrap:anywhere]">{m.label}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export function Sparkline({ values, className }: { values: (number | null)[]; className?: string }) {
  const w = 96;
  const h = 28;
  const pts = values.map((v, i) => [values.length === 1 ? w / 2 : 3 + (i / (values.length - 1)) * (w - 6), v] as const).filter((p): p is readonly [number, number] => p[1] != null);
  if (pts.length === 0) return null;
  const lo = Math.min(...pts.map((p) => p[1]));
  const hi = Math.max(...pts.map((p) => p[1]));
  const yy = (v: number) => (hi === lo ? h / 2 : 3 + (1 - (v - lo) / (hi - lo)) * (h - 6));
  const [lx, lv] = pts[pts.length - 1];
  return (
    <svg viewBox={`0 0 ${w} ${h}`} width={w} height={h} className={className} aria-hidden="true">
      <polyline points={pts.map(([px, v]) => `${px.toFixed(1)},${yy(v).toFixed(1)}`).join(" ")} fill="none" stroke="var(--line-hover)" strokeWidth="1.5" strokeLinejoin="round" strokeLinecap="round" />
      <circle cx={lx} cy={yy(lv)} r="3" fill="var(--you)" stroke="var(--surface)" strokeWidth="1.5" />
    </svg>
  );
}

export interface Segment {
  key: string;
  label: string;
  value: number;
  color: string;
}

/** Part to whole at a glance; the list beside it carries the figures. */
export function Donut({ segments, size = 168, children, label }: { segments: Segment[]; size?: number; children?: ReactNode; label: string }) {
  const total = segments.reduce((s, x) => s + x.value, 0);
  const r = size / 2;
  const inner = r * 0.64;
  let angle = -Math.PI / 2;
  const arcs = segments
    .filter((s) => s.value > 0)
    .map((s) => {
      const sweep = total ? (s.value / total) * Math.PI * 2 : 0;
      const a0 = angle;
      const a1 = angle + sweep;
      angle = a1;
      const large = sweep > Math.PI ? 1 : 0;
      const pt = (a: number, rad: number) => `${(r + rad * Math.cos(a)).toFixed(2)},${(r + rad * Math.sin(a)).toFixed(2)}`;
      const d =
        sweep >= Math.PI * 2 - 1e-6
          ? `M${pt(0, r)}A${r},${r} 0 1 1 ${pt(Math.PI, r)}A${r},${r} 0 1 1 ${pt(0, r)}M${pt(0, inner)}A${inner},${inner} 0 1 0 ${pt(Math.PI, inner)}A${inner},${inner} 0 1 0 ${pt(0, inner)}Z`
          : `M${pt(a0, r)}A${r},${r} 0 ${large} 1 ${pt(a1, r)}L${pt(a1, inner)}A${inner},${inner} 0 ${large} 0 ${pt(a0, inner)}Z`;
      return { ...s, d };
    });
  return (
    <div className="relative shrink-0" style={{ width: size, height: size }}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} role="img" aria-label={label}>
        {total === 0 && <circle cx={r} cy={r} r={(r + inner) / 2} fill="none" stroke="var(--line)" strokeWidth={r - inner} />}
        {arcs.map((a) => (
          <path key={a.key} d={a.d} fill={a.color} stroke="var(--surface)" strokeWidth="2" fillRule="evenodd">
            <title>{a.label}</title>
          </path>
        ))}
      </svg>
      {children && <div className="absolute inset-0 grid place-items-center text-center">{children}</div>}
    </div>
  );
}
