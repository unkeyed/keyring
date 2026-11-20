import { useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, Navigate, NavLink, Outlet, useLocation, useNavigate } from "react-router";
import { api, ApiError } from "./api";
import { useMe, useStatus } from "./queries";
import { useTheme, type ThemeMode } from "./theme";
import { Avatar, Button, buttonClass, Crumbs, cx, EmptyState, ErrorNote, Icon, Logo, NavMenu, PageHeader, Spinner, useModal, useWide, type IconName } from "./ui";

const nav: { to: string; label: string; icon: IconName }[] = [
  { to: "/contacts", label: "Contacts", icon: "users" },
  { to: "/companies", label: "Companies", icon: "building" },
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
      <div className="flex h-full">
        {wide ? (
          <aside className="flex w-14 shrink-0 flex-col border-r border-line bg-bg lg:w-52">
            <NavBody label={me.data.label} rail />
          </aside>
        ) : (
          <MenuSheet open={menuOpen} onClose={() => setMenuOpen(false)} label={me.data.label} />
        )}
        <main className="flex min-w-0 flex-1 flex-col overflow-hidden bg-bg">
          <Outlet />
        </main>
      </div>
    </NavMenu.Provider>
  );
}

function MenuSheet({ open, onClose, label }: { open: boolean; onClose: () => void; label: string }) {
  const ref = useModal(open);
  return (
    <dialog
      ref={ref}
      onClose={onClose}
      onClick={(e) => e.target === ref.current && onClose()}
      aria-label="Menu"
      className="m-0 h-dvh max-h-none w-72 max-w-[85vw] border-0 border-r border-line bg-bg p-0 text-fg shadow-floating"
    >
      {open && (
        <div className="flex h-full flex-col">
          <NavBody label={label} onNavigate={onClose} close={<Button variant="ghost" size="sm" onClick={onClose} aria-label="Close menu" className="ml-auto w-8 px-0"><Icon name="x" /></Button>} />
        </div>
      )}
    </dialog>
  );
}

/**
 * The navigation, account and theme controls. As a rail (tablet) it shows
 * icons only until the lg breakpoint, where labels appear.
 */
function NavBody({ label, rail, onNavigate, close }: { label: string; rail?: boolean; onNavigate?: () => void; close?: ReactNode }) {
  const [theme, setTheme] = useTheme();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
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
        <span className={cx(wideOnly, "text-sm font-semibold")}>uncrm</span>
        {close}
      </div>
      <nav aria-label="Main" className="flex flex-col gap-0.5 px-2 py-2">
        {nav.map((n) => (
          <NavLink
            key={n.to}
            to={n.to}
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
        <EnrichmentNotice rail={rail} onNavigate={onNavigate} />
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
                  "flex h-6 flex-1 items-center justify-center rounded transition-colors",
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

/** The one place that says enrichment is off or paused, instead of every record repeating it. */
function EnrichmentNotice({ rail, onNavigate }: { rail?: boolean; onNavigate?: () => void }) {
  const status = useStatus();
  const s = status.data;
  if (!s || (s.enrichment_enabled && !s.enrichment_halt)) return null;
  const halted = s.enrichment_halt;
  const text = halted ? "Enrichment paused" : "Enrichment is off";
  return (
    <Link
      to="/settings/enrichment"
      onClick={onNavigate}
      aria-label={text}
      title={halted ? halted.reason : "Turn it on to fill in titles, companies and logos"}
      className={cx(
        "flex h-8 items-center gap-2 rounded-surface px-2.5 text-xs transition-colors",
        rail && "justify-center lg:justify-start",
        halted ? "bg-warn-soft text-warn" : "text-muted hover:bg-subtle hover:text-fg",
      )}
    >
      <Icon name={halted ? "alert" : "sparkles"} className="size-3.5" />
      <span className={cx(rail ? "hidden lg:inline" : "inline", "min-w-0 flex-1 truncate")}>{text}</span>
      <Icon name="right" className={cx(rail ? "hidden lg:block" : "block", "size-3.5 text-faint")} />
    </Link>
  );
}

export function NotFound({
  title = "Page not found",
  body = "That page does not exist.",
  parent,
}: {
  title?: string;
  body?: string;
  /** The list to go back to, as in "Contacts". */
  parent?: { label: string; to: string };
}) {
  const back = parent ?? { label: "Contacts", to: "/contacts" };
  return (
    <>
      <PageHeader title={parent ? <Crumbs parent={parent.label} to={parent.to} current="Not found" /> : "Not found"} />
      <div className="min-h-0 flex-1 overflow-auto px-4 py-16">
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
