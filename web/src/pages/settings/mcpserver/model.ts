import type { McpGrade, McpGradeSummary, McpLetter, McpPartial, McpStatus, McpTool } from "../../../api";
import type { Tone } from "../../../ui";

export const statusTone: Record<McpStatus, Tone> = { pass: "success", partial: "warn", fail: "danger", skipped: "neutral" };

export const statusLabel: Record<McpStatus, string> = { pass: "Pass", partial: "Partial", fail: "Fail", skipped: "Skipped" };

const letterTones: Record<McpLetter, Tone> = { "A+": "success", A: "success", B: "info", C: "warn", D: "warn", F: "danger" };

export const letterTone = (l: McpLetter): Tone => letterTones[l];

export const letterColor = (l: McpLetter) => `var(--${letterTones[l]})`;

const partialNotes: Record<McpPartial, string | null> = {
  "": null,
  needs_token: "Partial grade. Sign-in blocked the tool checks. An admin can add a token to grade the rest.",
  no_session: "Partial grade. No MCP session started, so the metadata and tool checks could not run. Connection says why.",
  no_tools: "Partial grade. Some checks could not run.",
};

export const partialNote = (p: McpPartial) => partialNotes[p];

export const transportLabel: Record<McpGrade["server"]["transport"], string> = { streamable_http: "Streamable HTTP", sse: "SSE (legacy)", none: "None found" };

const hintLabels: Record<string, string> = { read_only: "Read-only", destructive: "Destructive", idempotent: "Idempotent", open_world: "Open world" };

export const hintLabel = (h: string) => hintLabels[h] ?? h;

export function duration(ms: number | null): string {
  if (ms == null) return "None";
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`;
}

export const shortfalls = (t: Pick<McpTool, "marks">) => t.marks.filter((m) => m.status === "fail" || m.status === "partial");

export const scoresOverTime = (history: Pick<McpGradeSummary, "score">[]) => history.map((h) => h.score).reverse();
