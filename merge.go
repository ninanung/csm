package main

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
// The seed copies only the fields that place a line in the session (seedFields)
// from the target's latest lines. Per-turn fields (isMeta, requestRef,
// stop_reason, usage, …) would describe turns that no longer exist. The
// target's name, generated title and /cd·worktree location lines are kept.

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
	// The transcript goes in on stdin because argv has OS size limits.
	// --safe-mode and --tools "" keep the current directory's hooks, CLAUDE.md,
	// MCP servers and tools out of the call; --no-session-persistence keeps it
	// from leaving a session behind. --tools takes a list, so it stays last.
	cmd := exec.Command(claudePath, "-p", "--output-format", "json",
		"--no-session-persistence", "--safe-mode", "--tools", "")
	cmd.Stdin = strings.NewReader(T("merge.prompt") + "\n\n" + body)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if msg := strings.TrimSpace(string(ee.Stderr)); msg != "" {
				return "", 0, fmt.Errorf("%w: %s", err, msg)
			}
		}
		return "", 0, err
	}
	var resp struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", 0, err
	}
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
		if (r.Type != "user" && r.Type != "assistant") || r.IsMeta {
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

// writeSeedSession overwrites the target's jsonl with a two-line seed — a user
// turn (the merge request) and an assistant turn carrying the consolidation —
// followed by the target's kept name / title / location lines.
func writeSeedSession(target Session, consolidated string) error {
	src, err := readSeedSource(target.Path)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	u1, u2 := newUUID(), newUUID()

	l1 := seedBase(src.user, target.ID)
	l1["type"] = "user"
	l1["uuid"] = u1
	l1["parentUuid"] = nil
	l1["timestamp"] = now
	l1["message"] = map[string]interface{}{"role": "user", "content": T("merge.seed_user")}

	l2 := seedBase(src.assistant, target.ID)
	l2["type"] = "assistant"
	l2["uuid"] = u2
	l2["parentUuid"] = u1
	l2["timestamp"] = now
	msg := map[string]interface{}{
		"id":            "msg_" + strings.ReplaceAll(newUUID(), "-", ""),
		"type":          "message",
		"role":          "assistant",
		"content":       []interface{}{map[string]interface{}{"type": "text", "text": consolidated}},
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		// zero, not the template's: a long session's last usage would make the
		// two-line seed look near the context limit and trigger auto-compact
		"usage": map[string]interface{}{
			"input_tokens": 0, "output_tokens": 0,
			"cache_creation_input_tokens": 0, "cache_read_input_tokens": 0,
		},
	}
	if am, ok := src.assistant["message"].(map[string]interface{}); ok {
		if model, ok := am["model"]; ok {
			msg["model"] = model
		}
	}
	l2["message"] = msg

	return writeLines(target.Path, append([]map[string]interface{}{l1, l2}, src.keep...))
}

// seedFields are copied from the target's own lines so Claude Code places the
// seed where the session lives.
var seedFields = []string{"cwd", "gitBranch", "version", "entrypoint", "userType"}

func seedBase(tmpl map[string]interface{}, sessionID string) map[string]interface{} {
	l := map[string]interface{}{"isSidechain": false, "sessionId": sessionID}
	for _, k := range seedFields {
		if v, ok := tmpl[k]; ok {
			l[k] = v
		}
	}
	return l
}

// keptLineTypes survive the seed: the /rename name, the generated title and
// where /cd or a worktree moved the session.
var keptLineTypes = map[string]bool{"custom-title": true, "ai-title": true, "relocated": true, "worktree-state": true}

// seedSource is what the seed takes from the target: its latest user and
// assistant lines as field templates, and the last line of each kept type in
// file order. With no assistant line, the user line stands in for both.
type seedSource struct {
	user, assistant map[string]interface{}
	keep            []map[string]interface{}
}

func readSeedSource(path string) (seedSource, error) {
	var src seedSource
	f, err := os.Open(path)
	if err != nil {
		return src, err
	}
	defer f.Close()

	type keptLine struct {
		idx int
		raw []byte
	}
	var userRaw, assistantRaw []byte
	kept := map[string]keptLine{}
	sc := newLineScanner(f)
	for i := 0; sc.Scan(); i++ {
		var l struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		switch {
		case l.Type == "user":
			userRaw = append(userRaw[:0], sc.Bytes()...)
		case l.Type == "assistant":
			assistantRaw = append(assistantRaw[:0], sc.Bytes()...)
		case keptLineTypes[l.Type]:
			kept[l.Type] = keptLine{i, append([]byte(nil), sc.Bytes()...)}
		}
	}
	if err := sc.Err(); err != nil {
		return src, err
	}
	if userRaw == nil || json.Unmarshal(userRaw, &src.user) != nil {
		return src, errors.New(T("merge.no_text"))
	}
	src.assistant = src.user
	if assistantRaw != nil {
		var a map[string]interface{}
		if json.Unmarshal(assistantRaw, &a) == nil {
			src.assistant = a
		}
	}
	ordered := make([]keptLine, 0, len(kept))
	for _, k := range kept {
		ordered = append(ordered, k)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].idx < ordered[j].idx })
	for _, k := range ordered {
		var m map[string]interface{}
		if json.Unmarshal(k.raw, &m) == nil {
			src.keep = append(src.keep, m)
		}
	}
	return src, nil
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
