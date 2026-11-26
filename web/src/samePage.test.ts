import assert from "node:assert/strict";
import { test } from "node:test";
import { samePage } from "./samePage.ts";

const routes = [
  { path: "/onboarding/:step?" },
  {
    children: [
      { index: true },
      { path: "ai", children: [{ index: true }, { path: "questions" }, { path: "competitors" }] },
      { path: "settings", children: [{ index: true }, { path: "team" }, { path: "keys" }] },
      { path: "notifications" },
      { path: "*" },
    ],
  },
];

test("samePage keeps the page another entity has too, and drops what names this one's records", () => {
  for (const [path, want] of [
    ["/", ""],
    ["/ai", "/ai"],
    ["/ai/questions", "/ai/questions"],
    ["/ai/questions/42", "/ai/questions"],
    ["/settings/team/7/edit", "/settings/team"],
    ["/settings/9", "/settings"],
    ["/onboarding/brand", "/onboarding"],
    ["/results/runs/42", ""],
    ["/notifications/", "/notifications"],
  ]) {
    assert.equal(samePage(path, routes), want, `samePage(${path})`);
  }
});
