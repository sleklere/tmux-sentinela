import assert from "node:assert/strict";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

test("tracks OpenCode sessions and both permission event versions", async () => {
  const cache = await mkdtemp(join(tmpdir(), "tmux-sentinela-"));
  process.env.XDG_CACHE_HOME = cache;
  process.env.TMUX_PANE = "%42";
  const { TmuxSentinela } = await import(`./tmux-sentinela.js?test=${Date.now()}`);
  const hooks = await TmuxSentinela({ directory: "/work/project" });
  const file = join(cache, "tmux-sentinela", "opencode", `${process.pid}.json`);
  const state = async () => JSON.parse(await readFile(file, "utf8"));
  const event = async (type, properties) => hooks.event({ event: { type, properties } });

  assert.deepEqual(await state(), {
    pid: process.pid,
    pane: "%42",
    name: "",
    status: "idle",
    updated: (await state()).updated,
    cwd: "/work/project",
  });

  await event("session.created", { info: { id: "session-1", title: "Fix rows" } });
  await event("session.status", { sessionID: "session-1", status: { type: "busy" } });
  assert.equal((await state()).status, "busy");
  assert.equal((await state()).name, "Fix rows");

  await Promise.all(
    Array.from({ length: 40 }, (_, i) =>
      event("session.status", { sessionID: "session-1", status: { type: i % 2 ? "idle" : "busy" } }),
    ),
  );
  assert.equal((await state()).status, "idle");
  await event("session.status", { sessionID: "session-1", status: { type: "busy" } });

  await event("permission.updated", { id: "old-permission", sessionID: "session-1" });
  assert.equal((await state()).status, "blocked");
  await event("permission.replied", { permissionID: "old-permission", sessionID: "session-1" });
  assert.equal((await state()).status, "busy");

  await event("permission.asked", { id: "new-permission", sessionID: "session-1" });
  assert.equal((await state()).status, "blocked");
  await event("permission.replied", { requestID: "new-permission", sessionID: "session-1" });
  assert.equal((await state()).status, "busy");

  await event("session.error", { sessionID: "session-1", error: { message: "failed" } });
  assert.equal((await state()).status, "idle");

  await event("permission.asked", { id: "abandoned", sessionID: "session-1" });
  await event("session.deleted", { info: { id: "session-1" } });
  assert.equal((await state()).status, "idle");

  await hooks.dispose();
  await assert.rejects(readFile(file));
  await event("permission.asked", { id: "after-dispose", sessionID: "session-1" });
  await assert.rejects(readFile(file));
  await rm(cache, { recursive: true, force: true });
});
