import { cx } from "../../ui";
import { useView } from "../../view";

/** A page's tabs, kept in ?tab= so a tab can be linked. The first tab is the default. */
export function useTab<T extends string>(tabs: readonly { id: T }[]): T {
  const v = useView().get("tab");
  return (tabs.find((t) => t.id === v) ?? tabs[0]).id;
}

export function Tabs<T extends string>({ label, tabs, current }: { label: string; tabs: readonly { id: T; label: string; count?: number }[]; current: T }) {
  const view = useView();
  return (
    <div role="tablist" aria-label={label} className="flex gap-4 border-b border-line">
      {tabs.map((t, i) => (
        <button
          key={t.id}
          type="button"
          role="tab"
          aria-selected={current === t.id}
          onClick={() => view.set({ tab: i === 0 ? null : t.id })}
          className={cx(
            "-mb-px inline-flex h-9 items-center gap-1.5 border-b-2 text-sm font-medium transition-colors",
            current === t.id ? "border-fg text-fg" : "border-transparent text-muted hover:text-fg",
          )}
        >
          {t.label}
          {t.count != null && <span className="rounded-control bg-subtle px-1.5 text-xs text-muted tabular-nums">{t.count}</span>}
        </button>
      ))}
    </div>
  );
}
