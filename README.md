# tmux-sentinela

*Sentinela* is Spanish for sentinel.

A tmux sidebar that lists every AI coding agent (Claude Code, OpenCode, Hermes)
running in any session, with its state, and jumps to its pane.

```
 agents 1

▌ ● api-refactor            ← blocked: waiting for permission / an answer
▌   claude  work:1  2m
  ● fix-login               ← busy: yellow-orange pulse
    claude  work:2
  ✓ docs                    ← done: finished and you have not looked yet
    opencode  home:0
  ○ scratch                 ← idle
    claude  home:1
```

`▌` is the cursor (Enter jumps to the pane). Busy and blocked rows show for
how long.

## Requirements

tmux ≥ 3.2, `ps`, and `curl` or `wget`. Go is optional: when available, the
plugin builds from source; otherwise it downloads a verified release binary.
Developed on Linux; macOS should work but is untested.

## Install

With tpm:

```tmux
set -g @plugin 'sleklere/tmux-sentinela'
```

Or clone anywhere and add to `.tmux.conf`:

```tmux
run-shell '/path/to/tmux-sentinela/tmux-sentinela.tmux'
```

The script builds the binary when Go is available. Without Go, it downloads
the latest release for Linux or macOS (`amd64` / `arm64`) and verifies its
SHA-256 checksum. It also registers hooks and the keybinding, and links the
OpenCode plugin into `~/.config/opencode/plugins/tmux-sentinela.js`.
Re-sourcing is idempotent.

Claude Code needs hooks in `~/.claude/settings.json` calling
`bin/tmux-sentinela claude-hook` on `Notification`, `PreToolUse`, `PostToolUse`,
`UserPromptSubmit` and `Stop`:

```json
{
  "hooks": {
    "Notification":     [{"hooks": [{"type": "command", "command": "/path/to/tmux-sentinela/bin/tmux-sentinela claude-hook", "timeout": 5}]}],
    "PreToolUse":       [{"hooks": [{"type": "command", "command": "/path/to/tmux-sentinela/bin/tmux-sentinela claude-hook", "timeout": 5}]}],
    "PostToolUse":      [{"hooks": [{"type": "command", "command": "/path/to/tmux-sentinela/bin/tmux-sentinela claude-hook", "timeout": 5}]}],
    "SessionStart":     [{"hooks": [{"type": "command", "command": "/path/to/tmux-sentinela/bin/tmux-sentinela claude-hook", "timeout": 5}]}],
    "SessionEnd":       [{"hooks": [{"type": "command", "command": "/path/to/tmux-sentinela/bin/tmux-sentinela claude-hook", "timeout": 5}]}],
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "/path/to/tmux-sentinela/bin/tmux-sentinela claude-hook", "timeout": 5}]}],
    "Stop":             [{"hooks": [{"type": "command", "command": "/path/to/tmux-sentinela/bin/tmux-sentinela claude-hook", "timeout": 5}]}]
  }
}
```

The hooks also mirror Claude's session registry into the plugin cache. This
keeps detection working when Claude runs with a custom `CLAUDE_CONFIG_DIR`.

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

Focusing the sidebar does not name the window after the plugin: automatic
renaming uses the last work pane and preserves your rename format. Manually
named windows keep their names.

Resizing a sidebar shares its width with the others within about two seconds and
updates `@sentinela_width` for new windows. Zoom and terminal-size changes do
not replace the shared width; small windows use as much of it as fits.

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
| `@sentinela_color_busy` | `@th_accent3` | start of the busy pulse |
| `@sentinela_color_busy_glow` | derived from busy color | theme-aware peak of the busy pulse; optional override |
| `@sentinela_color_alert` | `@th_alert` | blocked glyph and count |

Colors resolve in order: `@sentinela_color_*`, then the `@th_*` globals of a
tmux-wide theme if you keep one, then a rose-pine fallback. Changing them
applies live to open sidebars: colors are re-read on events and at least every
two seconds, and the pane background is repainted only when its resolved color
changes. Without
`@sentinela_bg` or `@th_base`, the sidebar inherits the window's styles.

With tmux-resurrect, to restore the sidebar as a process instead of an empty
shell: `set -g @resurrect-processes '"~tmux-sentinela sidebar"'`.

## How state is detected

| Agent | busy / idle | blocked | name |
|---|---|---|---|
| Claude Code | Claude session registry (`status`), mirrored by hooks for custom config dirs; pid → pane via the process tree | hook `Notification permission_prompt` or `PreToolUse AskUserQuestion`; cleared by any other hook | `name` (honors `/rename`) |
| OpenCode | plugin: `session.status` / `session.idle` | `permission.updated` or `permission.asked` until `permission.replied` | `title` of the root session |
| Hermes over SSH | live Hermes composer rendered in the local tmux pane | approval, clarification, sudo and secret prompt symbols | session-title badge, then active skin name |

State lives in `~/.cache/tmux-sentinela/`. Agent and tmux events wake every
sidebar immediately; a two-second poll recovers missed events. Ordered refresh
sequences prevent an older snapshot from replacing a newer one. Entries of dead
processes are dropped.

Hermes detection needs only this plugin on the local machine: it reads the
screen already rendered through SSH and never connects to or installs anything
on the remote host. Keep Hermes' status bar enabled; visual state is refreshed
by the two-second fallback poll because tmux has no pane-output hook.

## Binary commands

`sidebar` (TUI), `ensure`, `toggle`, `prune`, `refresh`, `status`, `claude-hook`.

## Tests and CI

```sh
node --test opencode/tmux-sentinela.test.js
go test -race -count=1 -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go vet ./...
```

CI runs on branch pushes and pull requests, on Linux and macOS. It checks Go
formatting, shell syntax, vet, tests with race detection, and the build. Coverage
is included in the job summary and downloadable as a profile and HTML report.
tmux integration tests use a separate server with no user configuration; they
are skipped locally if tmux is not installed. CI installs tmux on both platforms.

Tag pushes (`v*`) run the same CI workflow on the tagged commit. Release binaries
are built and published only after all checks pass on both platforms.
