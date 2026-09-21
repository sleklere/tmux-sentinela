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
  let promptActive = false;
  let sequence = 0;
  let writes = Promise.resolve();
  let disposed = false;

  const fallbackName = () => basename(cwd) || "Pi";
  const status = () => promptActive ? "blocked" : agentRunning ? "busy" : "idle";

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

  const cleanup = async () => {
    disposed = true;
    await writes;
    await unlink(file).catch(() => {});
  };

  pi.on("session_start", async (_event, ctx) => {
    cwd = ctx.cwd;
    name = pi.getSessionName() || fallbackName();
    agentRunning = false;
    promptActive = false;
    await write();
  });

  pi.on("session_info_changed", async (event) => {
    name = event.name || fallbackName();
    await write();
  });

  pi.on("agent_start", async () => {
    agentRunning = true;
    await write();
  });

  pi.on("agent_settled", async () => {
    agentRunning = false;
    await write();
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
