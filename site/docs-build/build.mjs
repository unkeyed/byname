// Renders the repository's docs/*.md into the static pages served at
// byname.dev/docs, in the site's own layout.
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
const repo = "https://github.com/unkeyed/byname";
const origin = "https://byname.dev";

// The sidebar, in reading order. Every .md under docs/ must appear here.
const sections = [
  { title: "Get started", pages: ["self-hosting", "deploy-unkey", "configuration", "sign-in"] },
  { title: "Where people ask", pages: ["engines/surfaces", "engines/treg", "engines/scraping-providers", "engines/model-providers", "engines/gateways"] },
  { title: "Connect", pages: ["search-console", "agents", "notifications", "changes"] },
];
const navLabels = {
  "self-hosting": "Self-host",
  "deploy-unkey": "Deploy on Unkey",
  agents: "Agents",
  "engines/surfaces": "Surfaces and fidelity",
};

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


const logo = `<a class="logo" href="/" aria-label="Byname home">by<span class="mk" data-mark="circle" data-pad="6 5">name</span></a>`;

const header = `<a class="skip" href="#main">Skip to content</a>
<header class="mast">
  <div class="wrap mast-in">
    ${logo}
    <nav class="mast-links" aria-label="Site">
      <a href="/#questions">Questions</a>
      <a href="/#record">The record</a>
      <a href="/#evidence">Surfaces</a>
      <a href="/#agents">Agents</a>
      <a href="/docs/" aria-current="page">Docs</a>
      <a href="${repo}">GitHub</a>
    </nav>
    <a class="btn btn-sm" href="/#run">Deploy on Unkey</a>
    <details class="mast-menu">
      <summary aria-label="Menu"><svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true"><path d="M3 6.5h14M3 13.5h14" /></svg></summary>
      <nav class="mast-sheet sheet" aria-label="Menu">
        <a href="/#questions">Questions</a>
        <a href="/#record">The record</a>
        <a href="/#evidence">Surfaces</a>
        <a href="/#agents">Agents</a>
        <a href="/docs/">Docs</a>
        <a href="${repo}">GitHub</a>
        <a class="btn" href="/#run">Deploy on Unkey</a>
      </nav>
    </details>
  </div>
</header>`;

const footer = `<footer class="foot">
  <div class="wrap foot-grid">
    <div>
      ${logo}
      <p class="foot-tag">Find out when AI answers name you, and who they name instead.</p>
      <p class="foot-meta">MIT licensed. Provided by <a href="https://www.unkey.com">Unkey</a>.</p>
    </div>
    <nav aria-labelledby="f-product"><h2 id="f-product">Product</h2><ul>
      <li><a href="/#questions">The questions</a></li>
      <li><a href="/#record">The record</a></li>
      <li><a href="/#evidence">Surfaces</a></li>
      <li><a href="/#search">Search and changes</a></li>
      <li><a href="/#agents">Agents</a></li>
      <li><a href="/#run">Deploy on Unkey</a></li>
    </ul></nav>
    <nav aria-labelledby="f-docs"><h2 id="f-docs">Docs</h2><ul>
      <li><a href="/docs/self-hosting">Self-host</a></li>
      <li><a href="/docs/engines/surfaces">Surfaces and fidelity</a></li>
      <li><a href="/docs/search-console">Search Console</a></li>
      <li><a href="/docs/agents">Agents</a></li>
      <li><a href="/docs/notifications">Notifications</a></li>
      <li><a href="/docs/changes">Changes</a></li>
    </ul></nav>
    <nav aria-labelledby="f-project"><h2 id="f-project">Project</h2><ul>
      <li><a href="${repo}">GitHub</a></li>
      <li><a href="${repo}/blob/main/LICENSE">MIT license</a></li>
      <li><a href="${repo}/issues">Issues</a></li>
      <li><a href="https://www.unkey.com">Unkey</a></li>
    </ul></nav>
  </div>
</footer>`;

function layout({ title, description, url, eyebrow, body, side, toc = "" }) {
  return `<!doctype html>
<html lang="en" class="no-js">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>${escape(title)} · Byname docs</title>
<meta name="description" content="${escape(description)}" />
<meta name="theme-color" content="#f4f1ea" media="(prefers-color-scheme: light)" />
<meta name="theme-color" content="#1a1814" media="(prefers-color-scheme: dark)" />
<link rel="canonical" href="${origin}${url}" />
<meta property="og:type" content="article" />
<meta property="og:site_name" content="Byname" />
<meta property="og:title" content="${escape(title)} · Byname docs" />
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
<link href="https://fonts.googleapis.com/css2?family=Newsreader:ital,opsz,wght@0,6..72,400..600;1,6..72,400..500&family=Instrument+Sans:wght@400..600&family=IBM+Plex+Mono:ital,wght@0,400;0,500;1,400&display=swap" rel="stylesheet" />
<link rel="stylesheet" href="/base.css" />
<link rel="stylesheet" href="/docs/docs.css" />
</head>
<body class="docs">
${header}
<main class="wrap docs-layout" id="main">
  <aside class="docs-side" aria-label="Documentation">${side}</aside>
  <article class="docs-main">
    ${url === "/docs/" ? "" : '<a class="docs-back" href="/docs/">&larr; All docs</a>'}
    <p class="docs-kicker">${escape(eyebrow)}</p>
    ${body}
  </article>
  <aside class="docs-toc" aria-label="On this page">${toc}</aside>
</main>
${footer}
<script src="/app.js" defer></script>
</body>
</html>
`;
}

for (const slug of slugs) fs.mkdirSync(path.join(outDir, path.dirname(slug)), { recursive: true });
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
const lede = "Run Byname yourself or on Unkey, choose where your questions are asked, connect Search Console, and give your agents access.";
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
    description: lede,
    url: "/docs/",
    eyebrow: "Docs",
    body: `<div class="prose"><h1>Documentation</h1><p class="lede-doc">${escape(lede)}</p></div>${index}`,
    side: sidebar(""),
  }),
);
console.log(`docs-build: ${pages.size} pages written to ${outDir}`);
