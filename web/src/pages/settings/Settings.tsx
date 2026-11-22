import type { ReactNode } from "react";
import { Navigate, NavLink, Outlet, useLocation, useNavigate, type RouteObject } from "react-router";
import { useMe } from "../../queries";
import { cx, LoadBoundary, PageHeader, scrollArea, selectClass } from "../../ui";
import { Account } from "./Account";
import { Apps } from "./Apps";
import { ApiKeys } from "./ApiKeys";
import { CheckHistory } from "./CheckHistory";
import { Notifications } from "./Notifications";
import { ReauthProvider } from "./Reauth";
import { SearchConsole } from "./SearchConsole";
import { SiteChanges } from "./SiteChanges";
import { SocialSettings } from "../social/SocialSettings";
import { Team } from "./Team";
import { Competitors, General, ProviderKeys, Questions, ScheduleAndSpending, Treg, WherePeopleAsk } from "./SetupTabs";

type Page = { path: string; label: string; element: ReactNode; admin?: boolean };

/**
 * Settings in five groups, by what a person is trying to do. admin marks the
 * pages only admins can use; the API refuses them to everyone else.
 */
const sections: { label: string; pages: Page[] }[] = [
  {
    label: "Your brand",
    pages: [
      { path: "general", label: "General", element: <General /> },
      { path: "competitors", label: "Competitors", element: <Competitors /> },
      { path: "questions", label: "Questions", element: <Questions /> },
    ],
  },
  {
    label: "Where Byname asks",
    pages: [
      { path: "ai-apps", label: "AI apps", element: <WherePeopleAsk /> },
      { path: "treg", label: "Treg", element: <Treg />, admin: true },
    ],
  },
  {
    label: "Schedule and spending",
    pages: [
      { path: "schedule", label: "Schedule and limits", element: <ScheduleAndSpending /> },
      { path: "checks", label: "Check history", element: <CheckHistory /> },
    ],
  },
  {
    label: "Connections",
    pages: [
      { path: "search-console", label: "Search Console", element: <SearchConsole />, admin: true },
      { path: "site-changes", label: "Site changes", element: <SiteChanges />, admin: true },
      { path: "social", label: "Social", element: <SocialSettings /> },
      { path: "alerts", label: "Alerts", element: <Notifications />, admin: true },
    ],
  },
  {
    label: "Team and access",
    pages: [
      { path: "account", label: "Account", element: <Account /> },
      { path: "team", label: "Team", element: <Team />, admin: true },
      { path: "provider-keys", label: "Provider keys", element: <ProviderKeys />, admin: true },
      { path: "keys", label: "API keys", element: <ApiKeys />, admin: true },
      { path: "apps", label: "Apps", element: <Apps />, admin: true },
    ],
  },
];

export const settingsRoutes: RouteObject[] = [
  { index: true, element: <Navigate to="general" replace /> },
  ...sections.flatMap((s) => s.pages).map((p) => ({ path: p.path, element: p.element })),
];

/**
 * From lg up, the groups are a list beside the page. Below it they are one
 * select, so the whole list never pushes the page off a phone's screen.
 */
export function Settings() {
  const me = useMe();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const admin = me.data?.role === "admin";
  const shown = sections.map((s) => ({ ...s, pages: s.pages.filter((p) => !p.admin || admin) })).filter((s) => s.pages.length > 0);
  const current = pathname.split("/")[2] ?? "";
  return (
    <>
      <PageHeader title="Settings" />
      <div className={cx("relative min-h-0 flex-1 scroll-pb-36", scrollArea)}>
        <div className="mx-auto max-w-3xl lg:grid lg:max-w-5xl lg:grid-cols-[12rem_minmax(0,1fr)] lg:gap-8 lg:px-8">
          <nav aria-label="Settings" className="bg-bg px-4 pt-4 md:px-8 lg:sticky lg:top-0 lg:self-start lg:px-0 lg:py-8">
            <select aria-label="Settings page" className={cx(selectClass, "w-full lg:hidden")} value={current} onChange={(e) => void navigate(e.target.value)}>
              {shown.map((s) => (
                <optgroup key={s.label} label={s.label}>
                  {s.pages.map((p) => (
                    <option key={p.path} value={p.path}>
                      {p.label}
                    </option>
                  ))}
                </optgroup>
              ))}
            </select>
            <div className="hidden space-y-4 lg:block">
              {shown.map((s) => (
                <div key={s.label}>
                  <h2 className="mb-1 px-2.5 text-xs font-medium text-faint">{s.label}</h2>
                  <div className="flex flex-col gap-0.5">
                    {s.pages.map((p) => (
                      <NavLink
                        key={p.path}
                        to={p.path}
                        className={({ isActive }) =>
                          cx("flex h-8 items-center rounded-control px-2.5 text-sm font-medium whitespace-nowrap transition-colors", isActive ? "bg-subtle-strong text-fg" : "text-muted hover:bg-subtle hover:text-fg")
                        }
                      >
                        {p.label}
                      </NavLink>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </nav>
          <div className="min-w-0 px-4 py-6 pb-16 md:px-8 md:py-8 lg:px-0">
            <ReauthProvider>
              <LoadBoundary key={pathname}>
                <Outlet />
              </LoadBoundary>
            </ReauthProvider>
          </div>
        </div>
      </div>
    </>
  );
}

export function SettingsHeader({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="mb-6">
      <h2 className="text-lg font-semibold">{title}</h2>
      {children && <p className="mt-1 text-sm text-muted">{children}</p>}
    </div>
  );
}

export function SettingsSubhead({ children, className }: { children: ReactNode; className?: string }) {
  return <h3 className={cx("mb-3 text-sm font-semibold", className)}>{children}</h3>;
}
