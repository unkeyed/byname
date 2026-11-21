import type { ReactNode } from "react";
import { useSurfaces } from "../../queries";
import { cx, PageHeader, selectClass } from "../../ui";
import { ranges, useView } from "../../view";

export function Segmented<T extends string>({ label, value, options, onChange, size = "md" }: { label: string; value: T; options: { id: T; label: string }[]; onChange: (v: T) => void; size?: "sm" | "md" }) {
  return (
    <div role="radiogroup" aria-label={label} className="flex shrink-0 rounded-control border border-line-strong bg-surface p-0.5">
      {options.map((o) => (
        <button
          key={o.id}
          type="button"
          role="radio"
          aria-checked={value === o.id}
          onClick={() => onChange(o.id)}
          className={cx(
            "rounded-inner font-medium whitespace-nowrap transition-colors",
            size === "sm" ? "h-6 px-2 text-xs" : "h-7 px-2.5 text-xs",
            value === o.id ? "bg-subtle-strong text-fg" : "text-muted hover:text-fg",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

/**
 * The header every results page shares: the title in the top bar with the
 * page's primary action, then the subtitle and the filters that scope
 * everything below. scope false hides the surface and model filters on
 * pages they do not apply to.
 */
export function ViewHeader({ title, subtitle, action, range = true, scope = true }: { title: string; subtitle: ReactNode; action?: ReactNode; range?: boolean; scope?: boolean }) {
  const view = useView();
  const surfaces = useSurfaces();
  const used = surfaces.data?.filter((s) => s.pairs.length > 0) ?? [];
  const models = [...new Set(used.flatMap((s) => s.pairs.map((p) => p.model)).filter(Boolean))].sort();
  const modelValue = view.get("model") || (view.get("by") === "model" ? "_by" : "");
  return (
    <>
      <PageHeader title={title}>{action}</PageHeader>
      <div className="shrink-0 border-b border-line bg-bg px-3 py-3 md:px-5">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-center">
          <p className="min-w-0 flex-1 text-sm text-muted">{subtitle}</p>
          {(range || scope) && (
            <div className="-mx-3 flex items-center gap-2 overflow-x-auto px-3 pb-0.5 md:mx-0 md:flex-wrap md:overflow-visible md:px-0">
              {range && <Segmented label="Date range" value={view.get("range")} options={ranges.map((r) => ({ id: r.id, label: r.label }))} onChange={(v) => view.set({ range: v })} />}
              {scope && (
                <>
                  <select aria-label="Surface" className={cx(selectClass, "h-8 shrink-0 text-xs")} value={view.get("surface")} onChange={(e) => view.set({ surface: e.target.value })}>
                    <option value="">All surfaces</option>
                    {used.map((s) => (
                      <option key={s.key} value={s.key}>
                        {s.title}
                      </option>
                    ))}
                  </select>
                  <select
                    aria-label="Model"
                    className={cx(selectClass, "h-8 shrink-0 text-xs")}
                    value={modelValue}
                    onChange={(e) => {
                      const v = e.target.value;
                      view.set(v === "_by" ? { by: "model", model: null } : v ? { by: "model", model: v } : { by: null, model: null });
                    }}
                  >
                    <option value="">All models, by surface</option>
                    <option value="_by">By model</option>
                    {models.length > 0 && (
                      <optgroup label="One model">
                        {models.map((m) => (
                          <option key={m} value={m}>
                            {m}
                          </option>
                        ))}
                      </optgroup>
                    )}
                  </select>
                </>
              )}
            </div>
          )}
        </div>
      </div>
    </>
  );
}
