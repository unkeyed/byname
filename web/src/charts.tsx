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

const M = { top: 10, right: 14, bottom: 24, left: 40 };
const yTicks = [0, 0.25, 0.5, 0.75, 1];

/**
 * Rates from 0 to 1 plotted by run. A crosshair snaps to the nearest run
 * and the tooltip lists every line there; arrow keys move it too.
 */
export function LineChart({ xs, lines, format, height = 240, label }: { xs: ChartX[]; lines: ChartLine[]; format: (v: number) => string; height?: number; label: string }) {
  const [ref, width] = useWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const w = Math.max(width, 200);
  const iw = w - M.left - M.right;
  const ih = height - M.top - M.bottom;
  const x = (i: number) => M.left + (xs.length === 1 ? iw / 2 : (i / (xs.length - 1)) * iw);
  const y = (v: number) => M.top + (1 - v) * ih;

  const path = (values: (number | null)[]) => {
    let d = "";
    let pen = false;
    values.forEach((v, i) => {
      if (v == null) {
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
        {yTicks.map((t) => (
          <g key={t}>
            <line x1={M.left} x2={w - M.right} y1={y(t)} y2={y(t)} stroke={t === 0 ? "var(--line-strong)" : "var(--line)"} strokeWidth="1" />
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
        {hover != null && <line x1={tipLeft} x2={tipLeft} y1={M.top} y2={M.top + ih} stroke="var(--line-hover)" strokeWidth="1" />}
        {lines.map((l) => {
          const last = l.values.reduce<number>((at, v, i) => (v == null ? at : i), -1);
          return (
          <g key={l.key}>
            <path d={path(l.values)} fill="none" stroke={l.color} strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" strokeDasharray={l.approx ? "5 4" : undefined} />
            {l.values.map((v, i) => {
              const isolated = v != null && l.values[i - 1] == null && l.values[i + 1] == null;
              return v != null && (i === last || isolated || i === hover) ? (
                <circle key={i} cx={x(i)} cy={y(v)} r="4" fill={l.color} stroke="var(--surface)" strokeWidth="2" />
              ) : null;
            })}
          </g>
          );
        })}
      </svg>
      {hover != null && rows.length > 0 && (
        <div
          role="status"
          className="pointer-events-none absolute top-1 z-10 min-w-44 rounded-surface bg-surface px-3 py-2 text-xs shadow-floating"
          style={tipLeft > w / 2 ? { right: w - tipLeft + 12 } : { left: tipLeft + 12 }}
        >
          <div className="mb-1.5 text-muted">{xs[hover].title}</div>
          <ul className="space-y-1">
            {rows.map((l) => (
              <li key={l.key} className="flex items-center gap-2">
                <LineKey color={l.color} dashed={l.approx} />
                <span className="w-10 font-semibold tabular-nums">{format(l.values[hover]!)}</span>
                <span className="min-w-0 truncate text-muted">
                  {l.label}
                  {l.approx && " (approx.)"}
                </span>
              </li>
            ))}
          </ul>
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
      <circle cx={lx} cy={yy(lv)} r="3" fill="var(--series-1)" stroke="var(--surface)" strokeWidth="1.5" />
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
