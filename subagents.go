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

// SubAgent is the metadata-light view of a sub-agent spawn inside a session.
// It powers the sub-agent drill-down view (press `s` on a session row).
type SubAgent struct {
	Path         string    // absolute path to the agent's .jsonl
	AgentID      string    // agentId field from inside the jsonl
	AgentType    string    // from sibling .meta.json ("plan-reviewer", etc.)
	Description  string    // from sibling .meta.json
	FirstMessage string    // first user-line content
	MessageCount int       // user + assistant rows
	LastActivity time.Time // latest timestamp in the jsonl, else file mtime
}

type subAgentMeta struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
}

// LoadSubAgents reads the sub-agent jsonl files (and matching .meta.json
// sidecars) under <session>/subagents/ and returns them sorted by most-recent
// activity first. Returns an empty slice when the directory is absent.
func LoadSubAgents(s Session) ([]SubAgent, error) {
	uuid := strings.TrimSuffix(filepath.Base(s.Path), ".jsonl")
	dir := filepath.Join(filepath.Dir(s.Path), uuid, "subagents")
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var agents []SubAgent
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		full := filepath.Join(dir, name)
		a, err := loadSubAgent(full)
		if err != nil {
			continue
		}
		agents = append(agents, a)
	}
	sort.SliceStable(agents, func(i, j int) bool {
		return agents[i].LastActivity.After(agents[j].LastActivity)
	})
	return agents, nil
}

func loadSubAgent(path string) (SubAgent, error) {
	a := SubAgent{Path: path}

	// .meta.json sidecar — agentType / description live here.
	metaPath := strings.TrimSuffix(path, ".jsonl") + ".meta.json"
	if b, err := os.ReadFile(metaPath); err == nil {
		var m subAgentMeta
		if err := json.Unmarshal(b, &m); err == nil {
			a.AgentType = m.AgentType
			a.Description = m.Description
		}
	}

	f, err := os.Open(path)
	if err != nil {
		return a, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		var r rawLine
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			continue
		}
		if r.Type == "user" || r.Type == "assistant" {
			a.MessageCount++
		}
		if a.FirstMessage == "" && r.Type == "user" && len(r.Message) > 0 {
			a.FirstMessage = extractFirstText(r.Message)
		}
		if r.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, r.Timestamp); err == nil && t.After(a.LastActivity) {
				a.LastActivity = t
			}
		}
	}

	// agentId lives in the same first user line we parsed above — but rawLine
	// drops it. Re-scan once with a richer struct.
	if a.AgentID == "" {
		if id, err := readAgentID(path); err == nil {
			a.AgentID = id
		}
	}

	if a.LastActivity.IsZero() {
		if fi, err := os.Stat(path); err == nil {
			a.LastActivity = fi.ModTime()
		}
	}
	return a, nil
}

func readAgentID(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		var probe struct {
			AgentID string `json:"agentId"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &probe); err == nil && probe.AgentID != "" {
			return probe.AgentID, nil
		}
	}
	return "", nil
}
