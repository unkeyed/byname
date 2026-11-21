import { useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, Navigate, NavLink, Outlet, useLocation, useNavigate } from "react-router";
import { api, ApiError } from "./api";
import { useMe } from "./queries";
import { Toaster } from "./components/Toaster";
import { useView } from "./view";
import { useTheme, type ThemeMode } from "./theme";
import { Avatar, Button, buttonClass, Crumbs, cx, EmptyState, ErrorNote, Icon, Logo, NavMenu, PageHeader, Spinner, useModal, useWide, type IconName } from "./ui";

/** The app's pages. soon is what a page will show, for those not built yet. */
export const nav: { to: string; label: string; icon: IconName; soon?: string }[] = [
  { to: "/", label: "Overview", icon: "chart", soon: "Mention rate, citation rate and share of mentions on every surface, next to your Search Console clicks." },
  { to: "/prompts", label: "Prompts", icon: "message", soon: "Every question Byname asks, in its groups, with whether each surface and model named or cited you." },
  { to: "/competitors", label: "Competitors", icon: "users", soon: "Who gets named instead of you, on which surfaces, and how that moves." },
  { to: "/gaps", label: "Gaps", icon: "alert", soon: "The questions where competitors are named and you are not." },
  { to: "/compare", label: "Compare", icon: "layers", soon: "Two checks side by side: the answers you gained, lost and kept." },
  { to: "/search", label: "Search", icon: "globe", soon: "Google Search Console clicks, impressions and position, with your goals." },
  { to: "/changes", label: "Changes", icon: "pencil", soon: "Changes to your site, when Google recrawled them, and what moved after." },
  { to: "/runs", label: "Runs", icon: "refresh" },
  { to: "/settings", label: "Settings", icon: "settings" },
];

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

  if (me.isPending) {
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
          <Outlet />
        </main>
      </div>
      </Toaster>
    </NavMenu.Provider>
  );
}

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
        <NavBody label={label} theme={theme} onNavigate={onClose} close={<Button variant="ghost" size="sm" onClick={onClose} aria-label="Close menu" className="ml-auto w-8 px-0"><Icon name="x" /></Button>} />
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
        {close}
      </div>
      <nav aria-label="Main" className="flex flex-col gap-0.5 px-2 py-2">
        {nav.map((n) => (
          <NavLink
            key={n.to}
            to={n.to === "/settings" ? n.to : n.to + filters}
            end={n.to === "/"}
            title={rail ? n.label : undefined}
            onClick={onNavigate}
            className={({ isActive }) =>
              cx(
                "flex h-8 items-center gap-2.5 rounded-surface px-2.5 text-sm font-medium transition-colors",
                rail && "justify-center lg:justify-start",
                isActive ? "bg-subtle text-fg" : "text-muted hover:bg-subtle hover:text-fg",
              )
            }
          >
            <Icon name={n.icon} />
            <span className={wideOnly}>{n.label}</span>
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
                  "flex h-6 flex-1 items-center justify-center rounded-inner transition-colors",
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
            <Button variant="ghost" size="sm" onClick={() => void logout()} aria-label="Log out" title="Log out" className="w-7 px-0">
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
  /** The page to go back to, as in "Overview". */
  parent?: { label: string; to: string };
}) {
  const back = parent ?? { label: "Overview", to: "/" };
  return (
    <>
      <PageHeader title={parent ? <Crumbs parent={parent.label} to={parent.to} current="Not found" /> : "Not found"} />
      <div className="relative min-h-0 flex-1 overflow-auto px-4 py-16">
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
