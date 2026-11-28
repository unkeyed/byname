import type { McpGrade, McpGradeSummary, McpLetter, McpPartial, McpReview, McpReviewStatus, McpStatus, McpTool } from "../../../api";
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

export const reviewOpen = (s: McpReviewStatus) => s === "queued" || s === "running";

export const toolCount = (n: number) => `${n} ${n === 1 ? "tool" : "tools"}`;

/** The tool documentation tile's score: the rules', with the model's beside it once there is one. */
export const docScore = (rules: number, review: Pick<McpReview, "score">) => (review.score == null ? `${rules}` : `Rules ${rules} · AI ${review.score}`);

const stopped: Partial<Record<McpReviewStatus, string>> = {
  capped: "The monthly spend cap stopped the review.",
  refused: "Treg refused a call, such as for an empty balance, so the review stopped.",
  failed: "The review failed.",
};

/** Why the latest review ended early, or null when it did not. */
export const reviewStopped = (r: Pick<McpReview, "status">) => stopped[r.status] ?? null;

/** What a toast says when a review someone watched ends. */
export function reviewDone(r: Pick<McpReview, "status" | "reviewed" | "tools" | "score">): string {
  const done = `${r.reviewed} of ${toolCount(r.tools)} reviewed`;
  if (r.status === "completed") return r.score == null ? `Review done: ${done}.` : `Review done: ${done}, AI score ${r.score}.`;
  return `${reviewStopped(r) ?? "The review stopped."} ${done}.`;
}
