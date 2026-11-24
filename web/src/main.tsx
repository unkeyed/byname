import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { keepPreviousData, MutationCache, QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createBrowserRouter, Navigate, RouterProvider, useLocation } from "react-router";
import { ApiError } from "./api";
import { keys } from "./queries";
import { Layout, NotFound } from "./Layout";
import { FirstTab, section, Section } from "./nav";
import { Login } from "./pages/Login";
import { AcceptInvite } from "./pages/AcceptInvite";
import { Setup } from "./pages/Setup";
import { Consent } from "./pages/Consent";
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

const router = createBrowserRouter([
  ...Object.entries(moved).map(([path, to]) => ({ path, element: <Moved to={to} /> })),
  { path: "/login", element: <Login /> },
  { path: "/invite/:token", element: <AcceptInvite /> },
  { path: "/setup/:token", element: <Setup /> },
  { path: "/oauth/consent", element: <Consent /> },
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
]);

/** Pages for people without a session, where a 401 is an answer rather than an expired session. */
const signedOutPages = ["/login", "/invite/", "/setup/"];

/** A 401 anywhere means the session ended: go to login and come back after. */
function onError(err: unknown) {
  if (!(err instanceof ApiError) || err.status !== 401) return;
  const { pathname, search } = router.state.location;
  if (signedOutPages.some((p) => pathname.startsWith(p))) return;
  queryClient.clear();
  void router.navigate(`/login?next=${encodeURIComponent(pathname + search)}`, { replace: true });
}

const queryClient = new QueryClient({
  queryCache: new QueryCache({ onError }),
  mutationCache: new MutationCache({ onError }),
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      retry: (count, err) => !(err instanceof ApiError && err.status < 500) && count < 2,
    },
  },
});

for (const prefix of [keys.results, keys.search]) queryClient.setQueryDefaults(prefix, { placeholderData: keepPreviousData });

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
