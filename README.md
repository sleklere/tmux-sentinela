# tmux-sentinela

*Sentinela* is Spanish for sentinel.

A tmux sidebar that lists every AI coding agent (Claude Code, OpenCode)
running in any session, with its state, and jumps to its pane.

```
 agents 1

▌ ● api-refactor            ← blocked: waiting for permission / an answer
▌   claude  work:1  2m
  ⠹ fix-login               ← busy
    claude  work:2
  ✓ docs                    ← done: finished and you have not looked yet
    opencode  home:0
  ○ scratch                 ← idle
    claude  home:1
```

`▌` is the cursor (Enter jumps to the pane). Busy and blocked rows show for
how long.

## Requirements

tmux ≥ 3.2, Go ≥ 1.22 to build, `ps`. Developed on Linux; macOS should work
(portable `ps`/signal 0 process lookup) but is untested.

## Install

With tpm:

```tmux
set -g @plugin 'sleklere/tmux-sentinela'
```

Or clone anywhere and add to `.tmux.conf`:

```tmux
run-shell '/path/to/tmux-sentinela/tmux-sentinela.tmux'
```

The script builds the binary (`go build`) when missing or stale, registers
hooks and the keybinding, and links the OpenCode plugin into
`~/.config/opencode/plugins/tmux-sentinela.js`. Re-sourcing is idempotent.

Claude Code needs hooks in `~/.claude/settings.json` calling
`bin/tmux-sentinela claude-hook` on `Notification`, `PreToolUse`, `PostToolUse`,
`UserPromptSubmit` and `Stop`:

```json
{
  "hooks": {
    "Notification":     [{"hooks": [{"type": "command", "command": "/path/to/tmux-sentinela/bin/tmux-sentinela claude-hook", "timeout": 5}]}],
    "PreToolUse":       [{"hooks": [{"type": "command", "command": "/path/to/tmux-sentinela/bin/tmux-sentinela claude-hook", "timeout": 5}]}],
    "PostToolUse":      [{"hooks": [{"type": "command", "command": "/path/to/tmux-sentinela/bin/tmux-sentinela claude-hook", "timeout": 5}]}],
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "/path/to/tmux-sentinela/bin/tmux-sentinela claude-hook", "timeout": 5}]}],
    "Stop":             [{"hooks": [{"type": "command", "command": "/path/to/tmux-sentinela/bin/tmux-sentinela claude-hook", "timeout": 5}]}]
  }
}
```

The hooks only provide the `blocked` state; busy/idle come from
`~/.claude/sessions/*.json` without any hook.

## Usage

| Key | Action |
|---|---|
| `prefix + a` | toggle the sidebar of the current window |
| `j` / `k`, `↑` / `↓` | move the cursor (inside the sidebar) |
| `Enter`, `l`, click | jump to the agent's pane |
| `q` | close the sidebar |

The cursor follows focus: switching to a window with an agent (by any tmux
means) moves the bar to it. Every sidebar marks the same agent, so jumping
never lands on a window whose bar sits somewhere else.

When an agent turns blocked in a pane you are not looking at, a message shows
on every attached client (see `@sentinela_notify` for desktop notifications).

The sidebar opens by itself in every new window and closes by itself when it
is the only pane left, so closing the last real pane closes the window as it
would without the plugin.

## Options (`set -g` before the `run-shell`)

| Option | Default | |
|---|---|---|
| `@sentinela_key` | `a` | toggle key |
| `@sentinela_width` | `32` | width in columns |
| `@sentinela_autocreate` | `on` | open in new windows |
| `@sentinela_notify` | `tmux` | on blocked: `tmux` (display-message), `desktop` (notify-send / osascript), `both`, `off` |
| `@sentinela_bg` | `@th_base` | opaque background of the sidebar pane |
| `@sentinela_color_text` | `@th_text` | agent names |
| `@sentinela_color_muted` | `@th_muted` | details, idle glyph |
| `@sentinela_color_accent` | `@th_accent1` | cursor bar, done glyph |
| `@sentinela_color_title` | `@th_accent2` | title |
| `@sentinela_color_busy` | `@th_accent3` | spinner |
| `@sentinela_color_alert` | `@th_alert` | blocked glyph and count |

Colors resolve in order: `@sentinela_color_*`, then the `@th_*` globals of a
tmux-wide theme if you keep one, then a rose-pine fallback.

With tmux-resurrect, to restore the sidebar as a process instead of an empty
shell: `set -g @resurrect-processes '"~tmux-sentinela sidebar"'`.

## How state is detected

| Agent | busy / idle | blocked | name |
|---|---|---|---|
| Claude Code | `~/.claude/sessions/<pid>.json` (`status`), pid → pane via the process tree | hook `Notification permission_prompt` or `PreToolUse AskUserQuestion`; cleared by any other hook | `name` (honors `/rename`) |
| OpenCode | plugin: `session.status` / `session.idle` | `permission.updated` until `permission.replied` | `title` of the root session |

State lives in `~/.cache/tmux-sentinela/`. Entries of dead processes are dropped.

## Binary commands

`sidebar` (TUI), `ensure`, `toggle`, `prune`, `status`, `claude-hook`.
