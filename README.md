<pre>
 ██████╗ ███████╗███╗   ███╗
██╔════╝ ██╔════╝████╗ ████║
██║      ███████╗██╔████╔██║
██║      ╚════██║██║╚██╔╝██║
╚██████╗ ███████║██║ ╚═╝ ██║
 ╚═════╝ ╚══════╝╚═╝     ╚═╝
</pre>

# csm

**English** | [한국어](README.ko.md)

A small CLI for browsing and resuming [Claude Code](https://docs.claude.com/en/docs/claude-code) sessions. Built because `claude --resume` shows a flat list with limited identification info, and switching between sessions across projects requires manually `cd`-ing every time.

## What it does

- Lists every session under `~/.claude/projects/`, grouped by project (cwd basename).
- Shows the first user message, git branch, last activity, and message count for each session — enough to identify what each session was about at a glance.
- Filters with fuzzy search (`/`) across project name + first message.
- Pins sessions you care about (`p`) — they show in a dedicated ★ Pinned section at the top and stay marked inline in their project group.
- Drills into a project for its full list when 5 isn't enough (`→` or `Enter` on the `▾ N more` toggle); `←` / `Esc` returns.
- Exports a session to markdown (`e`) — frontmatter + chronological messages + collapsible tool calls. Bulk `csm download` packages every session into a directory tree (with `_index.md`), a zip, or a single combined file.
- Sends sessions you no longer need to a recoverable trash (`d`); `t` opens the trash view where `r` restores and a second `d` deletes for good.
- On selection:
  - `cd`s into the session's original cwd,
  - aligns the git branch (when the working tree is clean and the branch exists locally; warns otherwise),
  - execs `claude --resume <id>` — so file paths, git commands, and tool calls all line up with where the session left off.

## Install

### Homebrew (macOS / Linux)

```bash
brew install ninanung/tap/csm
```

### Go

Requires Go 1.21+.

```bash
go install github.com/ninanung/csm@latest
```

Ensure `$GOBIN` (or `$GOPATH/bin`, typically `~/go/bin`) is on your `PATH`:

```bash
export PATH="$HOME/go/bin:$PATH"
```

### From source

```bash
git clone https://github.com/ninanung/csm ~/Documents/dev/my/csm
cd ~/Documents/dev/my/csm
go install .
```

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
| `→` / `←` / `l` / `h` | drill into project / back     |
| `Enter`         | select session (or drill into `▾ N more`) |
| `/`             | enter filter mode                   |
| `e`             | export current session to markdown (then `o` to open, `c` to copy path) |
| `p`             | toggle pin                          |
| `d`             | delete (press twice to confirm — moves to trash; in trash view, deletes permanently) |
| `t`             | toggle trash view                   |
| `r` / `u`       | restore from trash (trash view)     |
| `Ctrl-D` / `Ctrl-U` | half-page nav                   |
| `g` / `G` / `Home` / `End` | jump to first / last session |
| `Esc`           | unwind one level (status → drill → trash → quit) |
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

### Export and download

```bash
csm export <session-id>             # → ~/Documents/csm-exports/<auto>.md
csm export <session-id> -o out.md   # explicit destination
csm export <session-id> -o -        # to stdout (pipe to clipboard, etc.)

csm download                        # → ~/Documents/csm-downloads/<project>/...
csm download --zip                  # → ~/Documents/csm-downloads/csm-<date>.zip
csm download --single-file          # → one combined markdown
csm download --since 2026-06-01 --project csm --min-msgs 5
```

Inside the picker, `e` exports the highlighted session to the default directory and shows `[o] open · [c] copy path` actions.

## Status

This is the v0.3.0 (Phase 2A) release: picker, automatic `cd`, safe branch alignment, friendly empty state, shell completions, drill-down view, export / download, trash, and pinning.

Still intentionally out:

- Post-hoc rename / label editing UI (sidecar has the field; UI is Phase 2.x)
- Multiplexer-aware popup integration (Phase 2.x — standalone UX must mature first)
- Remote backup sync (Phase 3)
- AI-summarised export mode (Phase 3)

## License

MIT — see [LICENSE](LICENSE).
