import type { ReactNode } from "react";
import { Navigate, NavLink, Outlet, type RouteObject } from "react-router";
import { cx, PageHeader } from "../../ui";
import { Agents } from "./Agents";
import { ApiKeys } from "./ApiKeys";
import { Enrichment } from "./Enrichment";
import { Providers } from "./Providers";
import { Stages } from "./Stages";
import { Team } from "./Team";

const tabs: { path: string; label: string; element: ReactNode }[] = [
  { path: "providers", label: "Providers", element: <Providers /> },
  { path: "keys", label: "API keys", element: <ApiKeys /> },
  { path: "agents", label: "Agents & MCP", element: <Agents /> },
  { path: "team", label: "Team", element: <Team /> },
  { path: "stages", label: "Stages", element: <Stages /> },
  { path: "enrichment", label: "Enrichment", element: <Enrichment /> },
];

export const settingsRoutes: RouteObject[] = [
  { index: true, element: <Navigate to="providers" replace /> },
  ...tabs.map((t) => ({ path: t.path, element: t.element })),
];

/**
 * Sections are tabs above the content: an underlined row from md up, and
 * wrapping pills on a phone, so every section stays one tap away.
 */
export function Settings() {
  return (
    <>
      <PageHeader title="Settings" />
      <div className="min-h-0 flex-1 overflow-auto">
        <nav aria-label="Settings" className="z-10 bg-bg md:sticky md:top-0 md:border-b md:border-line">
          <div className="mx-auto flex max-w-3xl flex-wrap gap-1 px-4 pt-4 md:flex-nowrap md:gap-0 md:px-[1.375rem] md:pt-1">
            {tabs.map((t) => (
              <NavLink
                key={t.path}
                to={t.path}
                className={({ isActive }) =>
                  cx(
                    "flex h-8 items-center rounded-control px-2.5 text-sm font-medium whitespace-nowrap transition-colors",
                    "md:-mb-px md:h-10 md:rounded-none md:border-b-2 md:px-2.5",
                    isActive ? "bg-subtle-strong text-fg md:border-fg md:bg-transparent" : "text-muted hover:bg-subtle hover:text-fg md:border-transparent md:hover:bg-transparent",
                  )
                }
              >
                {t.label}
              </NavLink>
            ))}
          </div>
        </nav>
        <div className="mx-auto max-w-3xl px-4 py-6 pb-16 md:px-8 md:py-8">
          <Outlet />
        </div>
      </div>
    </>
  );
}

export function SettingsHeader({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="mb-6">
      <h2 className="text-lg font-semibold">{title}</h2>
      {children && <p className="mt-1 text-sm text-muted">{children}</p>}
    </div>
  );
}

export function SettingsSubhead({ children, className }: { children: ReactNode; className?: string }) {
  return <h3 className={cx("mb-3 text-sm font-semibold", className)}>{children}</h3>;
}
