import assert from "node:assert/strict";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

test("tracks only the selected OpenCode session", async () => {
  const cache = await mkdtemp(join(tmpdir(), "tmux-sentinela-"));
  process.env.XDG_CACHE_HOME = cache;
  process.env.TMUX_PANE = "%42";
  const { TmuxSentinela } = await import(`./tmux-sentinela.js?test=${Date.now()}`);
  const hooks = await TmuxSentinela({ directory: "/work/project", sessionID: "session-1" });
  const file = join(cache, "tmux-sentinela", "opencode", `${process.pid}.json`);
  const state = async () => JSON.parse(await readFile(file, "utf8"));
  const event = async (type, data) => hooks.event({ event: { type, data } });
  const initial = await state();
  assert.deepEqual(initial, {
    pid: process.pid,
    pane: "%42",
    session: "session-1",
    name: "",
    status: "idle",
    updated: initial.updated,
    revision: 1,
    cwd: "/work/project",
    server: initial.server,
  });

  await event("session.created", { sessionID: "session-1", title: "Fix rows" });
  await event("session.status", { sessionID: "session-1", status: { type: "busy" } });
  assert.equal((await state()).status, "busy");
  assert.equal((await state()).name, "Fix rows");
  assert.equal((await state()).revision, 3);

  await Promise.all(
    Array.from({ length: 40 }, (_, i) =>
      event("session.status", { sessionID: "session-1", status: { type: i % 2 ? "idle" : "busy" } }),
    ),
  );
  assert.equal((await state()).status, "idle");
  await event("session.status", { sessionID: "session-1", status: { type: "busy" } });

  await event("session.created", { sessionID: "session-2", title: "Wrong pane" });
  await event("session.status", { sessionID: "session-2", status: { type: "idle" } });
  await event("permission.asked", { id: "other-permission", sessionID: "session-2" });
  assert.equal((await state()).name, "Fix rows");
  assert.equal((await state()).status, "busy");

  await event("permission.asked", { id: "permission", sessionID: "session-1" });
  assert.equal((await state()).status, "blocked");
  await event("permission.replied", { requestID: "permission", sessionID: "session-1" });
  assert.equal((await state()).status, "busy");

  await event("form.created", { form: { id: "form", sessionID: "session-1" } });
  assert.equal((await state()).status, "blocked");
  await event("form.replied", { id: "form", sessionID: "session-1" });
  assert.equal((await state()).status, "busy");

  await event("session.renamed", { sessionID: "session-1", title: "Fixed rows" });
  assert.equal((await state()).name, "Fixed rows");

  await event("permission.asked", { id: "abandoned", sessionID: "session-1" });
  await event("session.deleted", { sessionID: "session-1" });
  assert.equal((await state()).status, "idle");
  assert.equal((await state()).session, "");

  await hooks.select({ sessionID: "session-2", title: "Second pane", status: "running" });
  assert.equal((await state()).session, "session-2");
  assert.equal((await state()).name, "Second pane");
  assert.equal((await state()).status, "busy");

  await hooks.dispose();
  await assert.rejects(readFile(file));
  await event("permission.asked", { id: "after-dispose", sessionID: "session-1" });
  await assert.rejects(readFile(file));
  await rm(cache, { recursive: true, force: true });
});

test("exports OpenCode V2 server and TUI plugin definitions", async () => {
  const { default: server } = await import(`./tmux-sentinela/index.js?v2=${Date.now()}`);
  const { default: tui } = await import(`./tmux-sentinela/tui.js?v2=${Date.now()}`);
  assert.equal(server.id, "tmux-sentinela");
  assert.equal(typeof server.setup, "function");
  assert.equal(tui.id, "tmux-sentinela.tui");
  assert.equal(typeof tui.setup, "function");
});

test("TUI reports its selected session instead of all sessions in its directory", async () => {
  // tui.js imports the reporter without a cache-busting query, so it uses the
  // cache home captured when the preceding module-definition test imported it.
  const cache = process.env.XDG_CACHE_HOME;
  process.env.TMUX_PANE = "%43";
  const { default: tui } = await import(`./tmux-sentinela/tui.js?tui=${Date.now()}`);
  const file = join(cache, "tmux-sentinela", "opencode", `${process.pid}.json`);
  const state = async () => JSON.parse(await readFile(file, "utf8"));
  const waitFor = async (predicate) => {
    for (let i = 0; i < 100; i++) {
      if (await predicate()) return;
      await new Promise((resolve) => setTimeout(resolve, 5));
    }
    assert.fail("timed out waiting for TUI state");
  };
  const sessions = {
    "session-1": { title: "First pane" },
    "session-2": { title: "Second pane" },
  };
  let current = "session-1";
  let listener;
  let cleanup;
  try {
    cleanup = await tui.setup({
      location: { directory: "/work/project" },
      ui: { router: { current: () => ({ type: "session", sessionID: current }) } },
      data: {
        session: {
          get: (sessionID) => sessions[sessionID],
          status: () => "idle",
          permission: { list: () => [] },
          form: { list: () => [] },
        },
        listen: (fn) => {
          listener = fn;
          return () => { listener = undefined; };
        },
      },
    });

    assert.equal((await state()).name, "First pane");
    listener({ details: { type: "session.status", data: { sessionID: "session-2", status: { type: "busy" } } } });
    await new Promise((resolve) => setImmediate(resolve));
    assert.equal((await state()).name, "First pane");
    assert.equal((await state()).status, "idle");

    current = "session-2";
    listener({ details: { type: "session.status", data: { sessionID: "session-1", status: { type: "busy" } } } });
    await waitFor(async () => (await state()).name === "Second pane");
    assert.equal((await state()).name, "Second pane");
    assert.equal((await state()).status, "idle");
  } finally {
    await cleanup?.();
    await rm(cache, { recursive: true, force: true });
  }
});
