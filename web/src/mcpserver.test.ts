import assert from "node:assert/strict";
import { test } from "node:test";
import { duration, letterColor, letterTone, partialNote, scoresOverTime, shortfalls, statusTone } from "./pages/settings/mcpserver/model.ts";

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

const category = (score: number | null, note = "") => ({ id: "metadata" as const, title: "Server metadata", score, note, checks: [] });

test("a partial grade says why, and names the token when sign-in blocked it", () => {
  assert.equal(partialNote({ complete: true, categories: [category(0, "Needs a token to grade")] }), null);
  assert.equal(
    partialNote({ complete: false, categories: [category(100), category(0, "Needs a token to grade")] }),
    "Partial grade. Sign-in blocked the tool checks. An admin can add a token to grade the rest.",
  );
  assert.equal(
    partialNote({ complete: false, categories: [category(0, "No session")] }),
    "Partial grade. No MCP session started, so the metadata and tool checks could not run. Connection says why.",
  );
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
