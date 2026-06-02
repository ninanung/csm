# csm

A small CLI for browsing and resuming [Claude Code](https://docs.claude.com/en/docs/claude-code) sessions. Built because `claude --resume` shows a flat list with limited identification info, and switching between sessions across projects requires manually `cd`-ing every time.

## What it does

- Lists every session under `~/.claude/projects/`, grouped by project (cwd basename).
- Shows the first user message, git branch, last activity, and message count for each session — enough to identify what each session was about at a glance.
- Filters with fuzzy search (`/`) across project name + first message.
- On selection:
  - `cd`s into the session's original cwd,
  - aligns the git branch (when the working tree is clean and the branch exists locally; warns otherwise),
  - execs `claude --resume <id>` — so file paths, git commands, and tool calls all line up with where the session left off.

## Install

Requires Go 1.21+.

```bash
git clone <this-repo> ~/Documents/dev/my/csm
cd ~/Documents/dev/my/csm
go install .
```

This drops a `csm` binary into `$GOBIN` (or `$GOPATH/bin`); ensure that directory is on your `PATH`.

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

### Language

The interface (header, footer hints, branch prompt, error messages) is available in **English** and **Korean**.

Default: auto-detected from `CSM_LANG`, then `LC_ALL` / `LC_MESSAGES` / `LANG`. Falls back to English.

Override per invocation:

```bash
csm --lang ko
csm --lang en
```

Or persistent:

```bash
export CSM_LANG=ko
```

### Keys

| Key             | Action                              |
| --------------- | ----------------------------------- |
| `↑` / `↓` / `j` / `k` | navigate                       |
| `Enter`         | select                              |
| `/`             | enter filter mode                   |
| `Esc`           | exit filter mode (or quit if not filtering) |
| `g` / `G`       | jump to first / last session        |
| `q`             | quit without selecting              |

## Branch alignment — safety rules

When you select a session, `csm` will switch the git branch only when **all** of the following hold:

- the working tree is clean,
- the recorded branch exists locally,
- no rebase / merge / cherry-pick is in progress,
- the branch is not checked out at another worktree.

Otherwise it prints a one-line warning and proceeds without switching, so the resumed Claude session can still load without destroying local state.

## How it works

Claude Code stores each session as a JSON-Lines file at:

```
~/.claude/projects/<encoded-cwd>/<session-uuid>.jsonl
```

Each line is a message with metadata including `cwd`, `gitBranch`, and `timestamp`. `csm` scans these files, extracts a session summary, and renders the list with a [bubbletea](https://github.com/charmbracelet/bubbletea) TUI.

## Status

This is a Phase 1 release focused on the core picker, automatic `cd`, and safe branch alignment. The following are intentionally not in this version:

- Post-hoc rename and tagging (Phase 2)
- Session archive / delete (Phase 2)
- Multiplexer-aware "popup" integration (Phase 2, once standalone UX is validated)
- Remote sync for backup (Phase 3)

## License

TBD.
