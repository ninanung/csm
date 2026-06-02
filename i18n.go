package main

import (
	"os"
	"strings"
)

// Lang identifies a supported language for user-facing strings.
type Lang int

const (
	LangEN Lang = iota
	LangKO
)

// currentLang is read by T() at lookup time so a late --lang flag can override
// the auto-detected default.
var currentLang Lang

func init() {
	currentLang = detectLang()
}

// detectLang resolves the runtime language with this precedence:
//   1. CSM_LANG env var
//   2. LC_ALL / LC_MESSAGES / LANG env vars (POSIX locale)
//   3. fallback: English
func detectLang() Lang {
	for _, k := range []string{"CSM_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			return parseLang(v)
		}
	}
	return LangEN
}

func parseLang(v string) Lang {
	v = strings.ToLower(v)
	switch {
	case strings.HasPrefix(v, "ko"), strings.Contains(v, "korean"):
		return LangKO
	default:
		return LangEN
	}
}

// SetLang lets a CLI flag override the detected language.
func SetLang(v string) { currentLang = parseLang(v) }

// T looks up a translation for the current language. Returns the key itself when
// no translation is registered — surfaces missing keys instead of silently
// hiding them.
func T(key string) string {
	if t, ok := i18n[key]; ok {
		s := t[currentLang]
		if s == "" {
			s = t[LangEN] // fall back to EN if a row is missing a translation
		}
		return s
	}
	return key
}

// i18n is the central translation table. First slot: English, second: Korean.
var i18n = map[string][2]string{
	// ----- TUI: header / footer / hints -----
	"header.csm":          {"csm", "csm"},
	"filter.placeholder":  {"type to filter…", "필터링할 단어 입력…"},
	"of_total":            {"(of %d total)", "(전체 %d개 중)"},
	"no_message":          {"(no message)", "(메시지 없음)"},
	"more.above":          {"▲ more above", "▲ 위에 더 있음"},
	"more.below":          {"▼ more below", "▼ 아래에 더 있음"},
	"more.both":           {"▲▼ more", "▲▼ 더 있음"},
	"footer.normal":       {"↑/↓ or j/k · ^d/^u half-page · ^f/^b page · g/G top/bottom · enter select · / filter · q quit", "↑/↓ 또는 j/k · ^d/^u 반페이지 · ^f/^b 페이지 · g/G 처음/끝 · enter 선택 · / 필터 · q 종료"},
	"footer.filter":       {"↑/↓ navigate · enter select · esc cancel filter", "↑/↓ 이동 · enter 선택 · esc 필터 취소"},
	"footer.pick":         {"↑/↓ or j/k · enter select · esc abort", "↑/↓ 또는 j/k · enter 선택 · esc 중단"},
	"header.keys1":        {"↑/↓ or j/k navigate · enter select · / filter", "↑/↓ 또는 j/k 이동 · enter 선택 · / 필터"},
	"header.keys2":        {"^d/^u half-page · g/G top/bottom · q quit", "^d/^u 반페이지 · g/G 처음/끝 · q 종료"},
	"msgs":                {"msgs", "메시지"},

	// ----- humanized time -----
	"time.just_now": {"just now", "방금"},
	"time.m_ago":    {"%dm ago", "%d분 전"},
	"time.h_ago":    {"%dh ago", "%d시간 전"},
	"time.d_ago":    {"%dd ago", "%d일 전"},

	// ----- branch prompt -----
	"branch.title":          {"csm: branch %q not found locally", "csm: 브랜치 %q 가 로컬에 없음"},
	"branch.current_line":   {"       current: %s", "       현재: %s"},
	"branch.opt_stay":       {"stay on current branch (%s)", "현재 브랜치 유지 (%s)"},
	"branch.opt_pick":       {"pick from existing local branches", "기존 로컬 브랜치에서 선택"},
	"branch.opt_abort":      {"abort — do not start claude", "중단 — claude 시작하지 않음"},
	"branch.pick_title":     {"pick a branch to check out", "체크아웃할 브랜치 선택"},
	"branch.current_marker": {"  ← current", "  ← 현재"},

	// ----- runtime / error messages -----
	"msg.no_sessions":         {"csm: no sessions found under ~/.claude/projects", "csm: ~/.claude/projects 아래에 세션 없음"},
	"msg.load_failed":         {"csm: failed to load sessions: %v", "csm: 세션 로드 실패: %v"},
	"msg.cwd_missing":         {"csm: session cwd missing or absent (%q). starting in current dir.", "csm: 세션 cwd 없음 (%q). 현재 디렉토리에서 시작."},
	"msg.chdir_failed":        {"csm: chdir failed: %v", "csm: chdir 실패: %v"},
	"msg.claude_missing":      {"csm: 'claude' not found in PATH: %v", "csm: 'claude' 명령을 PATH 에서 찾을 수 없음: %v"},
	"msg.exec_failed":         {"csm: exec failed: %v", "csm: exec 실패: %v"},
	"msg.switched":            {"csm: switched to branch %q", "csm: 브랜치 %q 로 전환"},
	"msg.checkout_failed":     {"csm: git checkout %q failed: %v\n%s", "csm: git checkout %q 실패: %v\n%s"},
	"msg.staying_dirty":       {"csm: working tree dirty; staying on %q (session was on %q)", "csm: working tree 가 dirty; %q 유지 (세션은 %q 였음)"},
	"msg.in_progress":         {"csm: %s in progress; staying on %q", "csm: %s 진행 중; %q 유지"},
	"msg.branch_in_worktree":  {"csm: branch %q is checked out at %s; staying on %q", "csm: 브랜치 %q 가 %s 에서 체크아웃됨; %q 유지"},
	"msg.aborted":             {"csm: aborted", "csm: 중단됨"},

	// ----- friendly empty / self-check states -----
	"empty.no_claude.title":      {"Claude Code is not installed on this machine.", "이 머신에 Claude Code 가 설치돼 있지 않아요."},
	"empty.no_claude.hint":       {"Install it from https://docs.claude.com/claude-code, then try csm again.", "https://docs.claude.com/claude-code 에서 설치 후 다시 csm 을 실행해 주세요."},
	"empty.no_projects_dir.title":{"No Claude Code data yet.", "아직 Claude Code 데이터가 없어요."},
	"empty.no_projects_dir.hint": {"Run `claude` at least once to start a session, then come back.", "`claude` 를 한 번 실행해서 세션을 시작한 뒤 다시 와 주세요."},
	"empty.no_sessions.title":    {"No sessions yet.", "아직 세션이 없어요."},
	"empty.no_sessions.hint":     {"Start a session with `claude`. csm will pick it up automatically.", "`claude` 로 세션을 시작하면 csm 이 자동으로 잡아 줍니다."},
}
