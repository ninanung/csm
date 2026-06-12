package main

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Merge — "합치기". Consolidate N sessions into ONE resumable session via the
// local `claude` binary:
//
//  1. Extract every session's conversation text and hand the combined
//     transcript to `claude -p`, which reorganizes it into a single clean,
//     de-duplicated record (the "정리" step).
//  2. Seed the *latest* selected session (the target) with that consolidation
//     as a user→assistant exchange, so `claude --resume <target>` continues
//     from the organized context. The target keeps its id.
//  3. Move the other (folded-in) sessions to the csm trash — recoverable.
//
// The seed reuses the target's own jsonl lines as templates so every Claude
// Code metadata field (cwd, gitBranch, version, userType, …) stays valid.

// mergeInputCharCap bounds how much transcript we feed claude at once, to stay
// within the model's context window. Oversize input is a guided stop rather
// than a silent truncation.
const mergeInputCharCap = 200_000

var systemReminderRE = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)

// mergeTurn is one extracted user/assistant turn.
type mergeTurn struct {
	role string
	text string
	ts   time.Time
}

// MergeConsolidate runs the full merge: consolidate via claude, seed the target
// session, and trash the folded-in sources. Returns the target id and the
// number of sessions moved to trash. Nothing is mutated until after the claude
// call succeeds.
func MergeConsolidate(sessions []Session) (string, int, error) {
	if len(sessions) < 2 {
		return "", 0, errors.New(T("merge.need_two"))
	}
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return "", 0, fmt.Errorf(T("msg.claude_missing"), err)
	}

	ordered := append([]Session(nil), sessions...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].LastActivity.Before(ordered[j].LastActivity)
	})
	target := ordered[len(ordered)-1]

	body, total, err := buildMergeBody(ordered)
	if err != nil {
		return "", 0, err
	}
	if total == 0 {
		return "", 0, errors.New(T("merge.no_text"))
	}
	if total > mergeInputCharCap {
		return "", 0, fmt.Errorf(T("merge.too_large"), total/1000, mergeInputCharCap/1000)
	}

	// claude consolidation — the only failure-prone step, run before any mutation.
	// --output-format json gives us both the result text and the session_id of
	// this headless call, so we can delete the call's own session afterward
	// (otherwise every merge leaves a junk session in ~/.claude/projects).
	out, err := exec.Command(claudePath, "-p", T("merge.prompt")+"\n\n"+body, "--output-format", "json").Output()
	if err != nil {
		return "", 0, err
	}
	var resp struct {
		Result    string `json:"result"`
		SessionID string `json:"session_id"`
		IsError   bool   `json:"is_error"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", 0, err
	}
	removeCallSession(resp.SessionID) // drop the headless call's own session
	if resp.IsError {
		return "", 0, errors.New(T("merge.failed_claude"))
	}
	consolidated := strings.TrimSpace(resp.Result)
	if consolidated == "" {
		return "", 0, errors.New(T("merge.empty_result"))
	}

	// seed the target (overwrites its file) then trash the rest.
	if err := writeSeedSession(target, consolidated); err != nil {
		return "", 0, err
	}
	folded := 0
	for _, s := range ordered {
		if s.ID == target.ID {
			continue
		}
		if _, err := MoveToTrash(s); err == nil {
			folded++
		}
	}
	return target.ID, folded, nil
}

// removeCallSession permanently deletes the session jsonl (and any sidecar
// dir) that `claude -p` created for our headless consolidation call, so merges
// don't accumulate junk sessions in ~/.claude/projects. Best-effort: a missing
// file or unreadable dir is silently ignored.
func removeCallSession(sessionID string) {
	if sessionID == "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	root := filepath.Join(home, ".claude", "projects")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		jsonl := filepath.Join(root, e.Name(), sessionID+".jsonl")
		if _, err := os.Stat(jsonl); err != nil {
			continue
		}
		_ = os.Remove(jsonl)
		_ = os.RemoveAll(filepath.Join(root, e.Name(), sessionID))
		return
	}
}

// buildMergeBody concatenates each session's turns into a single labeled
// transcript and returns it with its total character count.
func buildMergeBody(sessions []Session) (string, int, error) {
	var b strings.Builder
	total := 0
	for i, s := range sessions {
		project := s.Project
		if project == "" {
			project = "(unknown)"
		}
		fmt.Fprintf(&b, "\n===== SESSION %d — %s (%s) =====\n", i+1, project, s.ID)
		turns, err := extractTurns(s)
		if err != nil {
			return "", 0, err
		}
		for _, t := range turns {
			entry := fmt.Sprintf("[%s]\n%s\n\n", strings.ToUpper(t.role), t.text)
			b.WriteString(entry)
			total += len(entry)
		}
	}
	return b.String(), total, nil
}

// extractTurns reads a session jsonl and returns its user/assistant turns with
// text content, in file order. Reuses session.go's rawLine/rawMessage types.
func extractTurns(s Session) ([]mergeTurn, error) {
	f, err := os.Open(s.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)

	var turns []mergeTurn
	for scanner.Scan() {
		var r rawLine
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			continue
		}
		if r.Type != "user" && r.Type != "assistant" {
			continue
		}
		text := strings.TrimSpace(extractFullText(r.Message))
		if text == "" {
			continue
		}
		var ts time.Time
		if r.Timestamp != "" {
			ts, _ = time.Parse(time.RFC3339, r.Timestamp)
		}
		turns = append(turns, mergeTurn{role: r.Type, text: text, ts: ts})
	}
	return turns, scanner.Err()
}

// extractFullText joins every text block of a message (unlike session.go's
// extractFirstText). Tool-use/result blocks are skipped; system-reminder spans
// are stripped so injected context doesn't pollute the merge input.
func extractFullText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m rawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	var asString string
	if err := json.Unmarshal(m.Content, &asString); err == nil {
		return cleanReminders(asString)
	}
	var blocks []contentBlock
	if err := json.Unmarshal(m.Content, &blocks); err == nil {
		var parts []string
		for _, blk := range blocks {
			if blk.Type == "text" && strings.TrimSpace(blk.Text) != "" {
				parts = append(parts, blk.Text)
			}
		}
		return cleanReminders(strings.Join(parts, "\n"))
	}
	return ""
}

func cleanReminders(s string) string {
	return strings.TrimSpace(systemReminderRE.ReplaceAllString(s, ""))
}

// writeSeedSession overwrites the target's jsonl with a two-line seed: a user
// turn (the merge request) and an assistant turn carrying the consolidation.
// Both lines clone real template lines from the target so metadata stays valid.
func writeSeedSession(target Session, consolidated string) error {
	uBase, aBase, err := seedTemplates(target.Path)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	u1, u2 := newUUID(), newUUID()

	l1 := cloneLine(uBase)
	l1["type"] = "user"
	l1["uuid"] = u1
	l1["parentUuid"] = nil
	l1["sessionId"] = target.ID
	l1["timestamp"] = now
	l1["message"] = map[string]interface{}{"role": "user", "content": T("merge.seed_user")}

	l2 := cloneLine(aBase)
	l2["type"] = "assistant"
	l2["uuid"] = u2
	l2["parentUuid"] = u1
	l2["sessionId"] = target.ID
	l2["timestamp"] = now
	msg, _ := l2["message"].(map[string]interface{})
	if msg == nil {
		msg = map[string]interface{}{}
	}
	msg["role"] = "assistant"
	msg["content"] = []interface{}{map[string]interface{}{"type": "text", "text": consolidated}}
	l2["message"] = msg

	return writeLines(target.Path, []map[string]interface{}{l1, l2})
}

// seedTemplates returns the first user line and first assistant line of a
// session as decoded maps. When the session has no assistant line, the user
// line stands in for both (resume tolerates the looser shape).
func seedTemplates(path string) (map[string]interface{}, map[string]interface{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)

	var uBase, aBase map[string]interface{}
	for scanner.Scan() {
		var obj map[string]interface{}
		if json.Unmarshal(scanner.Bytes(), &obj) != nil {
			continue
		}
		switch obj["type"] {
		case "user":
			if uBase == nil {
				uBase = obj
			}
		case "assistant":
			if aBase == nil {
				aBase = obj
			}
		}
		if uBase != nil && aBase != nil {
			break
		}
	}
	if uBase == nil {
		return nil, nil, errors.New(T("merge.no_text"))
	}
	if aBase == nil {
		aBase = uBase
	}
	return uBase, aBase, nil
}

// cloneLine deep-copies a decoded jsonl line via a JSON round-trip.
func cloneLine(m map[string]interface{}) map[string]interface{} {
	b, _ := json.Marshal(m)
	var c map[string]interface{}
	_ = json.Unmarshal(b, &c)
	return c
}

func writeLines(path string, lines []map[string]interface{}) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, l := range lines {
		b, err := json.Marshal(l)
		if err != nil {
			return err
		}
		w.Write(b)
		w.WriteByte('\n')
	}
	return w.Flush()
}

// newUUID returns a random RFC 4122 v4 uuid, matching Claude Code's session id
// format. Uses crypto/rand — no external dependency.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%x", b)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return strings.Join([]string{
		fmt.Sprintf("%x", b[0:4]),
		fmt.Sprintf("%x", b[4:6]),
		fmt.Sprintf("%x", b[6:8]),
		fmt.Sprintf("%x", b[8:10]),
		fmt.Sprintf("%x", b[10:16]),
	}, "-")
}
