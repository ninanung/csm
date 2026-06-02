#!/usr/bin/env bash
# tmux popup adapter for csm.
#
# Usage: bind this to a tmux key, e.g.:
#   bind-key s display-popup -E -w 80% -h 70% \
#     "/full/path/to/scripts/tmux-popup.sh #{pane_id}"
#
# Behavior: runs `csm --print` inside the popup. On selection, sends keys to
# the originating pane to exit the current claude process, cd to the selected
# session's cwd, and resume that session.

set -euo pipefail

TARGET_PANE="${1:-}"
if [[ -z "$TARGET_PANE" ]]; then
  echo "tmux-popup.sh: missing target pane id" >&2
  exit 1
fi

# Run picker; capture selection. Non-zero (e.g., user cancelled) → exit silently.
SELECTION="$(csm --print)" || exit 0

SESSION_ID="$(printf '%s' "$SELECTION" | awk -F '\t' '{print $1}')"
CWD="$(printf '%s' "$SELECTION" | awk -F '\t' '{print $2}')"

if [[ -z "$SESSION_ID" ]]; then
  exit 0
fi

# Send to originating pane:
#   1. Ctrl-D (or `/exit` then enter) to leave current claude — Ctrl-D is fragile
#      mid-prompt, so use `/exit` slash command instead.
#   2. Wait briefly for shell prompt.
#   3. cd && claude --resume.
tmux send-keys -t "$TARGET_PANE" "/exit" Enter
# Small delay so the current claude finishes shutdown before the cd line is read.
sleep 0.3
if [[ -n "$CWD" && -d "$CWD" ]]; then
  tmux send-keys -t "$TARGET_PANE" "cd $(printf '%q' "$CWD") && claude --resume $SESSION_ID" Enter
else
  tmux send-keys -t "$TARGET_PANE" "claude --resume $SESSION_ID" Enter
fi
