// OpenCode plugin: reports this TUI's selected session to
// ~/.cache/tmux-sentinela/opencode/<pid>.json for the tmux-sentinela sidebar.
// The OpenCode server is shared by every TUI in a directory, so a reporter
// must never aggregate its sessions: one tmux pane has one selected session.
import { mkdir, rename, writeFile, unlink } from "node:fs/promises";
import { join } from "node:path";
import { homedir } from "node:os";
import { createHash } from "node:crypto";

const stateDir = join(process.env.XDG_CACHE_HOME || join(homedir(), ".cache"), "tmux-sentinela");
const dir = join(stateDir, "opencode");
const file = join(dir, `${process.pid}.json`);

function serverIdentity() {
  const tmuxEnv = process.env.TMUX || "";
  const socket = tmuxEnv.split(",")[0] || "";
  if (!socket) return "unknown";
  return createHash("sha256").update(socket).digest("hex").slice(0, 8);
}

const serverID = serverIdentity();

export const TmuxSentinela = async ({ directory, sessionID = "" }) => {
  let session = { id: sessionID, title: "", status: "idle" };
  const pending = new Set(); // permission/form IDs for the selected session
  const ready = mkdir(dir, { recursive: true }).catch(() => {});
  let sequence = 0;
  let writes = Promise.resolve();
  let disposed = false;

  const write = async () => {
    await ready;
    if (disposed) return;
    const status = pending.size ? "blocked" : session.status;
    const revision = ++sequence;
    const state = { pid: process.pid, pane: process.env.TMUX_PANE || "", session: session.id, name: session.title, status, updated: Date.now(), revision, cwd: directory, server: serverID };
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

  const isSelected = (id) => id && id === session.id;

  const select = async ({ sessionID: id = "", title = "", status = "idle", blocked = false }) => {
    const nextStatus = status === "busy" || status === "running" ? "busy" : "idle";
    const changed = session.id !== id || session.title !== title || session.status !== nextStatus ||
      (blocked !== (pending.size > 0));
    if (!changed) return;
    session = { id, title, status: nextStatus };
    pending.clear();
    if (blocked) pending.add("snapshot");
    await write();
  };

  const cleanup = async () => {
    disposed = true;
    await writes;
    await unlink(file).catch(() => {});
  };

  await write();

  return {
    select,
    event: async ({ event }) => {
      const p = event.data ?? event.properties;
      switch (event.type) {
        case "session.created":
        case "session.updated":
          {
            const info = p.info ?? p;
            const sessionID = info.sessionID ?? info.id;
            if (!isSelected(sessionID)) return;
            session.title = info.title || "";
          }
          break;
        case "session.renamed":
          if (!isSelected(p.sessionID)) return;
          session.title = p.title || "";
          break;
        case "session.status":
          if (!isSelected(p.sessionID)) return;
          session.status = p.status.type === "idle" ? "idle" : "busy";
          break;
        case "session.idle":
          if (!isSelected(p.sessionID)) return;
          session.status = "idle";
          break;
        case "session.deleted":
          {
            const sessionID = p.sessionID ?? p.info?.id;
            if (!isSelected(sessionID)) return;
            session = { id: "", title: "", status: "idle" };
            pending.clear();
          }
          break;
        case "session.error":
          if (!isSelected(p.sessionID)) return;
          session.status = "idle";
          break;
        case "permission.updated":
        case "permission.asked":
          if (!isSelected(p.sessionID)) return;
          pending.add(p.id);
          break;
        case "permission.replied":
          if (!isSelected(p.sessionID)) return;
          pending.delete(p.permissionID || p.requestID);
          break;
        case "form.created":
          if (!isSelected(p.form.sessionID)) return;
          pending.add(p.form.id);
          break;
        case "form.replied":
        case "form.cancelled":
          if (!isSelected(p.sessionID)) return;
          pending.delete(p.id);
          break;
        default:
          return;
      }
      await write();
    },
    dispose: cleanup,
  };
};
