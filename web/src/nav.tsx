import { createContext, useContext } from "react";
import { Navigate, NavLink, Outlet, useLocation } from "react-router";
import { cx, type IconName } from "./ui";
import { useView } from "./view";

/** short is the label on a phone, where the full labels would not fit in one row. */
export type SectionTab = { path: string; label: string; short?: string };
export type NavItem = { to: string; label: string; icon: IconName; tabs?: SectionTab[]; filters?: boolean };

/**
 * The app's six places, by what a person tracks rather than how it is
 * measured. A section with tabs is a layout route, and its first tab is
 * where the bare path lands. filters marks the sections whose pages share
 * the date range and AI app filters, which follow you between them.
 */
export const nav: NavItem[] = [
  { to: "/", label: "This week", icon: "calendar" },
  {
    to: "/ai",
    label: "AI answers",
    icon: "sparkles",
    filters: true,
    tabs: [
      { path: "questions", label: "Questions" },
      { path: "competitors", label: "Competitors" },
      { path: "missed", label: "Missed questions", short: "Missed" },
      { path: "what-changed", label: "What changed", short: "Changes" },
    ],
  },
  {
    to: "/google",
    label: "Google search",
    icon: "search",
    tabs: [
      { path: "traffic", label: "Traffic" },
      { path: "site-changes", label: "Site changes" },
    ],
  },
  { to: "/social", label: "Social", icon: "message" },
  { to: "/alerts", label: "Alerts", icon: "bell" },
  { to: "/settings", label: "Settings", icon: "settings" },
];

export const section = (to: string) => nav.find((n) => n.to === to)!;

/** A section's bare path opens its first tab, keeping the query so the filters carry over. */
export function FirstTab({ item }: { item: NavItem }) {
  const { search, hash } = useLocation();
  return <Navigate to={`${item.to}/${item.tabs![0].path}${search}${hash}`} replace />;
}

const Current = createContext<NavItem | null>(null);

/** The section a page renders in, for the header's title and tab row. */
export const useSection = () => useContext(Current);

export function Section({ item }: { item: NavItem }) {
  return (
    <Current.Provider value={item}>
      <Outlet />
    </Current.Provider>
  );
}

/** A section's tabs as links, so each tab has its own URL. On a phone the tabs take their short labels, so the row fits. */
export function SectionTabs({ item }: { item: NavItem }) {
  const view = useView();
  const query = item.filters ? view.filterSearch() : "";
  return (
    <nav aria-label={item.label} data-load-visible className="flex shrink-0 gap-4 bg-bg px-3 shadow-[inset_0_-1px_0_var(--line)] md:gap-5 md:px-5">
      {item.tabs!.map((t) => (
        <NavLink
          key={t.path}
          to={`${item.to}/${t.path}${query}`}
          className={({ isActive }) =>
            cx(
              "inline-flex h-11 min-w-11 shrink-0 items-center justify-center border-b-2 text-sm font-medium whitespace-nowrap transition-colors md:h-9 md:min-w-0",
              isActive ? "border-fg text-fg" : "border-transparent text-muted hover:text-fg",
            )
          }
        >
          {t.short ? (
            <>
              <span className="md:hidden" aria-hidden="true">
                {t.short}
              </span>
              <span className="sr-only md:not-sr-only">{t.label}</span>
            </>
          ) : (
            t.label
          )}
        </NavLink>
      ))}
    </nav>
  );
}
