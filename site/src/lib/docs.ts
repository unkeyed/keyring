// Turns a docs/*.md entry into what the site serves: its title, a one-line
// description, the HTML with site links and heading anchors, and the raw
// Markdown with absolute links for agents.
import { getCollection, type CollectionEntry } from "astro:content";
import { labels, origin, repo, sections } from "./site";

export type Doc = CollectionEntry<"docs">;

export async function allDocs(): Promise<Doc[]> {
  const docs = await getCollection("docs");
  const order = sections.flatMap((s) => s.pages);
  return docs.sort((a, b) => order.indexOf(a.id) - order.indexOf(b.id));
}

export function headings(doc: Doc) {
  return doc.rendered?.metadata?.headings ?? [];
}

export function title(doc: Doc): string {
  return headings(doc).find((h) => h.depth === 1)?.text ?? doc.id;
}

export function label(doc: Doc): string {
  return labels[doc.id] ?? title(doc).replace(/^(Connect|Set up|Use) /, "");
}

export function sectionOf(id: string): string {
  return sections.find((s) => s.pages.includes(id))?.title ?? "Docs";
}

// description is the first paragraph of prose, without Markdown syntax,
// cut to a sentence-sized length.
export function description(doc: Doc): string {
  const block =
    (doc.body ?? "").split(/\n\s*\n/).find((b) => b.trim() && !/^\s*(#|```|\||>|-|\*|\d+\.|<)/.test(b)) ?? "";
  const text = block.replace(/\[([^\]]+)\]\([^)]+\)/g, "$1").replace(/[`*_]/g, "").replace(/\s+/g, " ").trim();
  return text.length > 200 ? text.slice(0, 197).replace(/\s+\S*$/, "") + "…" : text;
}

// resolve maps a link written in docs/<from>.md to where it points. Another
// doc becomes its page (or its Markdown, for agents); any other repository
// file goes to GitHub. External links and in-page anchors are unchanged.
export function resolve(from: string, href: string, markdown = false): string {
  if (/^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith("#") || href.startsWith("/")) return href;
  const [file, hash] = href.split("#");
  const parts = ["docs", ...from.split("/").slice(0, -1)];
  for (const seg of file.split("/")) {
    if (seg === "..") parts.pop();
    else if (seg && seg !== ".") parts.push(seg);
  }
  const path = parts.join("/");
  const suffix = hash ? `#${hash}` : "";
  if (path.startsWith("docs/") && path.endsWith(".md")) {
    const slug = path.slice("docs/".length, -".md".length);
    return markdown ? `${origin}/docs/${slug}.md${suffix}` : `/docs/${slug}${suffix}`;
  }
  return `${repo}/blob/main/${path}${suffix}`;
}

// html is the rendered page body with site links and a hover anchor on
// every heading below the title.
export function html(doc: Doc): string {
  return (doc.rendered?.html ?? "")
    .replace(/href="([^"]+)"/g, (_, href) => `href="${resolve(doc.id, href)}"`)
    .replace(/<h([2-4]) id="([^"]+)">/g, '<h$1 id="$2"><a class="anchor" href="#$2" aria-hidden="true" tabindex="-1">#</a>');
}

// markdown is the doc's source with every relative link made absolute, so it
// reads correctly outside the repository.
export function markdown(doc: Doc): string {
  return (doc.body ?? "").replace(/\]\(([^)\s]+)\)/g, (_, href) => `](${resolve(doc.id, href, true)})`);
}

export const markdownHeaders = { "Content-Type": "text/markdown; charset=utf-8" };
export const textHeaders = { "Content-Type": "text/plain; charset=utf-8" };
