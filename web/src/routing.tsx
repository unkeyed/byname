import { createContext, useContext } from "react";
import { keepPreviousData, MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { createBrowserRouter, Navigate, useLocation, type RouteObject } from "react-router";
import { ApiError } from "./api";
import { keys, useWorkspaces } from "./queries";
import { Button, ErrorNote, Spinner } from "./ui";
import { Login } from "./pages/Login";
import { AcceptInvite } from "./pages/AcceptInvite";
import { Setup } from "./pages/Setup";
import { Consent } from "./pages/Consent";

/**
 * Every workspace's pages live under /w/<slug>. Each workspace the app opens
 * gets its own router, with /w/<slug> as its basename, and its own query
 * cache: a link inside it reads "/settings" and stays in it, and nothing one
 * workspace loaded can show in another. This router has the pages outside
 * any workspace, and its location says which workspace is open.
 */
export const rootRouter = createBrowserRouter([
  { path: "/login", element: <Login /> },
  { path: "/invite/:token", element: <AcceptInvite /> },
  { path: "/setup/:token", element: <Setup /> },
  { path: "/oauth/consent", element: <Consent /> },
  { path: "/w/*", element: null },
  { path: "*", element: <ToDefault /> },
]);

/** The workspace a path is in, or "" for a page outside them. */
export const workspaceOf = (path: string) => /^\/w\/([^/]+)/.exec(path)?.[1] ?? "";

/** Opens a path outside the current workspace: another workspace's page, or one with no workspace. */
export const go = (to: string, opts?: { replace?: boolean }) => void rootRouter.navigate(to, opts);

/** Pages for people without a session, where a 401 is an answer rather than an expired session. */
const signedOutPages = ["/login", "/invite/", "/setup/"];

/** A 401 anywhere means the session ended: go to login and come back after. */
function onError(err: unknown) {
  if (!(err instanceof ApiError) || err.status !== 401) return;
  if (signedOutPages.some((p) => location.pathname.startsWith(p))) return;
  toLogin(location.pathname + location.search);
}

function newClient() {
  const client = new QueryClient({
    queryCache: new QueryCache({ onError }),
    mutationCache: new MutationCache({ onError }),
    defaultOptions: {
      queries: {
        staleTime: 10_000,
        retry: (count, err) => !(err instanceof ApiError && err.status < 500) && count < 2,
      },
    },
  });
  for (const prefix of [keys.results, keys.search]) client.setQueryDefaults(prefix, { placeholderData: keepPreviousData });
  return client;
}

/** The cache for pages outside any workspace. */
export const rootClient = newClient();

type WorkspaceApp = { slug: string; router: ReturnType<typeof createBrowserRouter>; client: QueryClient };
let open: WorkspaceApp | undefined;

/**
 * The router and cache of the workspace at slug, made when it opens and
 * dropped when another one does. Who is signed in and the workspace list
 * are the same everywhere, so they carry over and switching shows no
 * spinner.
 */
export function workspaceApp(slug: string, routes: RouteObject[]): WorkspaceApp {
  if (open?.slug === slug) return open;
  const client = newClient();
  for (const key of [keys.me, keys.workspaces]) {
    const data = open?.client.getQueryData(key);
    if (data !== undefined) client.setQueryData(key, data);
  }
  open?.router.dispose();
  open = { slug, router: createBrowserRouter(routes, { basename: `/w/${slug}` }), client };
  return open;
}

/** Drops everything cached for the session and opens the login page, coming back to next after. */
export function toLogin(next?: string) {
  open?.router.dispose();
  open = undefined;
  rootClient.clear();
  go(next ? `/login?next=${encodeURIComponent(next)}` : "/login", { replace: true });
}

const Slug = createContext("");
export const WorkspaceSlug = Slug.Provider;

/** The open workspace's slug, its entry in the list once that loads, and the list. */
export function useWorkspace() {
  const slug = useContext(Slug);
  const list = useWorkspaces();
  return { slug, current: list.data?.find((w) => w.slug === slug), list };
}

/**
 * A path from before workspaces, such as a bookmark or an old notification's
 * link, opens the same page in the default workspace. The workspace's own
 * router then follows any page that moved.
 */
function ToDefault() {
  const workspaces = useWorkspaces();
  const { pathname, search, hash } = useLocation();
  const home = workspaces.data?.find((w) => w.default);
  if (home) return <Navigate to={`/w/${home.slug}${pathname === "/" ? "" : pathname}${search}${hash}`} replace />;
  if (workspaces.isError && !(workspaces.error instanceof ApiError && workspaces.error.status === 401)) {
    return (
      <div className="mx-auto mt-24 max-w-sm space-y-3 px-4">
        <ErrorNote error={workspaces.error} />
        <Button onClick={() => void workspaces.refetch()}>Try again</Button>
      </div>
    );
  }
  return (
    <div className="grid h-full place-items-center">
      <Spinner />
    </div>
  );
}
