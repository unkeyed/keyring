import { Link } from "react-router";
import type { JobState } from "../api";
import { useStatus } from "../queries";
import { Icon, RelativeTime } from "../ui";

/**
 * Why a record is not enriched yet, when the answer is instance-wide: no
 * CONTEXT_DEV_API_KEY, or context.dev rejected the key and every worker paused.
 */
export function EnrichmentNote({ state }: { state: JobState }) {
  const status = useStatus();
  if (state !== "pending" && state !== "failed") return null;
  const s = status.data;
  if (!s) return null;

  if (s.enrichment_halt) {
    return (
      <p className="flex items-start gap-1.5 rounded-control bg-warn-soft px-2.5 py-1.5 text-xs text-warn">
        <Icon name="alert" className="mt-px size-3.5" />
        <span>
          Enrichment is paused: {s.enrichment_halt.reason}. It retries <RelativeTime iso={s.enrichment_halt.until} />.{" "}
          <Link to="/settings/enrichment" className="underline">
            Details
          </Link>
        </span>
      </p>
    );
  }
  if (!s.enrichment_enabled && state === "pending") {
    return (
      <p className="flex items-start gap-1.5 rounded-control bg-subtle px-2.5 py-1.5 text-xs text-muted">
        <Icon name="sparkles" className="mt-px size-3.5" />
        <span>
          Enrichment is off, so this stays queued.{" "}
          <Link to="/settings/enrichment" className="text-fg underline underline-offset-2 hover:opacity-70">
            Turn it on
          </Link>
        </span>
      </p>
    );
  }
  return null;
}
