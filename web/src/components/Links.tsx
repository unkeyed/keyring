import { Icon, type IconName } from "../ui";

const kinds: Record<string, { label: string; icon: IconName }> = {
  linkedin: { label: "LinkedIn", icon: "linkedin" },
  x: { label: "X", icon: "xLogo" },
  github: { label: "GitHub", icon: "github" },
  youtube: { label: "YouTube", icon: "youtube" },
  facebook: { label: "Facebook", icon: "facebook" },
  instagram: { label: "Instagram", icon: "link" },
  discord: { label: "Discord", icon: "link" },
  bluesky: { label: "Bluesky", icon: "link" },
  medium: { label: "Medium", icon: "link" },
  tiktok: { label: "TikTok", icon: "link" },
  website: { label: "Website", icon: "globe" },
  careers: { label: "Careers", icon: "briefcase" },
  pricing: { label: "Pricing", icon: "tag" },
  blog: { label: "Blog", icon: "note" },
  contact: { label: "Contact", icon: "mail" },
};

/** "https://www.x.com/stripe/" reads as "x.com/stripe". */
export const shortURL = (u: string) => u.replace(/^https?:\/\/(www\.)?/, "").replace(/\/$/, "");

/** Known kinds first, in the order above; anything else after, by name. */
const order = (kind: string) => {
  const i = Object.keys(kinds).indexOf(kind);
  return i < 0 ? Infinity : i;
};

/**
 * Outside profiles and pages as rows that open in a new tab. The URLs come
 * from enrichment, so they open with no referrer and no window.opener.
 */
export function LinkList({ links }: { links: Record<string, string | undefined> }) {
  const rows = Object.entries(links)
    .filter((e): e is [string, string] => Boolean(e[1]))
    .sort(([a], [b]) => order(a) - order(b) || a.localeCompare(b));
  if (rows.length === 0) return null;
  return (
    <ul className="-mx-1.5">
      {rows.map(([kind, url]) => (
        <li key={kind}>
          <a href={url} target="_blank" rel="noopener noreferrer" title={url} className="group flex h-7 items-center gap-2 rounded-control px-1.5 text-sm transition-colors hover:bg-subtle">
            <Icon name={kinds[kind]?.icon ?? "link"} className="size-3.5 text-muted" />
            <span className="shrink-0">{kinds[kind]?.label ?? kind}</span>
            <span className="min-w-0 flex-1 truncate text-xs text-faint">{shortURL(url)}</span>
            <Icon name="external" className="size-3 text-faint opacity-0 transition-opacity group-hover:opacity-100" />
          </a>
        </li>
      ))}
    </ul>
  );
}
