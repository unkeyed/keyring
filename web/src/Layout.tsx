import { useQueryClient } from "@tanstack/react-query";
import { Link, Navigate, NavLink, Outlet, useLocation, useNavigate } from "react-router";
import { api, ApiError } from "./api";
import { useMe, useStatus } from "./queries";
import { useTheme, type ThemeMode } from "./theme";
import { Avatar, Button, cx, ErrorNote, Icon, Spinner, type IconName } from "./ui";

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
      <div className="mx-auto mt-24 max-w-sm space-y-3">
        <ErrorNote error={me.error} />
        <Button onClick={() => void me.refetch()}>Try again</Button>
      </div>
    );
  }

  return (
    <div className="flex h-full">
      <Sidebar label={me.data.label} />
      <main className="flex min-w-0 flex-1 flex-col overflow-hidden bg-surface">
        <Outlet />
      </main>
    </div>
  );
}

function Sidebar({ label }: { label: string }) {
  const [theme, setTheme] = useTheme();
  const status = useStatus();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const halted = status.data?.enrichment_halt;

  const logout = async () => {
    try {
      await api.auth.logout();
    } finally {
      queryClient.clear();
      void navigate("/login");
    }
  };

  return (
    <aside className="flex w-14 shrink-0 flex-col border-r border-line bg-bg lg:w-52">
      <div className="flex h-12 items-center gap-2 px-4">
        <span className="grid size-6 place-items-center rounded-control bg-primary text-xs font-bold text-primary-fg">u</span>
        <span className="hidden text-sm font-semibold lg:inline">uncrm</span>
      </div>
      <nav aria-label="Main" className="flex flex-col gap-0.5 px-2 py-2">
        {nav.map((n) => (
          <NavLink
            key={n.to}
            to={n.to}
            title={n.label}
            className={({ isActive }) =>
              cx(
                "flex h-8 items-center gap-2.5 rounded-control px-2.5 text-sm font-medium",
                isActive ? "bg-subtle text-fg" : "text-muted hover:bg-subtle hover:text-fg",
              )
            }
          >
            <Icon name={n.icon} />
            <span className="hidden lg:inline">{n.label}</span>
          </NavLink>
        ))}
      </nav>

      {halted && (
        <Link
          to="/settings/enrichment"
          className="mx-2 mt-2 flex items-start gap-2 rounded-control bg-warn-soft p-2 text-xs text-warn"
          title={halted.reason}
        >
          <Icon name="alert" className="mt-px size-3.5" />
          <span className="hidden lg:inline">Enrichment paused. See why.</span>
        </Link>
      )}

      <div className="mt-auto border-t border-line p-2">
        <div className="mb-2 hidden items-center rounded-control border border-line p-0.5 lg:flex" role="radiogroup" aria-label="Theme">
          {themes.map((t) => (
            <button
              key={t.mode}
              type="button"
              role="radio"
              aria-checked={theme === t.mode}
              aria-label={t.label}
              title={t.label}
              onClick={() => setTheme(t.mode)}
              className={cx("flex h-6 flex-1 items-center justify-center rounded", theme === t.mode ? "bg-subtle text-fg" : "text-faint hover:text-fg")}
            >
              <Icon name={t.icon} className="size-3.5" />
            </button>
          ))}
        </div>
        <div className="flex flex-col items-center gap-2 px-1 lg:flex-row">
          <Avatar label={label} size={22} />
          <span className="hidden min-w-0 flex-1 truncate text-xs text-muted lg:inline" title={label}>
            {label}
          </span>
          <Button variant="ghost" size="sm" onClick={() => void logout()} aria-label="Log out" title="Log out">
            <Icon name="logout" className="size-3.5" />
          </Button>
        </div>
      </div>
    </aside>
  );
}

export function NotFound() {
  return (
    <div className="mx-auto mt-24 max-w-sm text-center">
      <p className="text-base font-semibold">Nothing here</p>
      <p className="mt-1 text-muted">That page does not exist.</p>
      <Link to="/contacts" className="mt-4 inline-block text-fg underline underline-offset-2 hover:opacity-70">
        Go to contacts
      </Link>
    </div>
  );
}
