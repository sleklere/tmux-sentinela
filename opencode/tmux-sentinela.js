// OpenCode plugin: reports this process's agent state to
// ~/.cache/tmux-sentinela/opencode/<pid>.json for the tmux-sentinela sidebar.
// Runs inside the opencode server process. The pane comes from TMUX_PANE when
// inherited; otherwise the sidebar maps pid to pane through the process tree.
import { mkdir, writeFile, unlink } from "node:fs/promises";
import { join } from "node:path";
import { homedir } from "node:os";

const dir = join(process.env.XDG_CACHE_HOME || join(homedir(), ".cache"), "tmux-sentinela", "opencode");
const file = join(dir, `${process.pid}.json`);

export const TmuxSentinela = async ({ directory }) => {
  const roots = new Map(); // root sessionID -> { title, status }
  const pending = new Set(); // permission ids awaiting an answer
  let ready = mkdir(dir, { recursive: true }).catch(() => {});

  const write = async () => {
    await ready;
    const sessions = [...roots.values()];
    const status = pending.size ? "blocked" : sessions.some((s) => s.status === "busy") ? "busy" : "idle";
    const name = sessions.at(-1)?.title || "";
    const state = { pid: process.pid, pane: process.env.TMUX_PANE || "", name, status, updated: Date.now(), cwd: directory };
    await writeFile(file, JSON.stringify(state)).catch(() => {});
  };

  const root = (id) => roots.get(id) ?? (roots.set(id, { title: "", status: "idle" }), roots.get(id));

  const cleanup = () => unlink(file).catch(() => {});
  process.once("exit", cleanup);
  for (const sig of ["SIGINT", "SIGTERM", "SIGHUP"]) process.once(sig, cleanup);

  await write();

  return {
    event: async ({ event }) => {
      const p = event.properties;
      switch (event.type) {
        case "session.created":
        case "session.updated":
          if (p.info.parentID) return; // subagent sessions don't drive the pane state
          root(p.info.id).title = p.info.title || "";
          break;
        case "session.status":
          if (!roots.has(p.sessionID)) return;
          root(p.sessionID).status = p.status.type === "idle" ? "idle" : "busy";
          break;
        case "session.idle":
          if (!roots.has(p.sessionID)) return;
          root(p.sessionID).status = "idle";
          break;
        case "session.deleted":
          roots.delete(p.info.id);
          break;
        case "permission.updated":
          pending.add(p.id);
          break;
        case "permission.replied":
          pending.delete(p.permissionID);
          break;
        default:
          return;
      }
      await write();
    },
  };
};
