#!/usr/bin/env bash
# tmux-sentinela entrypoint. Source from .tmux.conf:
#   run-shell '~/workspace/tmux-sentinela/tmux-sentinela.tmux'
# Builds the binary when missing or stale, wires keybinding + hooks, links the
# OpenCode plugin and opens sidebars in existing windows. Re-sourcing is
# idempotent (hooks use fixed array indexes).
set -u
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$DIR/bin/tmux-sentinela"
mkdir -p "$DIR/bin"

rebuilt=off
if [ ! -x "$BIN" ] || [ -n "$(find "$DIR" -type f \( -name '*.go' -o -name 'go.mod' -o -name 'go.sum' \) -newer "$BIN" -print -quit)" ]; then
	if command -v go >/dev/null 2>&1; then
		if ! (cd "$DIR" && go build -o "$BIN" .) 2>"$DIR/build.log"; then
			tmux display-message "tmux-sentinela: go build failed, see $DIR/build.log"
			exit 0
		fi
	elif ! "$DIR/install-binary.sh" "$BIN" 2>"$DIR/build.log"; then
		tmux display-message "tmux-sentinela: release download failed, see $DIR/build.log"
		exit 0
	fi
	rebuilt=on
fi

key=$(tmux show-option -gqv @sentinela_key); key=${key:-a}
tmux bind-key "$key" run-shell "'$BIN' toggle '#{window_id}'"

# Keep window/session changes from restoring focus to a sidebar. Direct pane
# navigation and clicks may still focus it so its keyboard controls work.
focus_guard="if-shell -F '#{&&:#{@sentinela_sidebar},#{||:#{==:#{mouse_any_flag},0},#{!=:#{mouse_pane},#{pane_id}}}}' 'select-pane -l'"
tmux set-hook -gu 'after-select-pane[40]'
tmux set-hook -g 'after-select-window[40]' "$focus_guard"
tmux set-hook -g 'client-session-changed[40]' "$focus_guard"

tmux set-hook -g 'after-new-window[50]'  "run-shell -b \"'$BIN' ensure\""
tmux set-hook -g 'after-new-session[50]' "run-shell -b \"'$BIN' ensure\""
tmux set-hook -gw 'pane-exited[50]'      "run-shell -b \"'$BIN' prune\""
tmux set-hook -g 'after-kill-pane[50]'   "run-shell -b \"'$BIN' prune\""
tmux set-hook -g 'after-select-pane[50]' "run-shell -b \"'$BIN' refresh\""
tmux set-hook -g 'after-select-window[50]' "run-shell -b \"'$BIN' refresh\""
tmux set-hook -g 'client-session-changed[50]' "run-shell -b \"'$BIN' refresh\""

if [ "$rebuilt" = "on" ]; then
	while read -r window sidebar; do
		if [ -n "$sidebar" ]; then
			"$BIN" toggle "$window"
			"$BIN" toggle "$window"
		fi
	done < <(tmux list-panes -a -F '#{window_id} #{@sentinela_sidebar}')
fi

# OpenCode plugin (auto-loaded from ~/.config/opencode/plugins)
mkdir -p "$HOME/.config/opencode/plugins"
ln -sfn "$DIR/opencode/tmux-sentinela.js" "$HOME/.config/opencode/plugins/tmux-sentinela.js"

autocreate=$(tmux show-option -gqv @sentinela_autocreate)
[ "${autocreate:-on}" = "on" ] && "$BIN" ensure
exit 0
