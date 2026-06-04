package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ---- export options ----

type ExportOptions struct {
	// IncludeThinking emits the assistant's thinking blocks. Default false.
	IncludeThinking bool
	// MaxToolOutputLines truncates each tool result block. 0 = no limit.
	MaxToolOutputLines int
}

func defaultExportOptions() ExportOptions {
	return ExportOptions{
		IncludeThinking:    false,
		MaxToolOutputLines: 80,
	}
}

// ---- JSONL message parsing (deeper than session.go) ----

// exportLine is a more thorough view of a JSONL line than session.go's rawLine.
// We keep the structures local so session.go stays narrow.
type exportLine struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	CWD       string          `json:"cwd"`
	GitBranch string          `json:"gitBranch"`
	Message   json.RawMessage `json:"message"`
	ToolUseID string          `json:"toolUseID"`
}

type exportMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type exportContentBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`     // tool_use
	Input    json.RawMessage `json:"input"`    // tool_use
	Content  json.RawMessage `json:"content"`  // tool_result (string or [{type,text}])
	IsError  bool            `json:"is_error"` // tool_result
}

// ---- ExportSession ----

// ExportSession streams a markdown rendering of the given session to w.
func ExportSession(w io.Writer, s Session, opts ExportOptions) error {
	f, err := os.Open(s.Path)
	if err != nil {
		return fmt.Errorf("open session: %w", err)
	}
	defer f.Close()

	writeFrontmatter(w, s)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)

	for scanner.Scan() {
		var line exportLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		switch line.Type {
		case "user":
			renderUserMessage(w, line)
		case "assistant":
			renderAssistantMessage(w, line, opts)
		}
	}
	return scanner.Err()
}

// ---- rendering ----

func writeFrontmatter(w io.Writer, s Session) {
	fmt.Fprintln(w, "---")
	fmt.Fprintf(w, "session_id: %s\n", s.ID)
	if s.Project != "" {
		fmt.Fprintf(w, "project: %s\n", s.Project)
	}
	if s.CWD != "" {
		fmt.Fprintf(w, "cwd: %s\n", s.CWD)
	}
	if s.GitBranch != "" {
		fmt.Fprintf(w, "branch: %s\n", s.GitBranch)
	}
	if !s.LastActivity.IsZero() {
		fmt.Fprintf(w, "last_activity: %s\n", s.LastActivity.Format(time.RFC3339))
	}
	if s.MessageCount > 0 {
		fmt.Fprintf(w, "message_count: %d\n", s.MessageCount)
	}
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)

	if s.FirstMessage != "" {
		fmt.Fprintf(w, "# %s\n\n", oneLine(s.FirstMessage))
	}
}

func renderUserMessage(w io.Writer, line exportLine) {
	text := extractUserText(line.Message)
	if text == "" {
		return
	}
	ts := humanizeTime(line.Timestamp)
	fmt.Fprintf(w, "## User · %s\n\n%s\n\n", ts, text)
}

func renderAssistantMessage(w io.Writer, line exportLine, opts ExportOptions) {
	var m exportMessage
	if err := json.Unmarshal(line.Message, &m); err != nil {
		return
	}
	blocks := decodeBlocks(m.Content)
	if len(blocks) == 0 {
		return
	}

	ts := humanizeTime(line.Timestamp)
	headerWritten := false
	writeHeaderOnce := func() {
		if !headerWritten {
			fmt.Fprintf(w, "## Claude · %s\n\n", ts)
			headerWritten = true
		}
	}

	for _, b := range blocks {
		switch b.Type {
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" {
				writeHeaderOnce()
				fmt.Fprintf(w, "%s\n\n", t)
			}
		case "thinking":
			if opts.IncludeThinking {
				if t := strings.TrimSpace(b.Thinking); t != "" {
					writeHeaderOnce()
					fmt.Fprintf(w, "> _thinking_ %s\n\n", oneLine(t))
				}
			}
		case "tool_use":
			writeHeaderOnce()
			renderToolUse(w, b)
		case "tool_result":
			// tool_result usually arrives under a user-typed line right after
			// the tool_use. Render inline anyway.
			renderToolResult(w, b, opts)
		}
	}
}

func renderToolUse(w io.Writer, b exportContentBlock) {
	input := prettyJSON(b.Input)
	fmt.Fprintf(w, "<details><summary>🛠 %s</summary>\n\n", escapeMarkdown(b.Name))
	if input != "" {
		fmt.Fprintf(w, "```json\n%s\n```\n\n", input)
	}
	fmt.Fprintln(w, "</details>")
	fmt.Fprintln(w)
}

func renderToolResult(w io.Writer, b exportContentBlock, opts ExportOptions) {
	text := extractToolResultText(b.Content)
	if text == "" {
		return
	}
	if opts.MaxToolOutputLines > 0 {
		text = truncateLines(text, opts.MaxToolOutputLines)
	}
	label := "tool output"
	if b.IsError {
		label = "tool output (error)"
	}
	fmt.Fprintf(w, "<details><summary>📄 %s</summary>\n\n", label)
	fmt.Fprintf(w, "```\n%s\n```\n\n", text)
	fmt.Fprintln(w, "</details>")
	fmt.Fprintln(w)
}

// ---- content helpers ----

// extractUserText pulls the user-facing text out of a user message, filtering
// system-reminder blocks and tool_result placeholder lines that aren't useful
// in a transcript view.
func extractUserText(raw json.RawMessage) string {
	var m exportMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	// Content can be string or array
	var asString string
	if err := json.Unmarshal(m.Content, &asString); err == nil {
		return cleanUserText(asString)
	}
	blocks := decodeBlocks(m.Content)
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" {
				parts = append(parts, cleanUserText(t))
			}
		case "tool_result":
			// emit as a folded section so the transcript still shows
			// surrounding context even if we don't dump every byte
			text := extractToolResultText(b.Content)
			if text != "" {
				if len(text) > 400 {
					text = text[:400] + "…"
				}
				parts = append(parts, "_tool result_:\n```\n"+text+"\n```")
			}
		}
	}
	out := strings.Join(parts, "\n\n")
	return strings.TrimSpace(out)
}

var sysReminderRE = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
var localCmdRE = regexp.MustCompile(`(?s)<local-command-stdout>.*?</local-command-stdout>`)
var commandMsgRE = regexp.MustCompile(`(?s)<command-(?:name|message|args)>.*?</command-(?:name|message|args)>`)

func cleanUserText(s string) string {
	s = sysReminderRE.ReplaceAllString(s, "")
	s = localCmdRE.ReplaceAllString(s, "")
	s = commandMsgRE.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	return s
}

func decodeBlocks(raw json.RawMessage) []exportContentBlock {
	var blocks []exportContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return blocks
	}
	// fallback: string content treated as single text block
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil && asString != "" {
		return []exportContentBlock{{Type: "text", Text: asString}}
	}
	return nil
}

func extractToolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}
	blocks := decodeBlocks(raw)
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// ---- formatting helpers ----

func humanizeTime(s string) string {
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Format("2006-01-02 15:04")
	}
	return s
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(out)
}

func truncateLines(s string, max int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= max {
		return s
	}
	kept := lines[:max]
	return strings.Join(kept, "\n") + fmt.Sprintf("\n… (%d more lines truncated)", len(lines)-max)
}

func escapeMarkdown(s string) string {
	// minimal — we only worry about chars that would break our scaffolding
	return strings.ReplaceAll(s, "`", "\\`")
}

func oneLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			return l
		}
	}
	return ""
}

// ---- file IO helpers used by main / TUI ----

// ExportSessionToFile writes the rendered session to a stable, slugged filename
// under the user's csm-exports directory (or the supplied dir override).
// Returns the absolute output path.
func ExportSessionToFile(s Session, outDir string) (string, error) {
	if outDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		outDir = filepath.Join(home, "Documents", "csm-exports")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}

	name := exportFileName(s)
	path := filepath.Join(outDir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if err := ExportSession(f, s, defaultExportOptions()); err != nil {
		return "", err
	}
	return path, nil
}

func exportFileName(s Session) string {
	date := "session"
	if !s.LastActivity.IsZero() {
		date = s.LastActivity.Format("2006-01-02")
	}
	parts := []string{date}
	if s.Project != "" {
		parts = append(parts, slug(s.Project))
	}
	if s.FirstMessage != "" {
		parts = append(parts, slug(oneLine(s.FirstMessage)))
	}
	name := strings.Join(parts, "-")
	// truncate to a sane length so filesystems behave
	if len(name) > 80 {
		name = name[:80]
	}
	return name + ".md"
}

var slugSafeRE = regexp.MustCompile(`[^a-z0-9가-힣-]+`)

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugSafeRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = s[:40]
		s = strings.TrimRight(s, "-")
	}
	return s
}
