import type { APIRoute } from "astro";
import { allDocs, description, title, textHeaders } from "../lib/docs";
import { origin, repo, sections } from "../lib/site";

// /llms.txt (https://llmstxt.org): what Byname is and where each doc's
// Markdown lives, for language models and agents.
export const GET: APIRoute = async () => {
  const docs = await allDocs();
  const byId = new Map(docs.map((d) => [d.id, d]));
  const lines = [
    "# Byname",
    "",
    "> Byname is an open source app you run yourself that shows whether AI answers name your product. It asks your buyers' questions in the ChatGPT app, Google AI Mode, Claude Code and nine other AI apps on a schedule, keeps every answer, and records who was named and linked, at what position, and at what cost. It tells you by email, Slack or webhook when you gain or lose an answer, reads Google Search Console to show whether a page change helped, and gives agents an MCP server (OAuth sign-in) and a REST API (scoped API keys). It runs as one container next to Postgres, is MIT licensed, and is provided by Unkey.",
    "",
    `To deploy Byname on Unkey with a coding agent, add Unkey's MCP server (\`claude mcp add --transport http unkey https://mcp.unkey.com\`) and follow the prompt at ${origin}/deploy-prompt.md.`,
    "",
  ];
  for (const s of sections) {
    lines.push(`## ${s.title === "Get started" ? "Docs" : s.title}`, "");
    for (const id of s.pages) {
      const d = byId.get(id)!;
      lines.push(`- [${title(d)}](${origin}/docs/${id}.md): ${description(d)}`);
    }
    lines.push("");
  }
  lines.push(
    "## Optional",
    "",
    `- [Deploy Byname on Unkey](${origin}/deploy-prompt.md): The prompt a coding agent follows to deploy Byname through Unkey's MCP server.`,
    `- [Full documentation](${origin}/llms-full.txt): Every doc above in one Markdown file.`,
    `- [Source code](${repo}): The Go server, React UI, and these docs.`,
    `- [OpenAPI spec](${repo}/blob/main/internal/httpapi/openapi.yaml): Every REST route. A running Byname also serves it at /v1/openapi.yaml.`,
    "",
  );
  return new Response(lines.join("\n"), { headers: textHeaders });
};
