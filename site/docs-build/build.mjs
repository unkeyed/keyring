// Renders the repository's docs/*.md into the static pages served at
// kyring.dev/docs, in the site's own layout.
//
//   node build.mjs <docs dir> <out dir>
//
// Links between docs become /docs/... URLs, links to other repository files
// point at GitHub, and the build fails on a link to a missing page or a
// missing heading, so the site never ships a broken docs link.
import fs from "node:fs";
import path from "node:path";
import { Marked } from "marked";
import { gfmHeadingId, getHeadingList } from "marked-gfm-heading-id";

const [docsDir, outDir] = process.argv.slice(2);
if (!docsDir || !outDir) {
  console.error("usage: node build.mjs <docs dir> <out dir>");
  process.exit(2);
}
const repo = "https://github.com/unkeyed/keyring";
const origin = "https://kyring.dev";

// The sidebar, in reading order. Every .md under docs/ must appear here.
const sections = [
  { title: "Get started", pages: ["self-hosting", "deploy-unkey", "configuration", "sign-in", "agents"] },
  { title: "Auth providers", pages: ["providers/clerk", "providers/workos", "providers/better-auth", "providers/supabase", "providers/auth0", "providers/stytch"] },
];
const navLabels = { "self-hosting": "Self-host", "deploy-unkey": "Deploy on Unkey", agents: "Agents" };

const escape = (s) => s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);

function listDocs(dir, base = "") {
  return fs.readdirSync(path.join(dir, base), { withFileTypes: true }).flatMap((e) => {
    const rel = path.posix.join(base, e.name);
    if (e.isDirectory()) return listDocs(dir, rel);
    return e.name.endsWith(".md") ? [rel.replace(/\.md$/, "")] : [];
  });
}

const slugs = listDocs(docsDir).sort();
const listed = sections.flatMap((s) => s.pages);
const unlisted = slugs.filter((s) => !listed.includes(s));
const missing = listed.filter((s) => !slugs.includes(s));
if (unlisted.length || missing.length) {
  console.error(`docs-build: sidebar and docs/ disagree. Not in the sidebar: ${unlisted.join(", ") || "none"}. Missing files: ${missing.join(", ") || "none"}.`);
  process.exit(1);
}

const problems = [];
const pending = []; // links to check once every page's headings are known
const pages = new Map();

for (const slug of slugs) {
  const source = fs.readFileSync(path.join(docsDir, slug + ".md"), "utf8");
  const marked = new Marked(gfmHeadingId());
  marked.use({
    renderer: {
      link({ href, title, tokens }) {
        const text = this.parser.parseInline(tokens);
        const target = resolveLink(slug, href);
        const external = /^https?:/.test(target);
        const attrs = title ? ` title="${escape(title)}"` : "";
        return `<a href="${escape(target)}"${attrs}${external ? ' rel="noopener"' : ""}>${text}</a>`;
      },
    },
  });
  // gfmHeadingId gives each heading GitHub's id and records it while
  // rendering, so read the list after parse.
  const rendered = marked.parse(source);
  const headings = getHeadingList();
  // A hover anchor on every heading below the title, like GitHub's.
  const html = rendered.replace(/<h([2-6]) id="([^"]+)">/g, '<h$1 id="$2"><a class="anchor" href="#$2" aria-hidden="true" tabindex="-1">#</a>');
  const h1 = headings.find((h) => h.level === 1);
  const title = h1 ? h1.raw : slug;
  const firstPara = source.split(/\n\s*\n/).find((b) => b.trim() && !/^\s*(#|```|\||>|-|\d+\.)/.test(b)) ?? "";
  const intro = firstPara.replace(/\[([^\]]+)\]\([^)]+\)/g, "$1").replace(/[`*_]/g, "").replace(/\s+/g, " ").trim();
  pages.set(slug, {
    title,
    description: intro.length > 200 ? intro.slice(0, 197).replace(/\s+\S*$/, "") + "…" : intro,
    html,
    ids: new Set(headings.map((h) => h.id)),
    toc: headings.filter((h) => h.level === 2),
  });
}

for (const { from, slug, hash } of pending) {
  const page = pages.get(slug);
  if (!page) problems.push(`${from}.md links to docs/${slug}.md, which does not exist`);
  else if (hash && !page.ids.has(hash)) problems.push(`${from}.md links to #${hash} in ${slug}.md, which has no such heading`);
}
if (problems.length) {
  console.error("docs-build: broken links\n  " + problems.join("\n  "));
  process.exit(1);
}

// resolveLink maps a link in docs/<from>.md to a site URL. Another doc
// becomes /docs/<slug>; any other repository file goes to GitHub.
function resolveLink(from, href) {
  if (/^[a-z]+:/i.test(href)) return href;
  if (href.startsWith("#")) {
    pending.push({ from, slug: from, hash: href.slice(1) });
    return href;
  }
  const [file, hash = ""] = href.split("#");
  const repoPath = path.posix.normalize(path.posix.join("docs", path.posix.dirname(from), file));
  if (repoPath.startsWith("docs/") && repoPath.endsWith(".md")) {
    const slug = repoPath.slice("docs/".length, -".md".length);
    pending.push({ from, slug, hash });
    return `/docs/${slug}${hash ? "#" + hash : ""}`;
  }
  if (repoPath.startsWith("..")) {
    problems.push(`${from}.md links outside the repository: ${href}`);
    return href;
  }
  return `${repo}/blob/main/${repoPath}${hash ? "#" + hash : ""}`;
}

function label(slug) {
  return navLabels[slug] ?? pages.get(slug).title.replace(/^(Connect|Set up|Use) /, "");
}

function sidebar(active) {
  return sections
    .map(
      (s) => `<div class="side-group"><p class="side-title">${escape(s.title)}</p><ul>${s.pages
        .map((p) => `<li><a href="/docs/${p}"${p === active ? ' aria-current="page"' : ""}>${escape(label(p))}</a></li>`)
        .join("")}</ul></div>`,
    )
    .join("");
}

const header = `<header class="nav">
  <div class="wrap">
    <div class="nav-bar">
      <a class="brand" href="/" aria-label="Keyring home"><span class="brand-mark">K</span>keyring</a>
      <nav class="nav-links" aria-label="Site">
        <a href="/#features">Features</a>
        <a href="/#agents">Agents</a>
        <a href="/#deploy">Deploy on Unkey</a>
        <a href="/docs/" aria-current="page">Docs</a>
      </nav>
    </div>
    <a class="btn btn-primary btn-github" href="${repo}">
      <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 .5C5.65.5.5 5.65.5 12a11.5 11.5 0 0 0 7.86 10.92c.58.1.79-.25.79-.56v-2c-3.2.7-3.87-1.37-3.87-1.37-.52-1.33-1.28-1.68-1.28-1.68-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.19 1.76 1.19 1.03 1.76 2.69 1.25 3.35.96.1-.75.4-1.25.73-1.54-2.55-.29-5.24-1.28-5.24-5.68 0-1.26.45-2.28 1.19-3.09-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.17 1.18a11 11 0 0 1 5.77 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.59.23 2.76.11 3.05.74.81 1.19 1.83 1.19 3.09 0 4.41-2.69 5.39-5.25 5.67.41.36.78 1.06.78 2.14v3.17c0 .31.21.67.8.56A11.5 11.5 0 0 0 23.5 12C23.5 5.65 18.35.5 12 .5Z"/></svg>
      <span>GitHub</span>
    </a>
  </div>
</header>`;

const footer = `<footer>
  <div class="wrap">
    <a class="brand brand-light" href="/" aria-label="Keyring home"><span class="brand-mark">K</span>keyring</a>
    <nav aria-label="Footer">
      <a href="${repo}">GitHub</a>
      <a href="/docs/">Docs</a>
      <a href="${repo}/blob/main/LICENSE">MIT license</a>
    </nav>
    <p class="provided">Provided by <a href="https://www.unkey.com">Unkey</a></p>
  </div>
</footer>`;

function layout({ title, description, url, eyebrow, body, side, toc = "" }) {
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>${escape(title)} · Keyring docs</title>
<meta name="description" content="${escape(description)}" />
<meta name="theme-color" content="#040406" />
<link rel="canonical" href="${origin}${url}" />
<meta property="og:type" content="article" />
<meta property="og:site_name" content="Keyring" />
<meta property="og:title" content="${escape(title)} · Keyring docs" />
<meta property="og:description" content="${escape(description)}" />
<meta property="og:url" content="${origin}${url}" />
<meta property="og:image" content="${origin}/og.png" />
<meta property="og:image:width" content="1200" />
<meta property="og:image:height" content="630" />
<meta name="twitter:card" content="summary_large_image" />
<meta name="twitter:image" content="${origin}/og.png" />
<link rel="icon" href="/favicon.svg" type="image/svg+xml" />
<link rel="preconnect" href="https://fonts.googleapis.com" />
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&family=JetBrains+Mono:wght@400;500&family=Manrope:wght@400;500&display=swap" rel="stylesheet" />
<link rel="stylesheet" href="/base.css" />
<link rel="stylesheet" href="/docs/docs.css" />
</head>
<body class="docs">
${header}
<main class="wrap docs-layout">
  <aside class="docs-side" aria-label="Documentation">${side}</aside>
  <article class="docs-main">
    ${url === "/docs/" ? "" : '<a class="docs-back" href="/docs/">&larr; All docs</a><br />'}
    <span class="eyebrow"><span class="glow"><i></i></span>${escape(eyebrow)}</span>
    ${body}
  </article>
  <aside class="docs-toc" aria-label="On this page">${toc}</aside>
</main>
${footer}
</body>
</html>
`;
}

fs.mkdirSync(path.join(outDir, "providers"), { recursive: true });
fs.copyFileSync(new URL("./docs.css", import.meta.url), path.join(outDir, "docs.css"));

for (const [slug, page] of pages) {
  const section = sections.find((s) => s.pages.includes(slug)).title;
  const toc = page.toc.length > 1
    ? `<nav class="toc"><p class="side-title">On this page</p><ul>${page.toc.map((h) => `<li><a href="#${h.id}">${h.text}</a></li>`).join("")}</ul></nav>`
    : "";
  const body = `<div class="prose">${page.html}</div>
    <div class="docs-edit"><a href="${repo}/blob/main/docs/${slug}.md">Edit this page on GitHub</a></div>`;
  const html = layout({ title: page.title, description: page.description, url: `/docs/${slug}`, eyebrow: section, body, side: sidebar(slug), toc });
  fs.writeFileSync(path.join(outDir, slug + ".html"), html);
}

// /docs/: every page with its first sentence, grouped like the sidebar.
const index = sections
  .map(
    (s) => `<h2 class="index-title">${escape(s.title)}</h2><div class="index-grid">${s.pages
      .map((p) => `<a class="index-card" href="/docs/${p}"><span class="index-name">${escape(label(p))}</span><span class="index-desc">${escape(pages.get(p).description)}</span></a>`)
      .join("")}</div>`,
  )
  .join("");
fs.writeFileSync(
  path.join(outDir, "index.html"),
  layout({
    title: "Documentation",
    description: "Run Keyring yourself or on Unkey, connect your auth provider, and give your agents access.",
    url: "/docs/",
    eyebrow: "Docs",
    body: `<div class="prose"><h1>Documentation</h1><p class="lede-doc">Run Keyring yourself or on Unkey, connect your auth provider, and give your agents access.</p></div>${index}`,
    side: sidebar(""),
  }),
);
console.log(`docs-build: ${pages.size} pages written to ${outDir}`);
