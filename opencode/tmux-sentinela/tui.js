import { TmuxSentinela } from "../tmux-sentinela.js";

function sameLocation(event, directory) {
  return !event.location?.directory || event.location.directory === directory;
}

export default {
  id: "tmux-sentinela.tui",
  async setup(ctx) {
    const location = ctx.location ?? ctx.data.location.default();
    const directory = location?.directory ?? process.cwd();
    const hooks = await TmuxSentinela({ directory });

    for (const info of ctx.data.session.list()) {
      if (info.location?.directory !== directory || info.parentID) continue;
      await hooks.event({
        event: {
          type: "session.created",
          data: { sessionID: info.id, title: info.title },
        },
      });
      const status = ctx.data.session.status(info.id);
      if (status) {
        await hooks.event({
          event: { type: "session.status", data: { sessionID: info.id, status } },
        });
      }
    }

    const stop = ctx.data.listen(({ details }) => {
      if (!sameLocation(details, directory)) return;
      void hooks.event({ event: details });
    });

    return async () => {
      stop();
      await hooks.dispose();
    };
  },
};
