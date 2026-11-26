// Pages outside the docs: cost, alternatives, and posts. The sitemap,
// llms.txt, the footer, and the indexes all read this list.
export type GuideGroup = "cost" | "alternatives" | "blog";

export type Guide = {
  path: string;
  title: string;
  description: string;
  group: GuideGroup;
  /** Set on posts. */
  date?: string;
};

export const guides: Guide[] = [
  {
    path: "/cost",
    title: "What Byname costs",
    description:
      "Byname is free and open source. You pay your own AI provider calls. One check of 390 answers cost $2.96.",
    group: "cost",
  },
  {
    path: "/alternatives",
    title: "Alternatives",
    description:
      "Fair notes on Peec AI, Profound, OtterlyAI, Semrush's AI Visibility Toolkit, and Ahrefs Brand Radar, and where Byname differs.",
    group: "alternatives",
  },
  {
    path: "/alternatives/peec-ai",
    title: "Byname and Peec AI",
    description:
      "Peec AI is a hosted AI search platform for marketing teams. Byname is free, open source, and self-hosted. Here is what each does.",
    group: "alternatives",
  },
  {
    path: "/alternatives/profound",
    title: "Byname and Profound",
    description:
      "Profound is a hosted AI marketing platform with custom enterprise pricing. Byname is free, open source, and self-hosted.",
    group: "alternatives",
  },
  {
    path: "/alternatives/otterly",
    title: "Byname and OtterlyAI",
    description:
      "OtterlyAI monitors AI search from a hosted subscription. Byname is free, open source, and self-hosted. Here is what each does.",
    group: "alternatives",
  },
  {
    path: "/alternatives/semrush",
    title: "Byname and Semrush AI Visibility",
    description:
      "Semrush sells an AI Visibility Toolkit next to its other toolkits. Byname is free, open source, and self-hosted.",
    group: "alternatives",
  },
  {
    path: "/alternatives/ahrefs",
    title: "Byname and Ahrefs Brand Radar",
    description:
      "Ahrefs Brand Radar studies AI visibility from a large prompt index and from prompts you write. Byname is free, open source, and self-hosted.",
    group: "alternatives",
  },
  {
    path: "/blog",
    title: "Posts",
    description: "Notes on AI visibility, search, social, and what a developer tools startup can measure.",
    group: "blog",
  },
  {
    path: "/blog/seo-social-and-ai-visibility",
    title: "What tool tracks SEO, social and AI visibility in one place?",
    description:
      "Byname does. One install reads AI answers, Google Search Console, Bing Webmaster Tools, and posts that name you on seven social networks.",
    group: "blog",
    date: "2026-11-26",
  },
  {
    path: "/blog/developer-tools-startup",
    title: "How can a developer tools startup see what AI assistants say about it?",
    description:
      "Run Byname and ask the questions your buyers ask, including in Claude Code and Codex CLI. Each brand has an MCP server your agent can read.",
    group: "blog",
    date: "2026-11-26",
  },
  {
    path: "/blog/recommended-by-chatgpt",
    title: "How do I get my product recommended by ChatGPT?",
    description:
      "Publish a page that answers the buyer's question in the first paragraph, and check later answers to see if they cite it and name you.",
    group: "blog",
    date: "2026-11-26",
  },
];

export function guide(path: string): Guide {
  const found = guides.find((g) => g.path === path);
  if (!found) throw new Error(`no guide for ${path}`);
  return found;
}

/** Comparison pages, or posts, without the hub. */
export function childGuides(group: Exclude<GuideGroup, "cost">): Guide[] {
  return guides.filter((g) => g.group === group && g.path !== `/${group}`);
}
