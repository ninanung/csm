# csm

A small CLI for browsing and resuming [Claude Code](https://docs.claude.com/en/docs/claude-code) sessions. Built because `claude --resume` shows you a flat list with limited identification info, and switching between sessions across projects requires manually `cd`-ing every time.

`csm` is multiplexer-agnostic. It works standalone in any terminal, and integrates with tmux (or any multiplexer) via small adapter scripts.

## What it does

- Lists every session under `~/.claude/projects/`, grouped by project (cwd basename).
- Shows the first user message, git branch, last activity, and message count for each session — enough to identify what each session was about at a glance.
- Filters with fuzzy search (`/`) across project name + first message.
- On selection, `cd`s into the session's original cwd and execs `claude --resume <id>` — so file paths, git commands, and tool calls all line up with where the session left off.
- Warns about cwd issues right in the picker.

## Install

Requires Go 1.21+.

```bash
git clone <this-repo> ~/Documents/dev/my/csm
cd ~/Documents/dev/my/csm
go install .
csm install         # register the global Claude skill
```

`go install` drops a `csm` binary into `$GOBIN` (or `$GOPATH/bin`); ensure that directory is on your `PATH`.

`csm install` writes the bundled skill to `~/.claude/skills/csm/SKILL.md` so that Claude can trigger `csm` from inside a session (see [Skill mode](#skill-mode) below). Rerun with `--force` to overwrite after a `csm` update. `csm uninstall` removes it.

## Usage

### Standalone

Run from any shell. Pick a session, hit `Enter` — `csm` execs `claude --resume` in the right directory.

```bash
csm
```

### Print mode (for adapters)

Prints `<session-id>\t<cwd>` to stdout and exits, without launching Claude. Useful when another script wants to consume the selection.

```bash
csm --print
```

### List mode (JSON)

Prints every session's metadata as a JSON array and exits. Non-interactive; intended for programmatic consumption (e.g., the Claude skill).

```bash
csm --list-json
```

### Skill mode

After `csm install`, Claude can invoke `csm` from inside a session:

- Trigger phrases: `/csm`, `csm`, `세션 바꿔`, `switch session`, etc.
- The skill calls `csm --list-json`, renders the list as a message, accepts a numeric or keyword choice, then drives the swap via `cmux send` or `tmux send-keys` (depending on which multiplexer is active). In plain terminals, it just prints the commands for the user to run.
- Selecting a session executes `/exit`, `cd <cwd>`, and `claude --resume <id>` in the active pane.

### Keys

| Key             | Action                              |
| --------------- | ----------------------------------- |
| `↑` / `↓` / `j` / `k` | navigate                       |
| `Enter`         | select                              |
| `/`             | enter filter mode                   |
| `Esc`           | exit filter mode (or quit if not filtering) |
| `g` / `G`       | jump to first / last session        |
| `q`             | quit without selecting              |

## Multiplexer integration (experimental)

Multiplexer integration is currently out of scope for Phase 1. The repo ships an experimental tmux adapter script at `scripts/tmux-popup.sh`, but it's untested in real usage and not the recommended path. Use `csm` standalone for now; multiplexer adapters will be designed once the standalone UX is validated.

For reference, the tmux adapter would be bound like this once stabilized:

```tmux
bind-key s display-popup -E -w 80% -h 70% \
  "$HOME/Documents/dev/my/csm/scripts/tmux-popup.sh #{pane_id}"
```

cmux and other multiplexers will require their own adapters since `display-popup` is tmux-specific.

## How it works

Claude Code stores each session as a JSON-Lines file at:

```
~/.claude/projects/<encoded-cwd>/<session-uuid>.jsonl
```

Each line is a message with metadata including `cwd`, `gitBranch`, and `timestamp`. `csm` scans these files, extracts a session summary, and renders the list with a [bubbletea](https://github.com/charmbracelet/bubbletea) TUI.

## Status

This is a Phase 1 release focused on the core picker + auto-`cd` + branch warning. The following are intentionally not in this version:

- Post-hoc rename and tagging (planned for Phase 2)
- Automatic git branch checkout with safety guards (Phase 2)
- Session archive / delete (Phase 2)
- Multiplexer integration — tmux popup, cmux send adapter, etc. (Phase 2, once standalone UX is validated)
- Remote sync for backup (Phase 3)

## License

TBD.
