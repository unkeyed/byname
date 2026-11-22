import assert from "node:assert/strict";
import { test } from "node:test";
import cases from "../../internal/money/cases.json" with { type: "json" };
import { money } from "./money.ts";

test("money matches the cases shared with internal/money", () => {
  assert.ok(cases.length > 0, "cases.json has no cases");
  for (const c of cases) {
    assert.equal(money(c.usd), c.text, `money(${c.usd})`);
    assert.equal(money(c.usd, "about"), c.about, `money(${c.usd}, "about")`);
  }
});
