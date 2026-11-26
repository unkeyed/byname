import assert from "node:assert/strict";
import { test } from "node:test";
import cases from "../../internal/notify/changes.json" with { type: "json" };
import { changeLine } from "./lines.ts";

test("changeLine matches the cases shared with internal/notify", () => {
  assert.ok(cases.length > 0, "changes.json has no cases");
  for (const c of cases) assert.equal(changeLine(c.previous_run_id, c.gained, c.lost, c.moved), c.text, JSON.stringify(c));
});
