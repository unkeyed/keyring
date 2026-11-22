// Fails the build when docs/ and the site disagree: a doc missing from the
// sidebar (or listed but missing), a link to a doc that does not exist, or a
// link to a heading that does not exist. Heading ids follow GitHub's rules,
// which the site's rendered pages use too.
//
//   node scripts/check-docs.mjs <docs dir>
import fs from "node:fs";
import path from "node:path";
import GithubSlugger from "github-slugger";
import { sections } from "../src/lib/site.ts";

const dir = process.argv[2] ?? "../docs";
const problems = [];

function list(base, rel = "") {
  return fs.readdirSync(path.join(base, rel), { withFileTypes: true }).flatMap((e) => {
    const p = path.posix.join(rel, e.name);
    if (e.isDirectory()) return list(base, p);
    return e.name.endsWith(".md") ? [p.replace(/\.md$/, "")] : [];
  });
}

const slugs = list(dir);
const listed = sections.flatMap((s) => s.pages);
for (const s of slugs) if (!listed.includes(s)) problems.push(`docs/${s}.md is not in the sidebar in src/lib/site.ts`);
for (const s of listed) if (!slugs.includes(s)) problems.push(`the sidebar lists ${s}, but docs/${s}.md does not exist`);

// prose drops fenced code, where links and headings are only text.
const prose = (src) => src.replace(/^(```|~~~)[\s\S]*?^\1/gm, "");
const plain = (s) => s.replace(/\[([^\]]*)\]\([^)]*\)/g, "$1").replace(/[`*]/g, "").trim();

const ids = new Map();
for (const s of slugs) {
  const slugger = new GithubSlugger();
  const found = new Set();
  for (const m of prose(fs.readFileSync(path.join(dir, s + ".md"), "utf8")).matchAll(/^#{1,6}\s+(.+?)\s*#*\s*$/gm)) {
    found.add(slugger.slug(plain(m[1])));
  }
  ids.set(s, found);
}

for (const s of slugs) {
  const src = prose(fs.readFileSync(path.join(dir, s + ".md"), "utf8")).replace(/`[^`]*`/g, "");
  for (const m of src.matchAll(/\]\(([^)\s]+)\)/g)) {
    const href = m[1];
    if (/^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith("/")) continue;
    const [file, hash] = href.split("#");
    const target = file ? path.posix.normalize(path.posix.join("docs", path.posix.dirname(s), file)) : `docs/${s}.md`;
    if (target.startsWith("..")) {
      problems.push(`docs/${s}.md links outside the repository: ${href}`);
      continue;
    }
    if (!target.startsWith("docs/") || !target.endsWith(".md")) continue; // another repository file, linked on GitHub
    const slug = target.slice(5, -3);
    if (!ids.has(slug)) problems.push(`docs/${s}.md links to ${target}, which does not exist`);
    else if (hash && !ids.get(slug).has(hash)) problems.push(`docs/${s}.md links to #${hash} in ${target}, which has no such heading`);
  }
}

if (problems.length) {
  console.error("check-docs:\n  " + problems.join("\n  "));
  process.exit(1);
}
console.log(`check-docs: ${slugs.length} docs, every link and anchor resolves`);
