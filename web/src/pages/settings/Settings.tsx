import type { ReactNode } from "react";
import { Navigate, NavLink, Outlet, useLocation, type RouteObject } from "react-router";
import { useMe } from "../../queries";
import { cx, LoadBoundary, PageHeader } from "../../ui";
import { Account } from "./Account";
import { Apps } from "./Apps";
import { ApiKeys } from "./ApiKeys";
import { Notifications } from "./Notifications";
import { ReauthProvider } from "./Reauth";
import { SearchConsole } from "./SearchConsole";
import { SiteChanges } from "./SiteChanges";
import { SocialSettings } from "../social/SocialSettings";
import { Team } from "./Team";
import { Competitors, Credentials, General, Limits, Questions, Schedule, WherePeopleAsk } from "./SetupTabs";

/** admin marks the sections only admins can use; the API refuses them to everyone else. */
const tabs: { path: string; label: string; element: ReactNode; admin?: boolean }[] = [
  { path: "account", label: "Account", element: <Account /> },
  { path: "general", label: "General", element: <General /> },
  { path: "competitors", label: "Competitors", element: <Competitors /> },
  { path: "questions", label: "Questions", element: <Questions /> },
  { path: "surfaces", label: "Where people ask", element: <WherePeopleAsk /> },
  { path: "schedule", label: "Schedule", element: <Schedule /> },
  { path: "limits", label: "Limits", element: <Limits /> },
  { path: "credentials", label: "Credentials", element: <Credentials />, admin: true },
  { path: "keys", label: "API keys", element: <ApiKeys />, admin: true },
  { path: "apps", label: "Apps", element: <Apps />, admin: true },
  { path: "team", label: "Team", element: <Team />, admin: true },
  { path: "notifications", label: "Notifications", element: <Notifications />, admin: true },
  { path: "search-console", label: "Search Console", element: <SearchConsole />, admin: true },
  { path: "site-changes", label: "Site changes", element: <SiteChanges />, admin: true },
  { path: "social", label: "Social", element: <SocialSettings /> },
];

export const settingsRoutes: RouteObject[] = [
  { index: true, element: <Navigate to={tabs[0].path} replace /> },
  ...tabs.map((t) => ({ path: t.path, element: t.element })),
];

/**
 * Sections are wrapping pills above the content up to lg, and a list beside
 * it from lg up, so every section stays one tap away however many there are.
 */
export function Settings() {
  const me = useMe();
  const { pathname } = useLocation();
  const shown = tabs.filter((t) => !t.admin || me.data?.role === "admin");
  return (
    <>
      <PageHeader title="Settings" />
      <div className="relative min-h-0 flex-1 scroll-pb-36 overflow-auto">
        <div className="mx-auto max-w-3xl lg:grid lg:max-w-5xl lg:grid-cols-[11rem_minmax(0,1fr)] lg:gap-8 lg:px-8">
          <nav aria-label="Settings" className="bg-bg lg:sticky lg:top-0 lg:self-start lg:py-8">
            <div className="flex flex-wrap gap-1 px-4 pt-4 md:px-8 lg:flex-col lg:gap-0.5 lg:p-0">
              {shown.map((t) => (
                <NavLink
                  key={t.path}
                  to={t.path}
                  className={({ isActive }) =>
                    cx(
                      "flex h-11 items-center rounded-control px-2.5 md:h-8 text-sm font-medium whitespace-nowrap transition-colors",
                      isActive ? "bg-subtle-strong text-fg" : "text-muted hover:bg-subtle hover:text-fg",
                    )
                  }
                >
                  {t.label}
                </NavLink>
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
