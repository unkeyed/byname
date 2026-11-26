import { createContext, useContext } from "react";
import { keepPreviousData, MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { createBrowserRouter, Navigate, useLocation, type RouteObject } from "react-router";
import { ApiError, syncEntity } from "./api";
import { keys, useEntities } from "./queries";
import { Button, ErrorNote, Spinner } from "./ui";
import { Login } from "./pages/Login";
import { AcceptInvite } from "./pages/AcceptInvite";
import { Setup } from "./pages/Setup";
import { Consent } from "./pages/Consent";

/**
 * Every entity's pages live under /e/<slug>. Each entity the app opens
 * gets its own router, with /e/<slug> as its basename, and its own query
 * cache: a link inside it reads "/settings" and stays in it, and nothing one
 * entity loaded can show in another. This router has the pages outside
 * any entity, and its location says which entity is open.
 */
export const rootRouter = createBrowserRouter([
  { path: "/login", element: <Login /> },
  { path: "/invite/:token", element: <AcceptInvite /> },
  { path: "/setup/:token", element: <Setup /> },
  { path: "/oauth/consent", element: <Consent /> },
  { path: "/e/*", element: null },
  { path: "*", element: <ToDefault /> },
]);

// Every navigation, Back and Forward included, passes through here, so
// leaving an entity ends its requests at once.
rootRouter.subscribe(() => void syncEntity());

/** The entity a path is in, or "" for a page outside them. */
export const entityOf = (path: string) => /^\/e\/([^/]+)/.exec(path)?.[1] ?? "";

/** Opens a path outside the current entity: another entity's page, or one with no entity. */
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
        retry: (count, err) => !(err instanceof ApiError && err.status < 500) && !(err instanceof DOMException && err.name === "AbortError") && count < 2,
      },
    },
  });
  for (const prefix of [keys.results, keys.search]) client.setQueryDefaults(prefix, { placeholderData: keepPreviousData });
  return client;
}

/** The cache for pages outside any entity. */
export const rootClient = newClient();

type EntityApp = { slug: string; router: ReturnType<typeof createBrowserRouter>; client: QueryClient };
let open: EntityApp | undefined;

/**
 * The router and cache of the entity at slug, made when it opens and
 * dropped when another one does. Who is signed in and the entity list
 * are the same everywhere, so they carry over and switching shows no
 * spinner.
 */
export function entityApp(slug: string, routes: RouteObject[]): EntityApp {
  if (open?.slug === slug) return open;
  const client = newClient();
  for (const key of [keys.me, keys.entities]) {
    const data = open?.client.getQueryData(key);
    if (data !== undefined) client.setQueryData(key, data);
  }
  open?.router.dispose();
  open = { slug, router: createBrowserRouter(routes, { basename: `/e/${slug}` }), client };
  return open;
}

/** The open entity's routes. */
export const openRoutes = () => open?.router.routes ?? [];

/** Drops everything cached for the session and opens the login page, coming back to next after. */
export function toLogin(next?: string) {
  open?.router.dispose();
  open = undefined;
  rootClient.clear();
  go(next ? `/login?next=${encodeURIComponent(next)}` : "/login", { replace: true });
}

const Slug = createContext("");
export const EntitySlug = Slug.Provider;

/** The open entity's slug, its entry in the list once that loads, and the list. */
export function useEntity() {
  const slug = useContext(Slug);
  const list = useEntities();
  return { slug, current: list.data?.find((w) => w.slug === slug), list };
}

function ToDefault() {
  const entities = useEntities();
  const { pathname, search, hash } = useLocation();
  const home = entities.data?.find((w) => w.default);
  if (home) return <Navigate to={`/e/${home.slug}${pathname === "/" ? "" : pathname}${search}${hash}`} replace />;
  if (entities.isError && !(entities.error instanceof ApiError && entities.error.status === 401)) {
    return (
      <div className="mx-auto mt-24 max-w-sm space-y-3 px-4">
        <ErrorNote error={entities.error} />
        <Button onClick={() => void entities.refetch()}>Try again</Button>
      </div>
    );
  }
  return (
    <div className="grid h-full place-items-center">
      <Spinner />
    </div>
  );
}
