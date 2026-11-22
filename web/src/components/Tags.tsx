import { useId, useState, type FormEvent, type ReactNode } from "react";
import type { Tag } from "../api";
import { useTags } from "../queries";
import { Badge, cx, Icon, inputClass } from "../ui";
import { SectionTitle } from "./RecordLayout";

export type TagPatch = { add_tags?: Tag[]; remove_tags?: Tag[] };

/**
 * The Tags section of a contact or company: removable badges and an input that
 * suggests tags already in use. lead goes before the editable tags, such as a
 * contact's tags from its company; note goes under them.
 */
export function TagEditor({ tags, onChange, lead, note }: { tags: Tag[]; onChange: (p: TagPatch) => void; lead?: ReactNode; note?: ReactNode }) {
  const all = useTags();
  const listId = useId();
  const [draft, setDraft] = useState("");

  const add = (e: FormEvent) => {
    e.preventDefault();
    const t = draft.trim().toLowerCase();
    setDraft("");
    if (t && !tags.includes(t)) onChange({ add_tags: [t] });
  };

  return (
    <>
      <SectionTitle className="mt-6">Tags</SectionTitle>
      <div className="flex flex-wrap items-center gap-1.5">
        {lead}
        {tags.map((t) => (
          <Badge
            key={t}
            tone="outline"
            title={t}
            onRemove={() => onChange({ remove_tags: [t] })}
            removeLabel={`Remove tag ${t}`}
            className="transition-[opacity,scale] duration-150 starting:scale-90 starting:opacity-0"
          >
            <span className="truncate">{t}</span>
          </Badge>
        ))}
        <form onSubmit={add} className={cx("min-w-24 flex-1", tags.length === 0 && !lead && "-ml-[7px]")}>
          <input
            aria-label="Add tag"
            placeholder="Add tag…"
            list={listId}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            className={cx(inputClass, "h-6 border-transparent bg-transparent px-1.5 text-xs hover:border-line-strong")}
          />
          <datalist id={listId}>
            {all.data?.filter((t) => !tags.includes(t.tag)).map((t) => <option key={t.tag} value={t.tag} />)}
          </datalist>
        </form>
      </div>
      {note && <p className="mt-1.5 text-xs text-faint">{note}</p>}
    </>
  );
}

function TagBadge({ tag, company, className }: { tag: Tag; company?: boolean; className?: string }) {
  return (
    <Badge tone={company ? "neutral" : "outline"} title={company ? `${tag} (from the company)` : tag} className={cx("min-w-0", className)}>
      {company && <Icon name="building" className="size-3 shrink-0 text-faint" />}
      <span className="truncate">{tag}</span>
    </Badge>
  );
}

/**
 * A list row's tags: one up to xl, two from there, then a count of the rest,
 * so no tag is ever cut to a sliver. Tags from the contact's company come
 * first, marked with a building.
 */
export function TagCell({ tags, companyTags = [] }: { tags: Tag[]; companyTags?: Tag[] }) {
  const all = [...companyTags.map((tag) => ({ tag, company: true })), ...tags.filter((t) => !companyTags.includes(t)).map((tag) => ({ tag, company: false }))];
  if (all.length === 0) return null;
  return (
    <div className="flex min-w-0 items-center gap-1" title={all.map((t) => (t.company ? `${t.tag} (from the company)` : t.tag)).join(", ")}>
      <TagBadge {...all[0]} />
      {all[1] && <TagBadge {...all[1]} className="max-xl:hidden" />}
      {all.length > 1 && <span className="shrink-0 text-xs text-faint xl:hidden">+{all.length - 1}</span>}
      {all.length > 2 && <span className="hidden shrink-0 text-xs text-faint xl:inline">+{all.length - 2}</span>}
    </div>
  );
}
