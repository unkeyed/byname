import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createBrowserRouter, RouterProvider } from "react-router";
import { ApiError } from "./api";
import { Layout, nav, NotFound } from "./Layout";
import { Login } from "./pages/Login";
import { AcceptInvite } from "./pages/AcceptInvite";
import { Setup } from "./pages/Setup";
import { Consent } from "./pages/Consent";
import { ComingSoon } from "./pages/ComingSoon";
import { Overview } from "./pages/results/Overview";
import { Competitors } from "./pages/results/Competitors";
import { Prompts } from "./pages/results/Prompts";
import { Gaps } from "./pages/results/Gaps";
import { Compare } from "./pages/results/Compare";
import { Runs } from "./pages/results/Runs";
import { Settings, settingsRoutes } from "./pages/settings/Settings";
import "@fontsource-variable/geist";
import "@fontsource-variable/geist-mono";
import "./index.css";

const router = createBrowserRouter([
  { path: "/login", element: <Login /> },
  { path: "/invite/:token", element: <AcceptInvite /> },
  { path: "/setup/:token", element: <Setup /> },
  { path: "/oauth/consent", element: <Consent /> },
  {
    element: <Layout />,
    children: [
      { index: true, element: <Overview /> },
      { path: "prompts", element: <Prompts /> },
      { path: "competitors", element: <Competitors /> },
      { path: "gaps", element: <Gaps /> },
      { path: "compare", element: <Compare /> },
      { path: "runs", element: <Runs /> },
      ...nav.flatMap((n) => (n.soon ? [{ path: n.to === "/" ? undefined : n.to, index: n.to === "/", element: <ComingSoon title={n.label} body={n.soon} /> }] : [])),
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

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
