import assert from "node:assert/strict";
import { test } from "node:test";
import cases from "../../internal/config/monthcap.json" with { type: "json" };
import { defaultMonthCap } from "./config/costs.ts";

test("defaultMonthCap matches the cases shared with internal/config", () => {
  assert.ok(cases.length > 0, "monthcap.json has no cases");
  for (const c of cases) assert.equal(defaultMonthCap(c.month_usd), c.cap_usd, `defaultMonthCap(${c.month_usd})`);
});
