// Facts the whole site shares: where it lives, the repository, and the
// order of the docs.
export const origin = "https://byname.dev";
export const repo = "https://github.com/unkeyed/byname";

export const title = "Byname: find out when AI answers name you";
export const summary =
  "See when ChatGPT, Google and Claude recommend your product, and who they pick instead. Open source and self-hosted.";

// The docs sidebar, in reading order. scripts/check-docs.mjs fails the build
// when a file under docs/ is missing here or a listed page does not exist.
export const sections = [
  { title: "Get started", pages: ["self-hosting", "deploy-unkey", "configuration", "sign-in"] },
  {
    title: "AI apps",
    pages: ["engines/surfaces", "scores", "other-brands", "engines/treg", "engines/scraping-providers", "engines/model-providers", "engines/gateways"],
  },
  { title: "Connect", pages: ["search-console", "agents", "notifications", "changes", "social"] },
];

// Short sidebar labels where a page's title is long.
export const labels: Record<string, string> = {
  "self-hosting": "Self-host",
  "deploy-unkey": "Deploy on Unkey",
  agents: "Agents",
  "engines/surfaces": "AI apps and accuracy labels",
};
