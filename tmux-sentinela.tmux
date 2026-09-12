#!/usr/bin/env bash
# tmux-sentinela entrypoint. Source from .tmux.conf:
#   run-shell '~/workspace/tmux-sentinela/tmux-sentinela.tmux'
# Builds the binary when missing or stale, wires keybinding + hooks, links the
# OpenCode plugin and opens sidebars in existing windows. Re-sourcing is
# idempotent (hooks use fixed array indexes).
set -u
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$DIR/bin/tmux-sentinela"

if [ ! -x "$BIN" ] || [ -n "$(find "$DIR" -name '*.go' -newer "$BIN" -print -quit)" ]; then
    if ! (cd "$DIR" && go build -o "$BIN" .) 2>"$DIR/build.log"; then
        tmux display-message "tmux-sentinela: go build failed, see $DIR/build.log"
        exit 0
    fi
fi

key=$(tmux show-option -gqv @sentinela_key); key=${key:-a}
tmux bind-key "$key" run-shell "'$BIN' toggle '#{window_id}'"

tmux set-hook -g 'after-new-window[50]'  "run-shell -b \"'$BIN' ensure\""
tmux set-hook -g 'after-new-session[50]' "run-shell -b \"'$BIN' ensure\""
tmux set-hook -gw 'pane-exited[50]'      "run-shell -b \"'$BIN' prune\""
tmux set-hook -g 'after-kill-pane[50]'   "run-shell -b \"'$BIN' prune\""

# OpenCode plugin (auto-loaded from ~/.config/opencode/plugins)
mkdir -p "$HOME/.config/opencode/plugins"
ln -sfn "$DIR/opencode/tmux-sentinela.js" "$HOME/.config/opencode/plugins/tmux-sentinela.js"

autocreate=$(tmux show-option -gqv @sentinela_autocreate)
[ "${autocreate:-on}" = "on" ] && "$BIN" ensure
exit 0
