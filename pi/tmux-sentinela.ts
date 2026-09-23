// Pi extension: reports this Pi process to tmux-sentinela.
// Link this file into ~/.pi/agent/extensions/ so Pi auto-discovers it.
import { createHash } from "node:crypto";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { mkdir, rename, unlink, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { basename, join } from "node:path";

const stateDir = join(process.env.XDG_CACHE_HOME || join(homedir(), ".cache"), "tmux-sentinela");
const dir = join(stateDir, "pi");
const file = join(dir, `${process.pid}.json`);

function serverIdentity(): string {
  const socket = (process.env.TMUX || "").split(",")[0] || "";
  return socket ? createHash("sha256").update(socket).digest("hex").slice(0, 8) : "unknown";
}

const server = serverIdentity();

export default function (pi: ExtensionAPI) {
  let cwd = "";
  let name = "";
  let agentRunning = false;
  let autoCompacting = false;
  let promptActive = false;
  let sequence = 0;
  let writes = Promise.resolve();
  let disposed = false;
  let settleTimer: ReturnType<typeof setTimeout> | undefined;

  const cancelSettle = () => {
    if (settleTimer) clearTimeout(settleTimer);
    settleTimer = undefined;
  };

  const fallbackName = () => basename(cwd) || "Pi";
  const status = () => promptActive ? "blocked" : agentRunning || autoCompacting ? "busy" : "idle";

  const write = async () => {
    if (disposed) return;
    const revision = ++sequence;
    const state = JSON.stringify({
      pid: process.pid,
      pane: process.env.TMUX_PANE || "",
      name: name || pi.getSessionName() || fallbackName(),
      status: status(),
      updated: Date.now(),
      revision,
      cwd,
      server,
    });
    const temp = `${file}.${revision}.tmp`;
    writes = writes
      .then(async () => {
        if (disposed) return;
        await mkdir(dir, { recursive: true });
        await writeFile(temp, state);
        await rename(temp, file);
      })
      .catch(() => {})
      .finally(() => unlink(temp).catch(() => {}));
    await writes;
  };

  // Reload/new/resume/fork keep this process: the next instance overwrites the
  // file, so removing it would drop the agent (and the sidebar cursor) for a poll.
  const cleanup = async (event: { reason: string }) => {
    cancelSettle();
    disposed = true;
    await writes;
    if (event.reason === "quit") await unlink(file).catch(() => {});
  };

  pi.on("session_start", async (_event, ctx) => {
    cancelSettle();
    cwd = ctx.cwd;
    name = pi.getSessionName() || fallbackName();
    agentRunning = false;
    autoCompacting = false;
    promptActive = false;
    await write();
  });

  pi.on("session_info_changed", async (event) => {
    name = event.name || fallbackName();
    await write();
  });

  // Pi can compact before agent_start when a new prompt arrives. Keep the
  // sidebar busy until that prompt starts, even though the previous run settled.
  pi.on("session_before_compact", async (event) => {
    if (event.reason === "manual") return;
    autoCompacting = true;
    await write();
  });

  pi.on("session_compact_failed", async () => {
    autoCompacting = false;
    await write();
  });

  pi.on("agent_start", async () => {
    cancelSettle();
    agentRunning = true;
    autoCompacting = false;
    await write();
  });

  pi.on("agent_settled", () => {
    // A new run may start shortly after settlement (queued work or an async
    // wake-up). Do not announce completion during that brief handoff.
    cancelSettle();
    settleTimer = setTimeout(() => {
      settleTimer = undefined;
      agentRunning = false;
      autoCompacting = false;
      void write();
    }, 3000);
    settleTimer.unref();
  });

  pi.on("ui_prompt_start", async () => {
    promptActive = true;
    await write();
  });

  pi.on("ui_prompt_end", async () => {
    promptActive = false;
    await write();
  });

  pi.on("session_shutdown", cleanup);
}
