import assert from "node:assert/strict";
import { test } from "node:test";
import { docScore, duration, letterColor, letterTone, partialNote, reviewDone, reviewOpen, reviewStopped, scoresOverTime, shortfalls, statusTone } from "./pages/settings/mcpserver/model.ts";

test("each check status has the tone its badge shows", () => {
  assert.deepEqual(statusTone, { pass: "success", partial: "warn", fail: "danger", skipped: "neutral" });
});

test("letters run from success to danger", () => {
  assert.equal(letterTone("A+"), "success");
  assert.equal(letterTone("B"), "info");
  assert.equal(letterTone("D"), "warn");
  assert.equal(letterTone("F"), "danger");
  assert.equal(letterColor("F"), "var(--danger)");
});

test("a partial grade says why, and names the token when sign-in blocked it", () => {
  assert.equal(partialNote(""), null);
  assert.equal(partialNote("needs_token"), "Partial grade. Sign-in blocked the tool checks. An admin can add a token to grade the rest.");
  assert.equal(partialNote("no_session"), "Partial grade. No MCP session started, so the metadata and tool checks could not run. Connection says why.");
  assert.equal(partialNote("no_tools"), "Partial grade. Some checks could not run.");
});

test("negotiation time reads in ms under a second", () => {
  assert.equal(duration(412), "412 ms");
  assert.equal(duration(1530), "1.5 s");
  assert.equal(duration(null), "None");
});

test("a tool's shortfalls are its fails and partials", () => {
  const marks = [
    { check: "a", status: "pass" as const, finding: "" },
    { check: "b", status: "partial" as const, finding: "" },
    { check: "c", status: "skipped" as const, finding: "" },
    { check: "d", status: "fail" as const, finding: "" },
  ];
  assert.deepEqual(
    shortfalls({ marks }).map((m) => m.check),
    ["b", "d"],
  );
});

test("history draws oldest first", () => {
  assert.deepEqual(scoresOverTime([{ score: 90 }, { score: 70 }, { score: 40 }]), [40, 70, 90]);
});

test("the documentation tile shows the AI score beside the rules' once there is one", () => {
  assert.equal(docScore(75, { score: null }), "75");
  assert.equal(docScore(75, { score: 64 }), "Rules 75 · AI 64");
});

test("a review is open while queued or running", () => {
  assert.equal(reviewOpen("queued"), true);
  assert.equal(reviewOpen("running"), true);
  assert.equal(reviewOpen("completed"), false);
  assert.equal(reviewOpen("none"), false);
});

test("a review that ended early says why, and the toast says how far it got", () => {
  assert.equal(reviewStopped({ status: "completed" }), null);
  assert.equal(reviewStopped({ status: "capped" }), "The monthly spend cap stopped the review.");
  assert.equal(reviewDone({ status: "completed", reviewed: 3, tools: 3, score: 71 }), "Review done: 3 of 3 tools reviewed, AI score 71.");
  assert.equal(reviewDone({ status: "completed", reviewed: 0, tools: 1, score: null }), "Review done: 0 of 1 tool reviewed.");
  assert.equal(reviewDone({ status: "refused", reviewed: 1, tools: 4, score: 60 }), "Treg refused a call, such as for an empty balance, so the review stopped. 1 of 4 tools reviewed.");
});
