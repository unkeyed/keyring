import { jobStates } from "../../api";
import { useStatus } from "../../queries";
import { CodeBlock, cx, enrichmentStates, ErrorNote, Icon, PageSpinner, RelativeTime } from "../../ui";
import { SettingsHeader, SettingsSubhead } from "./Settings";

export function Enrichment() {
  const status = useStatus({ poll: true });
  const s = status.data;

  return (
    <>
      <SettingsHeader title="Enrichment">
        uncrm looks up people and companies on context.dev: job titles, locations, company logos, sizes and industries. Free-mail addresses like Gmail are
        skipped. Fields a person edited are never overwritten.
      </SettingsHeader>
      {status.isPending && <PageSpinner />}
      <ErrorNote error={status.error} />

      {s && (
        <>
          {s.enrichment_halt ? (
            <div className="flex items-start gap-3 rounded-surface border border-warn/40 bg-warn-soft p-4 text-warn">
              <Icon name="alert" className="mt-0.5 size-4" />
              <div className="min-w-0 text-sm [overflow-wrap:anywhere]">
                <div className="font-semibold">Paused: context.dev rejected the request</div>
                <div className="mt-0.5">{s.enrichment_halt.reason}</div>
                <div className="mt-2 text-xs opacity-80">
                  Workers try again <RelativeTime iso={s.enrichment_halt.until} />. If the plan ran out, upgrade it on context.dev and enrichment resumes
                  then. If the key was revoked, set a new one below. Queued records wait; none are lost.
                </div>
              </div>
            </div>
          ) : (
            <div className="flex items-center gap-2 rounded-surface border border-line bg-surface p-4 text-sm">
              <span className={cx("size-2 shrink-0 rounded-full", s.enrichment_enabled ? "bg-success" : "bg-faint")} />
              <span className="font-medium">{s.enrichment_enabled ? "On." : "Off."}</span>
              <span className="text-muted">{s.enrichment_enabled ? "New sign-ups are looked up within seconds." : "Records queue up and are enriched once a key is set."}</span>
            </div>
          )}

          <div className="mt-4 grid grid-cols-2 gap-px overflow-hidden rounded-surface border border-line bg-line sm:grid-cols-5">
            {jobStates.map((j) => (
              <div key={j} className="bg-surface px-4 py-3 last:col-span-2 sm:last:col-span-1">
                <div className="truncate text-xs text-muted" title={enrichmentStates[j].help}>
                  {enrichmentStates[j].label}
                </div>
                <div className="mt-0.5 text-lg font-semibold tabular-nums">{(s.jobs?.[j] ?? 0).toLocaleString()}</div>
              </div>
            ))}
          </div>

          {(!s.enrichment_enabled || s.enrichment_halt) && (
            <section className="mt-8 space-y-3 text-sm">
              <SettingsSubhead className="mb-0">{s.enrichment_enabled ? "Replace the key" : "Turn it on"}</SettingsSubhead>
              <ol className="list-decimal space-y-2 pl-5 text-muted">
                <li>
                  Create an API key at{" "}
                  <a href="https://context.dev" target="_blank" rel="noreferrer noopener" className="text-fg underline underline-offset-2 hover:opacity-70">
                    context.dev
                  </a>
                  .
                </li>
                <li>
                  Set it as <code className="font-mono text-fg">CONTEXT_DEV_API_KEY</code> on this deployment. On Unkey Deploy that is an environment variable on
                  the project; with Docker Compose, add it to <code className="font-mono text-fg">.env</code>.
                </li>
                <li>Restart uncrm. Everything queued is enriched right away.</li>
              </ol>
              <CodeBlock code="CONTEXT_DEV_API_KEY=ctx_..." />
            </section>
          )}
        </>
      )}
    </>
  );
}
