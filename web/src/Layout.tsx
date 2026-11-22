import { useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, Navigate, NavLink, Outlet, useLocation, useNavigate } from "react-router";
import { api, ApiError } from "./api";
import { useAlerts, useCan, useMe, useStatus } from "./queries";
import { NotSetUp } from "./pages/onboarding/Onboarding";
import { Toaster } from "./components/Toaster";
import { QuestionDrawer } from "./pages/results/QuestionDrawer";
import { EngineDrawer } from "./pages/results/EngineDrawer";
import { useView } from "./view";
import { nav } from "./nav";
import { useTheme, type ThemeMode } from "./theme";
import { Avatar, Button, buttonClass, Crumbs, cx, EmptyState, ErrorNote, Icon, LoadBoundary, Logo, NavMenu, PageHeader, scrollArea, Spinner, useModal, useWide, type IconName } from "./ui";

const themes: { mode: ThemeMode; icon: IconName; label: string }[] = [
  { mode: "system", icon: "monitor", label: "System theme" },
  { mode: "light", icon: "sun", label: "Light theme" },
  { mode: "dark", icon: "moon", label: "Dark theme" },
];

export function Layout() {
  const me = useMe();
  const location = useLocation();
  const wide = useWide();
  const [menuOpen, setMenuOpen] = useState(false);
  // Here, not in NavBody: NavBody moves between the rail and the phone menu
  // as the window resizes, and the theme must follow the system throughout.
  const theme = useTheme();
  const status = useStatus();
  const canSetUp = useCan("config:write");
  // Until setup is finished, people who can finish it go to onboarding and everyone else sees why the pages are empty. Settings stays open to all.
  const notSetUp = status.data?.onboarded === false && !location.pathname.startsWith("/settings");

  if (me.isPending || (status.isPending && !me.isError)) {
    return (
      <div className="grid h-full place-items-center">
        <Spinner />
      </div>
    );
  }
  if (me.error instanceof ApiError && me.error.status === 401) {
    return <Navigate to={`/login?next=${encodeURIComponent(location.pathname + location.search)}`} replace />;
  }
  if (me.isError) {
    return (
      <div className="mx-auto mt-24 max-w-sm space-y-3 px-4">
        <ErrorNote error={me.error} />
        <Button onClick={() => void me.refetch()}>Try again</Button>
      </div>
    );
  }

  if (notSetUp && canSetUp) return <Navigate to="/onboarding" replace />;

  return (
    <NavMenu.Provider value={wide ? null : () => setMenuOpen(true)}>
      <Toaster>
      <div className="flex h-full">
        {wide ? (
          <aside className="flex w-14 shrink-0 flex-col border-r border-line bg-bg lg:w-52">
            <NavBody label={me.data.name} theme={theme} rail />
          </aside>
        ) : (
          <MenuSheet open={menuOpen} onClose={() => setMenuOpen(false)} label={me.data.name} theme={theme} />
        )}
        <main className="flex min-w-0 flex-1 flex-col overflow-hidden bg-bg">
          <LoadBoundary key={notSetUp ? "not-set-up" : pageKey(location.pathname)} className="flex min-h-0 flex-1 flex-col">
            {notSetUp ? (
              <>
                <PageHeader title="Byname" />
                <NotSetUp />
              </>
            ) : (
              <Outlet />
            )}
          </LoadBoundary>
        </main>
      </div>
      <EngineDrawer />
      <QuestionDrawer />
      </Toaster>
    </NavMenu.Provider>
  );
}

/** Settings tabs share one boundary here, and each tab holds its own inside the settings page. */
const pageKey = (path: string) => (path.startsWith("/settings") ? "/settings" : path);

function MenuSheet({ open, onClose, label, theme }: { open: boolean; onClose: () => void; label: string; theme: ThemeState }) {
  const ref = useModal(open);
  return (
    <dialog
      ref={ref}
      onClose={onClose}
      onClick={(e) => e.target === ref.current && onClose()}
      aria-label="Menu"
      data-sheet
      className="m-0 h-dvh max-h-none w-72 max-w-[85vw] border-0 border-r border-line bg-bg p-0 text-fg shadow-floating"
    >
      <div className="flex h-full flex-col">
        <NavBody label={label} theme={theme} onNavigate={onClose} close={<Button variant="ghost" size="sm" onClick={onClose} aria-label="Close menu" icon className="ml-auto"><Icon name="x" /></Button>} />
      </div>
    </dialog>
  );
}

/**
 * The navigation, account and theme controls. As a rail (tablet) it shows
 * icons only until the lg breakpoint, where labels appear.
 */
type ThemeState = ReturnType<typeof useTheme>;

function NavBody({ label, theme: [theme, setTheme], rail, onNavigate, close }: { label: string; theme: ThemeState; rail?: boolean; onNavigate?: () => void; close?: ReactNode }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const filters = useView().filterSearch();
  const unread = useAlerts().data?.unread ?? 0;
  const wideOnly = rail ? "hidden lg:block" : "block";

  const logout = async () => {
    try {
      await api.auth.logout();
    } finally {
      queryClient.clear();
      void navigate("/login");
    }
  };

  return (
    <>
      <div className={cx("flex h-12 shrink-0 items-center gap-2", rail ? "justify-center lg:justify-start lg:px-4" : "pr-2 pl-4")}>
        <Logo />
        <span className={cx(wideOnly, "text-sm font-semibold")}>Byname</span>
        {close && <span className="ml-auto flex items-center gap-1">{close}</span>}
      </div>
      <nav aria-label="Main" className="flex flex-col gap-0.5 px-2 py-2">
        {nav.map((n) => (
          <NavLink
            key={n.to}
            to={n.filters ? n.to + filters : n.to}
            end={n.to === "/"}
            title={rail ? n.label : undefined}
            aria-label={n.to === "/alerts" && unread ? `${n.label}, ${unread} unread` : undefined}
            onClick={onNavigate}
            className={({ isActive }) =>
              cx(
                "flex h-11 items-center gap-2.5 rounded-surface px-2.5 md:h-8 text-sm font-medium transition-colors",
                rail && "justify-center lg:justify-start",
                isActive ? "bg-subtle text-fg" : "text-muted hover:bg-subtle hover:text-fg",
              )
            }
          >
            <span className="relative">
              <Icon name={n.icon} />
              {n.to === "/alerts" && unread > 0 && (
                <span className={cx(rail ? "lg:hidden" : "hidden", "absolute -top-1.5 -right-2 grid h-3.5 min-w-3.5 place-items-center rounded-full bg-danger px-0.5 text-[0.625rem] leading-none font-semibold text-surface tabular-nums")}>
                  {unread > 99 ? "99+" : unread}
                </span>
              )}
            </span>
            <span className={cx(wideOnly, "flex-1")}>{n.label}</span>
            {n.to === "/alerts" && unread > 0 && (
              <span className={cx(rail ? "hidden lg:grid" : "grid", "h-4 min-w-4 place-items-center rounded-full bg-danger px-1 text-[0.625rem] leading-none font-semibold text-surface tabular-nums")}>
                {unread > 99 ? "99+" : unread}
              </span>
            )}
          </NavLink>
        ))}
      </nav>

      <div className="mt-auto space-y-2 p-2">
        <div className="space-y-2 border-t border-line pt-2">
          <div className={cx("flex rounded-control border border-line p-0.5", rail && "flex-col lg:flex-row")} role="radiogroup" aria-label="Theme">
            {themes.map((t) => (
              <button
                key={t.mode}
                type="button"
                role="radio"
                aria-checked={theme === t.mode}
                aria-label={t.label}
                title={t.label}
                onClick={() => setTheme(t.mode)}
                className={cx(
                  "flex h-11 flex-1 items-center justify-center rounded-inner md:h-6 transition-colors",
                  theme === t.mode ? "bg-subtle-strong text-fg" : "text-faint hover:text-fg",
                )}
              >
                <Icon name={t.icon} className="size-3.5" />
              </button>
            ))}
          </div>
          <div className={cx("flex items-center gap-2 px-1", rail && "flex-col lg:flex-row")}>
            <Avatar label={label} size={24} />
            <AccountLabel label={label} className={cx(wideOnly, "min-w-0 flex-1")} />
            <Button variant="ghost" size="sm" onClick={() => void logout()} aria-label="Log out" title="Log out" icon>
              <Icon name="logout" className="size-3.5" />
            </Button>
          </div>
        </div>
      </div>
    </>
  );
}

/** An email reads as name, then domain, so neither half is cut off. */
function AccountLabel({ label, className }: { label: string; className?: string }) {
  const at = label.indexOf("@");
  return (
    <div className={cx("text-xs leading-4 [overflow-wrap:anywhere]", className)} title={label}>
      {at > 0 ? (
        <>
          <div className="font-medium">{label.slice(0, at)}</div>
          <div className="text-muted">{label.slice(at)}</div>
        </>
      ) : (
        <div className="font-medium">{label}</div>
      )}
    </div>
  );
}

export function NotFound({
  title = "Page not found",
  body = "That page does not exist.",
  parent,
}: {
  title?: string;
  body?: string;
  /** The page to go back to, as in "This week". */
  parent?: { label: string; to: string };
}) {
  const back = parent ?? { label: "This week", to: "/" };
  return (
    <>
      <PageHeader title={parent ? <Crumbs parent={parent.label} to={parent.to} current="Not found" /> : "Not found"} />
      <div className={cx("relative min-h-0 flex-1 px-4 py-16", scrollArea)}>
        <div className="mx-auto max-w-md">
          <EmptyState
            icon="search"
            title={title}
            action={
              <Link to={back.to} className={buttonClass()}>
                <Icon name="left" className="size-3.5" />
                Back to {back.label.toLowerCase()}
              </Link>
            }
          >
            {body}
          </EmptyState>
        </div>
      </div>
    </>
  );
}
