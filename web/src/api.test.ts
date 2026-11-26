import assert from "node:assert/strict";
import { test } from "node:test";
import { api, syncEntity } from "./api.ts";

type Sent = { url: string; method: string; answer: () => void };

function fakeBrowser(path: string) {
  const sent: Sent[] = [];
  const loc = { pathname: path };
  Object.assign(globalThis, {
    location: loc,
    fetch: (url: string, init: RequestInit) =>
      new Promise<Response>((resolve, reject) => {
        init.signal?.addEventListener("abort", () => reject(init.signal!.reason));
        sent.push({ url, method: init.method ?? "GET", answer: () => resolve(new Response("{}", { status: 200 })) });
      }),
  });
  const go = (to: string) => {
    loc.pathname = to;
    syncEntity();
  };
  return { sent, go };
}

const settled = (p: Promise<unknown>) =>
  Promise.race([p.then(() => "resolved", (e: unknown) => e), new Promise((r) => setTimeout(() => r("still running"), 100))]);

test("leaving an entity ends its requests, and the next request goes to the entity entered", async () => {
  const { sent, go } = fakeBrowser("/w/a/settings");
  const inA = api.settings.get();
  const shared = api.me.get();
  assert.equal(sent[0].url, "/w/a/v1/settings");

  go("/w/b/settings");
  const ended = await settled(inA);
  assert.ok(ended instanceof Error && ended.name === "AbortError", `a request in a after leaving it: ${String(ended)}`);

  const inB = api.settings.get();
  assert.equal(sent.at(-1)!.url, "/w/b/v1/settings");
  sent.at(-1)!.answer();
  assert.equal(await settled(inB), "resolved");

  sent[1].answer();
  assert.equal(await settled(shared), "resolved", "a request every entity shares outlives the switch");
});

test("a save that reads, then writes, stops when the entity is left in between", async () => {
  const { sent, go } = fakeBrowser("/w/a/settings");
  const save = (async () => {
    const current = await api.settings.get();
    return api.settings.put(current as never);
  })();
  go("/w/b/settings");
  assert.notEqual(await settled(save), "still running");
  assert.deepEqual(
    sent.map((s) => `${s.method} ${s.url}`),
    ["GET /w/a/v1/settings"],
    "nothing is written to b, or to a",
  );
});

test("Back to an entity left before opens it afresh", async () => {
  const { sent, go } = fakeBrowser("/w/a/settings");
  go("/w/b/settings");
  go("/w/a/settings");
  const again = api.settings.get();
  assert.equal(sent.at(-1)!.url, "/w/a/v1/settings");
  sent.at(-1)!.answer();
  assert.equal(await settled(again), "resolved");
});
