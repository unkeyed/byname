import { createContext, useContext, useLayoutEffect, useRef } from "react";
import { Navigate, NavLink, Outlet, useLocation } from "react-router";
import { cx, type IconName } from "./ui";
import { useView } from "./view";

export type SectionTab = { path: string; label: string };
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
      { path: "missed", label: "Missed questions" },
      { path: "what-changed", label: "What changed" },
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

/** Pages that moved when the navigation was regrouped. Old links and bookmarks land on the new page with their query. */
export const moved: [from: string, to: string][] = [
  ["prompts", "/ai/questions"],
  ["competitors", "/ai/competitors"],
  ["gaps", "/ai/missed"],
  ["compare", "/ai/what-changed"],
  ["search", "/google/traffic"],
  ["changes", "/google/site-changes"],
  ["runs", "/settings/checks"],
];

/** Redirects to `to`, keeping the query and hash, so a moved link opens the same view. */
export function Moved({ to }: { to: string }) {
  const { search, hash } = useLocation();
  return <Navigate to={to + search + hash} replace />;
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

/**
 * A section's tabs as links, so each tab has its own URL. On a phone the row
 * scrolls sideways rather than wrapping, with the current tab scrolled into view.
 */
export function SectionTabs({ item }: { item: NavItem }) {
  const view = useView();
  const ref = useRef<HTMLElement>(null);
  const { pathname } = useLocation();
  useLayoutEffect(() => {
    const row = ref.current;
    const active = row?.querySelector<HTMLElement>("[aria-current=page]");
    if (row && active && (active.offsetLeft < row.scrollLeft || active.offsetLeft + active.offsetWidth > row.scrollLeft + row.clientWidth)) row.scrollLeft = active.offsetLeft - 12;
  }, [pathname]);
  const query = item.filters ? view.filterSearch() : "";
  return (
    <nav ref={ref} aria-label={item.label} data-load-visible className="relative flex shrink-0 gap-4 overflow-x-auto bg-bg px-3 shadow-[inset_0_-1px_0_var(--line)] [scrollbar-width:none] md:gap-5 md:px-5">
      {item.tabs!.map((t) => (
        <NavLink
          key={t.path}
          to={`${item.to}/${t.path}${query}`}
          className={({ isActive }) =>
            cx(
              "inline-flex h-11 shrink-0 items-center border-b-2 text-sm font-medium whitespace-nowrap transition-colors md:h-9",
              isActive ? "border-fg text-fg" : "border-transparent text-muted hover:text-fg",
            )
          }
        >
          {t.label}
        </NavLink>
      ))}
    </nav>
  );
}
