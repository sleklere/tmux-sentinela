// OpenCode plugin: reports this process's agent state to
// ~/.cache/tmux-sentinela/opencode/<pid>.json for the tmux-sentinela sidebar.
// Runs inside the opencode server process. The pane comes from TMUX_PANE when
// inherited; otherwise the sidebar maps pid to pane through the process tree.
import { mkdir, rename, writeFile, unlink } from "node:fs/promises";
import { join } from "node:path";
import { homedir } from "node:os";

const stateDir = join(process.env.XDG_CACHE_HOME || join(homedir(), ".cache"), "tmux-sentinela");
const dir = join(stateDir, "opencode");
const file = join(dir, `${process.pid}.json`);

export const TmuxSentinela = async ({ directory }) => {
  const roots = new Map(); // root sessionID -> { title, status }
  const pending = new Map(); // permission id -> session id
  const ready = mkdir(dir, { recursive: true }).catch(() => {});
  let sequence = 0;
  let writes = Promise.resolve();
  let disposed = false;

  const write = async () => {
    await ready;
    if (disposed) return;
    const sessions = [...roots.values()];
    const status = pending.size ? "blocked" : sessions.some((s) => s.status === "busy") ? "busy" : "idle";
    const name = sessions.at(-1)?.title || "";
    const revision = ++sequence;
    const state = { pid: process.pid, pane: process.env.TMUX_PANE || "", name, status, updated: Date.now(), revision, cwd: directory };
    const data = JSON.stringify(state);
    const temp = `${file}.${revision}.tmp`;
    writes = writes
      .then(async () => {
        await writeFile(temp, data);
        await rename(temp, file);
      })
      .catch(() => {})
      .finally(() => unlink(temp).catch(() => {}));
    await writes;
  };

  const root = (id) => roots.get(id) ?? (roots.set(id, { title: "", status: "idle" }), roots.get(id));

  const cleanup = async () => {
    disposed = true;
    await writes;
    await unlink(file).catch(() => {});
  };

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
          for (const [id, sessionID] of pending) {
            if (sessionID === p.info.id) pending.delete(id);
          }
          break;
        case "session.error":
          if (roots.has(p.sessionID)) root(p.sessionID).status = "idle";
          break;
        case "permission.updated":
        case "permission.asked":
          pending.set(p.id, p.sessionID);
          break;
        case "permission.replied":
          pending.delete(p.permissionID || p.requestID);
          break;
        default:
          return;
      }
      await write();
    },
    dispose: cleanup,
  };
};
