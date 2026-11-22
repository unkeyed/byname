import type { ReactNode } from "react";
import { useSurfaces } from "../../queries";
import { cx, PageHeader, selectClass, useLoading } from "../../ui";
import { ranges, useView } from "../../view";
import { SectionTabs, useSection } from "../../nav";

export function Segmented<T extends string>({
  label,
  value,
  options,
  onChange,
  size = "md",
  className,
}: {
  label: string;
  value: T;
  options: { id: T; label: string }[];
  onChange: (v: T) => void;
  size?: "sm" | "md";
  className?: string;
}) {
  return (
    <div role="radiogroup" aria-label={label} className={cx("flex shrink-0 rounded-control border border-line-strong bg-surface p-0.5", className)}>
      {options.map((o) => (
        <button
          key={o.id}
          type="button"
          role="radio"
          aria-checked={value === o.id}
          onClick={() => onChange(o.id)}
          className={cx(
            "rounded-inner font-medium whitespace-nowrap transition-colors",
            size === "sm" ? "h-11 min-w-11 px-2 text-xs md:h-6 md:min-w-0" : "h-11 min-w-11 px-2.5 text-xs md:h-7 md:min-w-0",
            value === o.id ? "bg-subtle-strong text-fg" : "text-muted hover:text-fg",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

const filterSelect = cx(selectClass, "w-full text-xs md:w-48");

/**
 * The header every results page shares: the title in the top bar with the
 * page's primary action, the section's tabs when the page is one, then the
 * subtitle and, on a row of their own, the filters that scope everything
 * below. Inside a section the top bar shows the section's name, and the tab
 * row says which page this is. On a phone the filters are selects in a grid,
 * so every one stays on screen. scope false hides the AI app and model
 * filters on pages they do not apply to.
 */
export function ViewHeader({ title, subtitle, action, range = true, scope = true }: { title: string; subtitle: ReactNode; action?: ReactNode; range?: boolean; scope?: boolean }) {
  const view = useView();
  const section = useSection();
  const surfaces = useSurfaces();
  useLoading(scope && surfaces.isPending);
  const used = surfaces.data?.filter((s) => s.pairs.length > 0) ?? [];
  const models = [...new Set(used.flatMap((s) => s.pairs.map((p) => p.model)).filter(Boolean))].sort();
  const modelValue = view.get("model") || (view.get("by") === "model" ? "_by" : "");
  const rangeOptions = ranges.map((r) => ({ id: r.id, label: r.label }));
  return (
    <>
      <PageHeader title={section?.label ?? title}>{action}</PageHeader>
      {section?.tabs && <SectionTabs item={section} />}
      <div className="shrink-0 space-y-3 border-b border-line bg-bg px-3 py-3 md:px-5">
        <p className="max-w-3xl text-sm text-muted">{subtitle}</p>
        {(range || scope) && (
          <div className="grid grid-cols-2 gap-2 md:flex md:flex-wrap md:items-center">
            {range && (
              <>
                <Segmented className="hidden md:flex" label="Date range" value={view.get("range")} options={rangeOptions} onChange={(v) => view.set({ range: v })} />
                <select aria-label="Date range" className={cx(filterSelect, "md:hidden")} value={view.get("range")} onChange={(e) => view.set({ range: e.target.value })}>
                  {rangeOptions.map((r) => (
                    <option key={r.id} value={r.id}>
                      {r.label}
                    </option>
                  ))}
                </select>
              </>
            )}
            {scope && (
              <>
                <select aria-label="AI app" className={filterSelect} value={view.get("surface")} onChange={(e) => view.set({ surface: e.target.value })}>
                  <option value="">All AI apps</option>
                  {used.map((s) => (
                    <option key={s.key} value={s.key}>
                      {s.title}
                    </option>
                  ))}
                </select>
                <select
                  aria-label="Model"
                  className={cx(filterSelect, "col-span-2")}
                  value={modelValue}
                  onChange={(e) => {
                    const v = e.target.value;
                    view.set(v === "_by" ? { by: "model", model: null } : v ? { by: "model", model: v } : { by: null, model: null });
                  }}
                >
                  <option value="">All models, by AI app</option>
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
    </>
  );
}
