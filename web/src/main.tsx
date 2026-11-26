import { StrictMode, useSyncExternalStore } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { Navigate, RouterProvider, useLocation, type RouteObject } from "react-router";
import { Layout, NotFound } from "./Layout";
import { FirstTab, section, Section } from "./nav";
import { rootClient, rootRouter, workspaceApp, WorkspaceSlug, workspaceOf } from "./routing";
import { ThisWeek } from "./pages/results/ThisWeek";
import { Competitors } from "./pages/results/Competitors";
import { Questions } from "./pages/results/Questions";
import { Missed } from "./pages/results/Missed";
import { Compare } from "./pages/results/Compare";
import { Notifications } from "./pages/notifications/Notifications";
import { Search } from "./pages/search/Search";
import { Changes } from "./pages/changes/Changes";
import { Social } from "./pages/social/Social";
import { Settings, settingsRoutes } from "./pages/settings/Settings";
import { Onboarding } from "./pages/onboarding/Onboarding";
import "@fontsource-variable/geist";
import "@fontsource-variable/geist-mono";
import "./index.css";

/**
 * Pages that moved when Bing joined Google. Notifications sent before
 * still link to the old paths, and bookmarks keep them.
 */
const moved: Record<string, string> = {
  "/google": "/search/traffic?provider=google",
  "/google/traffic": "/search/traffic?provider=google",
  "/google/site-changes": "/search/site-changes",
  "/settings/search-console": "/settings/search-engines",
  "/onboarding/search_console": "/onboarding/search",
};

/** Sends an old path to where it moved, keeping its query, such as a change's ?chg=. */
function Moved({ to }: { to: string }) {
  const { search, hash } = useLocation();
  const url = new URL(to, window.location.origin);
  new URLSearchParams(search).forEach((v, k) => url.searchParams.set(k, v));
  return <Navigate to={url.pathname + url.search + hash} replace />;
}

/** One workspace's pages, under its /w/<slug>. */
const workspaceRoutes: RouteObject[] = [
  ...Object.entries(moved).map(([path, to]) => ({ path, element: <Moved to={to} /> })),
  { path: "/onboarding/:step?", element: <Onboarding /> },
  {
    element: <Layout />,
    children: [
      { index: true, element: <ThisWeek /> },
      {
        path: "ai",
        element: <Section item={section("/ai")} />,
        children: [
          { index: true, element: <FirstTab item={section("/ai")} /> },
          { path: "questions", element: <Questions /> },
          { path: "competitors", element: <Competitors /> },
          { path: "missed", element: <Missed /> },
          { path: "what-changed", element: <Compare /> },
        ],
      },
      {
        path: "search",
        element: <Section item={section("/search")} />,
        children: [
          { index: true, element: <FirstTab item={section("/search")} /> },
          { path: "traffic", element: <Search /> },
          { path: "site-changes", element: <Changes /> },
        ],
      },
      { path: "social", element: <Social /> },
      { path: "notifications", element: <Notifications /> },
      { path: "settings", element: <Settings />, children: settingsRoutes },
      { path: "*", element: <NotFound /> },
    ],
  },
];

/** The open workspace's pages, or the pages outside any workspace. */
function App() {
  const slug = useSyncExternalStore(rootRouter.subscribe, () => workspaceOf(rootRouter.state.location.pathname));
  if (!slug) {
    return (
      <QueryClientProvider client={rootClient}>
        <RouterProvider router={rootRouter} />
      </QueryClientProvider>
    );
  }
  const app = workspaceApp(slug, workspaceRoutes);
  return (
    <WorkspaceSlug value={slug}>
      <QueryClientProvider key={slug} client={app.client}>
        <RouterProvider router={app.router} />
      </QueryClientProvider>
    </WorkspaceSlug>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
