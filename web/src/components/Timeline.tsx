import type { ReactNode } from "react";
import type { Activity, ActivityData, ActivityKind, FieldChanges, FieldDiff, TagChange } from "../api";
import { Icon, RelativeTime, type IconName } from "../ui";

const fieldNames: Record<string, string> = {
  name: "name",
  title: "title",
  location: "location",
  bio: "bio",
  linkedin_url: "LinkedIn",
  company: "company",
  stage: "stage",
  tags: "tags",
  description: "description",
  employee_range: "employees",
  industry: "industry",
  country: "country",
};
const fieldName = (f: string) => fieldNames[f] ?? f.replaceAll("_", " ");

const show = (v: unknown) => (v == null || v === "" ? "empty" : Array.isArray(v) ? v.join(", ") : String(v));

function describeUpdate(data: Extract<ActivityData, { kind: "contact_updated" }>["data"]): ReactNode {
  if ("reset" in data && typeof data.reset === "string") {
    return (
      <>
        reset <b>{fieldName(data.reset)}</b> to automatic
      </>
    );
  }
  const diffs = Object.entries(data as FieldChanges);
  if (diffs.length === 1) {
    const [f, d] = diffs[0];
    if (f === "tags") return describeTags(d as TagChange);
    const diff = d as FieldDiff;
    return (
      <>
        changed <b>{fieldName(f)}</b> from <i className="not-italic text-muted">{show(diff.from)}</i> to <b>{show(diff.to)}</b>
      </>
    );
  }
  return <>changed {diffs.map(([f]) => fieldName(f)).join(", ")}</>;
}

function describeTags({ added = [], removed = [] }: TagChange): ReactNode {
  return (
    <>
      {added.length > 0 && (
        <>
          tagged <b>{added.join(", ")}</b>
        </>
      )}
      {added.length > 0 && removed.length > 0 && " and "}
      {removed.length > 0 && (
        <>
          removed <b>{removed.join(", ")}</b>
        </>
      )}
    </>
  );
}

type Kind<K extends ActivityKind> = {
  icon: IconName;
  /** False when the actor is the provider or the enrichment worker, which the sentence already names. */
  byline: boolean;
  text: (d: Extract<ActivityData, { kind: K }>["data"]) => ReactNode;
};

/** One entry per activity kind, so a new kind is a type error here until it has a sentence. */
const kinds: { [K in ActivityKind]: Kind<K> } = {
  signed_up: { icon: "login", byline: false, text: (d) => <>Signed up through <b>{d.provider}</b></> },
  provider_deleted: { icon: "trash", byline: false, text: (d) => <>Deleted in <b>{d.provider}</b></> },
  contact_updated: { icon: "pencil", byline: true, text: describeUpdate },
  company_updated: { icon: "building", byline: true, text: describeUpdate },
  note_added: { icon: "note", byline: true, text: () => <>added a note</> },
  contacts_merged: { icon: "merge", byline: true, text: (d) => <>merged in <b>{d.drop_email}</b></> },
  enriched: {
    icon: "sparkles",
    byline: false,
    text: (d) => (d.changed?.length ? <>Enrichment filled in {d.changed.map(fieldName).join(", ")}</> : <>Enrichment found nothing new</>),
  },
};

function Entry({ a }: { a: Activity }) {
  // A kind this build does not know (a newer server) still gets a line.
  const k = kinds[a.kind] as Kind<ActivityKind> | undefined;
  return (
    <li className="relative flex gap-3">
      <span className="z-[1] grid size-6 shrink-0 place-items-center rounded-full border border-line bg-surface text-muted">
        <Icon name={k?.icon ?? "alert"} className="size-3" />
      </span>
      <p className="min-w-0 flex-1 pt-0.5 text-sm leading-5 [overflow-wrap:anywhere]">
        {k?.byline !== false && (
          <>
            <span className="font-medium">{a.actor.label || a.actor.id}</span>
            {a.actor.kind === "api_key" && <span className="ml-1 text-xs text-faint">(API key)</span>}{" "}
          </>
        )}
        {k ? (k.text as (d: unknown) => ReactNode)(a.data) : a.kind.replaceAll("_", " ")}
        <RelativeTime iso={a.occurred_at} className="ml-2 text-xs text-faint" />
      </p>
    </li>
  );
}

export function Timeline({ items }: { items: Activity[] }) {
  if (items.length === 0) return <p className="text-sm text-faint">Nothing yet.</p>;
  return (
    <ol className="relative space-y-4 before:absolute before:top-2 before:bottom-2 before:left-[11px] before:w-px before:bg-line">
      {items.map((a) => (
        <Entry key={a.id} a={a} />
      ))}
    </ol>
  );
}
