import { strict as assert } from "node:assert";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

test("Pi stays busy during automatic compaction before and within a run", async () => {
  const cache = await mkdtemp(join(tmpdir(), "sentinela-pi-"));
  const previousCache = process.env.XDG_CACHE_HOME;
  // The extension computes its state path at import time, so import a fresh copy.
  process.env.XDG_CACHE_HOME = cache;
  try {
    const { default: extension } = await import(`./tmux-sentinela.ts?test=${Date.now()}`);
    const handlers = new Map();
    const pi = {
      on(event, handler) { handlers.set(event, handler); },
      getSessionName() { return "test"; },
    };
    extension(pi);
    const emit = async (event, value = {}) => {
      assert.ok(handlers.has(event), `missing ${event} handler`);
      await handlers.get(event)(value, { cwd: cache });
    };
    const state = async () => JSON.parse(await readFile(join(cache, "tmux-sentinela", "pi", `${process.pid}.json`), "utf8"));

    await emit("session_start");
    assert.equal((await state()).status, "idle");
    await emit("session_before_compact", { reason: "threshold" });
    assert.equal((await state()).status, "busy");
    // The extension ignores successful compaction: stay busy until agent_start.
    assert.equal((await state()).status, "busy");
    await emit("agent_start");
    assert.equal((await state()).status, "busy");
    await emit("session_before_compact", { reason: "overflow" });
    assert.equal((await state()).status, "busy");
    await emit("agent_settled");
    assert.equal((await state()).status, "idle");

    await emit("session_before_compact", { reason: "threshold" });
    await emit("session_compact_failed", { reason: "threshold" });
    assert.equal((await state()).status, "idle");
    await emit("session_before_compact", { reason: "manual" });
    assert.equal((await state()).status, "idle");
    await emit("session_shutdown", { reason: "quit" });
  } finally {
    if (previousCache === undefined) delete process.env.XDG_CACHE_HOME;
    else process.env.XDG_CACHE_HOME = previousCache;
    await rm(cache, { recursive: true, force: true });
  }
});
