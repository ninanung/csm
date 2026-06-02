package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Session struct {
	ID           string
	Path         string
	CWD          string
	GitBranch    string
	FirstMessage string
	LastActivity time.Time
	MessageCount int
	Project      string // basename of CWD or its parent
}

type rawLine struct {
	Type      string          `json:"type"`
	CWD       string          `json:"cwd"`
	GitBranch string          `json:"gitBranch"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// LoadSessions scans ~/.claude/projects and returns all session metadata.
func LoadSessions() ([]Session, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(home, ".claude", "projects")

	var sessions []Session
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		projectDir := filepath.Join(root, e.Name())
		files, err := os.ReadDir(projectDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			s, err := loadSession(filepath.Join(projectDir, f.Name()))
			if err != nil {
				continue // skip malformed; future: log
			}
			sessions = append(sessions, s)
		}
	}

	// sort: most recent activity first
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].LastActivity.After(sessions[j].LastActivity)
	})
	return sessions, nil
}

func loadSession(path string) (Session, error) {
	s := Session{
		Path: path,
		ID:   strings.TrimSuffix(filepath.Base(path), ".jsonl"),
	}

	f, err := os.Open(path)
	if err != nil {
		return s, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// some sessions have very long lines (large pasted content); raise buffer
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		var r rawLine
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}

		if s.CWD == "" && r.CWD != "" {
			s.CWD = r.CWD
		}
		if s.GitBranch == "" && r.GitBranch != "" {
			s.GitBranch = r.GitBranch
		}
		if r.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, r.Timestamp); err == nil {
				if t.After(s.LastActivity) {
					s.LastActivity = t
				}
			}
		}
		// count "message-bearing" entries (user/assistant)
		if r.Type == "user" || r.Type == "assistant" {
			s.MessageCount++
		}
		if s.FirstMessage == "" && r.Type == "user" && len(r.Message) > 0 {
			s.FirstMessage = extractFirstText(r.Message)
		}
	}

	if s.LastActivity.IsZero() {
		// fall back to file mtime
		if fi, err := os.Stat(path); err == nil {
			s.LastActivity = fi.ModTime()
		}
	}

	s.Project = deriveProject(s.CWD)
	return s, nil
}

func extractFirstText(raw json.RawMessage) string {
	var m rawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	// Content may be a plain string or an array of content blocks
	var asString string
	if err := json.Unmarshal(m.Content, &asString); err == nil {
		return cleanText(asString)
	}
	var blocks []contentBlock
	if err := json.Unmarshal(m.Content, &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return cleanText(b.Text)
			}
		}
	}
	return ""
}

func cleanText(s string) string {
	s = strings.TrimSpace(s)
	// drop system-reminder blocks (frequently leading user messages)
	if strings.HasPrefix(s, "<") {
		// strip leading XML-ish tags
		for strings.HasPrefix(s, "<") {
			end := strings.Index(s, ">")
			if end < 0 {
				break
			}
			closeTag := strings.Index(s, "</")
			if closeTag < 0 {
				break
			}
			closeEnd := strings.Index(s[closeTag:], ">")
			if closeEnd < 0 {
				break
			}
			s = strings.TrimSpace(s[closeTag+closeEnd+1:])
		}
	}
	// take first non-empty line
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func deriveProject(cwd string) string {
	if cwd == "" {
		return "(unknown)"
	}
	return filepath.Base(cwd)
}
