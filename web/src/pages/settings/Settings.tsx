import type { ReactNode } from "react";
import { Navigate, NavLink, Outlet, type RouteObject } from "react-router";
import { cx, Icon, PageHeader, type IconName } from "../../ui";
import { Agents } from "./Agents";
import { ApiKeys } from "./ApiKeys";
import { Enrichment } from "./Enrichment";
import { Providers } from "./Providers";
import { Stages } from "./Stages";
import { Team } from "./Team";

const tabs: { path: string; label: string; icon: IconName; element: ReactNode }[] = [
  { path: "providers", label: "Providers", icon: "plug", element: <Providers /> },
  { path: "keys", label: "API keys", icon: "key", element: <ApiKeys /> },
  { path: "agents", label: "Agents & MCP", icon: "bot", element: <Agents /> },
  { path: "team", label: "Team", icon: "users", element: <Team /> },
  { path: "stages", label: "Stages", icon: "layers", element: <Stages /> },
  { path: "enrichment", label: "Enrichment", icon: "sparkles", element: <Enrichment /> },
];

export const settingsRoutes: RouteObject[] = [
  { index: true, element: <Navigate to="providers" replace /> },
  ...tabs.map((t) => ({ path: t.path, element: t.element })),
];

export function Settings() {
  return (
    <>
      <PageHeader title="Settings" />
      <div className="flex min-h-0 flex-1">
        <nav aria-label="Settings" className="w-48 shrink-0 space-y-0.5 border-r border-line p-2">
          {tabs.map((t) => (
            <NavLink
              key={t.path}
              to={t.path}
              className={({ isActive }) =>
                cx("flex h-8 items-center gap-2.5 rounded-control px-2.5 text-sm font-medium", isActive ? "bg-subtle text-fg" : "text-muted hover:bg-subtle hover:text-fg")
              }
            >
              <Icon name={t.icon} className="size-3.5" />
              {t.label}
            </NavLink>
          ))}
        </nav>
        <div className="min-w-0 flex-1 overflow-auto">
          <div className="mx-auto max-w-3xl px-8 py-7">
            <Outlet />
          </div>
        </div>
      </div>
    </>
  );
}

export function SettingsHeader({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="mb-6">
      <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
      {children && <p className="mt-1 text-sm text-muted">{children}</p>}
    </div>
  );
}
