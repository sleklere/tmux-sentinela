import { TmuxSentinela } from "../tmux-sentinela.js";

function selectedSessionID(ctx) {
  const route = ctx.ui?.router?.current?.();
  return route?.type === "session" ? route.sessionID : "";
}

function selectedSession(ctx) {
  const sessionID = selectedSessionID(ctx);
  if (!sessionID) return { sessionID };

  const info = ctx.data.session.get(sessionID);
  const status = ctx.data.session.status(sessionID);
  const permissions = ctx.data.session.permission?.list?.(sessionID) ?? [];
  const forms = ctx.data.session.form?.list?.(sessionID, ctx.location) ?? [];
  return {
    sessionID,
    title: info?.title || "",
    status,
    blocked: permissions.length > 0 || forms.length > 0,
  };
}

export default {
  id: "tmux-sentinela.tui",
  async setup(ctx) {
    const location = ctx.location ?? ctx.data.location.default();
    const directory = location?.directory ?? process.cwd();
    const hooks = await TmuxSentinela({ directory });
    let updates = Promise.resolve();

    const enqueue = (update) => {
      updates = updates.then(update).catch(() => {});
      return updates;
    };
    const sync = () => hooks.select(selectedSession(ctx));

    await enqueue(sync);

    const stop = ctx.data.listen(({ details }) => {
      void enqueue(async () => {
        // The event stream is shared by every TUI using this directory. Read
        // this TUI's route first, then let the reporter ignore other sessions.
        await sync();
        await hooks.event({ event: details });
      });
    });
    const poll = setInterval(() => void enqueue(sync), 250);

    return async () => {
      clearInterval(poll);
      stop();
      await updates;
      await hooks.dispose();
    };
  },
};
