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
- **Hides SDK / orchestration sessions by default** (worktree agents, `entrypoint=sdk-cli`) so the picker shows only what you started yourself. Toggle with `a`; hidden count surfaces in the header.
- **Groups templated repeats** in the overview — sessions that share an identical first user message (e.g. repeated `spec-to-plan` workflow invocations) collapse into one row with a `+N similar` badge. Drill into the project to see them individually.
- **Surfaces sub-agent spawns**: sessions that ran `Task` agents show a `↳N agents (s)` badge. Press `s` to drill into the sub-agent list and inspect each spawn (agent type, description, first message) — Enter opens the jsonl in the OS default viewer.
- Exports a session as raw JSONL — exactly the bytes Claude Code wrote (`e`). When the session has sub-agent or tool-result sidecars, the export becomes a folder containing the full on-disk structure, suitable for `cp -r` round-trip back into `~/.claude/projects/`. Bulk `csm download` packages every session into a directory tree (with a markdown `_index.md` TOC) or a zip.
- Sends sessions you no longer need to a recoverable trash (`d`); sub-agent sidecars move along with the main session, so nothing is left orphaned. `t` opens the trash view where `r` restores and a second `d` deletes for good.
- **Bulk-prunes old sessions** with `csm prune <days>` — pinned sessions are protected, the operation previews what's about to disappear, and confirmation is required unless `-y` / `--force`.
- **Merges sessions** via your local `claude` — mark sessions with `Space`, press `m`, and Claude consolidates their combined content into the **latest** selected session (kept, with its id, so `claude --resume` continues from it); the other sessions move to the trash. Also `csm merge <id> <id>…`.
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
| `Enter`         | select session (or drill into `▾ N more`; on a sub-agent row, open jsonl in OS viewer) |
| `/`             | enter filter mode                   |
| `e`             | export current session as raw JSONL (then `o` to open, `c` to copy path) |
| `Space`         | mark / unmark session for merge (numbered `[1] [2]` badges) |
| `m`             | merge marked sessions — consolidate via `claude` into the latest |
| `p`             | toggle pin                          |
| `s`             | open sub-agent view for the cursor session (`↳N agents` badge marks eligible rows) |
| `a`             | show / hide SDK-spawned (orchestration) sessions — hidden by default |
| `d`             | move to trash (recoverable; in trash view, press `d` twice to permanently delete) |
| `t`             | toggle trash view                   |
| `r` / `u`       | restore from trash (trash view)     |
| `?`             | full key reference overlay (any key to dismiss) |
| `Ctrl-D` / `Ctrl-U` | half-page nav                   |
| `g` / `G` / `Home` / `End` | jump to first / last session |
| `Esc`           | unwind one level (status → sub-agent view → drill → trash → quit) |
| `q`             | quit without selecting              |

Mouse wheel also scrolls the picker.

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

When a session spawns sub-agents (via the `Task` tool), Claude Code adds a sibling directory for sidecar artifacts:

```
~/.claude/projects/<encoded-cwd>/
├── <session-uuid>.jsonl         ← main session (user + assistant lines)
└── <session-uuid>/              ← sidecar container (when present)
    ├── subagents/agent-*.jsonl  ← each Task spawn's conversation
    │  + agent-*.meta.json       ← {agentType, description, toolUseId}
    └── tool-results/            ← captured tool outputs
```

Each line in the main jsonl carries metadata including `cwd`, `gitBranch`, `timestamp`, and `entrypoint`. `csm` scans every session, walks the sidecar dir to compute the sub-agent count and the latest activity timestamp, and renders the list with a [bubbletea](https://github.com/charmbracelet/bubbletea) TUI.

### Export and download

Exports copy the raw JSONL session file verbatim — same bytes Claude Code wrote, suitable for backup or re-import. When a session has a `<uuid>/` sidecar dir (sub-agents, tool-results), the export becomes a folder mirroring the on-disk layout, so the contents are still round-trippable.

```bash
csm export <session-id>             # → ~/Downloads/<auto>.jsonl
                                    #   (or ~/Downloads/<auto>/ when sidecars exist)
csm export <session-id> -o out.jsonl
csm export <session-id> -o -        # stdout (pipe to jq, etc.)

csm download                        # → ~/Downloads/csm-<date>/<project>/...
csm download --zip                  # → ~/Downloads/csm-<date>.zip
csm download --since 2026-06-01 --project csm --min-msgs 5
```

Inside the picker, `e` exports the highlighted session and shows the resulting path in the footer (`c` copies the path).

### Pruning old sessions

`csm prune <days>` moves sessions whose last activity is older than `<days>` into the trash. Pinned (★) sessions are protected by default.

```bash
csm prune 30                        # interactive — preview + confirm
csm prune 30 --dry-run              # see what would be pruned, no changes
csm prune 30 -y                     # skip confirmation (for cron / scripts)
csm prune 30 --permanent            # skip trash, delete forever
csm prune 30 --include-pinned       # opt in to pruning pinned sessions
csm prune 30 --project NAME         # limit to a single project
```

Flag order is flexible — `csm prune 30 --dry-run` and `csm prune --dry-run 30` both work.

### Merging sessions

`csm merge` consolidates several sessions into one. The combined conversation text is handed to your local `claude` binary, which reorganizes it into a single clean record; that consolidation is seeded into the **latest** selected session (kept, with its id, so `claude --resume` continues from it), and the other sessions move to the trash.

```bash
csm merge <id> <id> [<id>…]         # consolidate into the latest; trash the rest
```

Inside the picker: mark sessions with `Space` (numbered `[1] [2]` badges), then press `m`. The consolidation calls `claude` (a few seconds; uses tokens), and the headless call's own session is removed automatically so it doesn't clutter the list.

### Housekeeping

`csm cleanup` consolidates orphan sub-agent directories that earlier csm versions left behind in `~/.claude/projects/` when their main jsonl was moved to the trash. The current trash flow handles this automatically; the subcommand is a safe, idempotent one-shot for already-leaked dirs.

## Status

Current release: **v0.3.2**. Picker, automatic `cd`, safe branch alignment, friendly empty state, shell completions, drill-down view, export / download (with sub-agent bundling), trash (with sub-agent dir co-handling), pinning, SDK-agent filter, first-message grouping, sub-agent drill-down, bulk prune, and session merge (claude-backed consolidation).

Still intentionally out:

- Post-hoc rename / label editing UI (Phase 2.x)
- Multiplexer-aware popup integration (Phase 2.x — standalone UX must mature first)
- Remote backup sync (Phase 3)
- AI-summarised export mode (Phase 3)

## License

MIT — see [LICENSE](LICENSE).
