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
	// header.keys1 / .keys2 are now built structurally — see helpKeysPrimary
	// / helpKeysSecondary in tui.go.
	"more.show":           {"▾ %d more  (enter to expand)", "▾ %d개 더  (enter 펼치기)"},
	"more.collapse":       {"▴ collapse", "▴ 접기"},

	// export / download
	"export.success":      {"✓ exported to %s", "✓ %s 에 export 완료"},
	"export.actions":      {"[o] open · [c] copy path · esc dismiss", "[o] 열기 · [c] 경로 복사 · esc 닫기"},
	"export.copied":       {"✓ path copied to clipboard", "✓ 경로 클립보드 복사됨"},
	"export.opening":      {"opening…", "여는 중…"},
	"export.failed":       {"✗ export failed: %v", "✗ export 실패: %v"},
	"download.summary":    {"✓ %d sessions exported to %s", "✓ %d개 세션을 %s 에 export"},
	"download.indexing":   {"writing index…", "인덱스 작성 중…"},

	// merge (consolidate N sessions via claude into the latest one; trash the rest)
	"merge.selected":      {"%d selected · m merge · esc clear", "%d개 선택 · m 합치기 · esc 해제"},
	"merge.need_two":      {"select at least 2 sessions to merge (space to mark)", "합치려면 세션을 2개 이상 선택하세요 (space 로 마킹)"},
	"merge.no_target":     {"no session under cursor to mark", "커서 위치에 마킹할 세션이 없음"},
	"merge.running":       {"consolidating via claude… (calls claude, may take a moment)", "claude 로 정리하는 중… (claude 호출, 잠시 걸릴 수 있음)"},
	"merge.success":       {"✓ consolidated into latest session · %d moved to trash — enter to resume", "✓ 최신 세션으로 정리·통합 · %d개 휴지통 이동 — enter 로 이어받기"},
	"merge.actions":       {"enter resume · esc dismiss", "enter 이어받기 · esc 닫기"},
	"merge.success_cli":   {"✓ consolidated into %s · %d session(s) moved to trash", "✓ %s 로 정리·통합 · %d개 휴지통 이동"},
	"merge.failed":        {"✗ merge failed: %v", "✗ 합치기 실패: %v"},
	"merge.not_found":     {"csm merge: session %q not found", "csm merge: 세션 %q 를 찾을 수 없음"},
	"merge.no_text":       {"selected sessions are empty — nothing to merge", "선택한 세션이 비어 있음 — 합칠 내용이 없음"},
	"merge.empty_result":  {"claude returned no content — nothing was changed", "claude 가 빈 응답을 반환함 — 변경된 것 없음"},
	"merge.failed_claude":  {"claude reported an error during consolidation — nothing was changed", "claude 가 정리 중 오류를 보고함 — 변경된 것 없음"},
	"merge.too_large":     {"combined sessions too large (~%dk chars > %dk cap) — merging this much isn't supported yet", "합친 입력이 너무 큼 (~%dk자 > %dk 한도) — 아직 이만큼은 지원하지 않음"},
	"merge.seed_user":     {"Consolidate my previous work sessions into one and continue from there.", "이전 작업 세션들을 하나로 정리·통합해줘. 이걸 이어서 작업할게."},
	"merge.prompt": {
		"You are given the full transcripts of several Claude Code work sessions, separated by lines like '===== SESSION N ====='. Consolidate them into ONE coherent record that organizes the combined work by topic and chronology, removes redundancy, and preserves concrete decisions, code changes, commands run, and open threads. Write it as if briefing someone who will continue this work. Write in English. Output only the consolidated record — no preamble, no meta commentary.",
		"여러 개의 Claude Code 작업 세션 전체 기록이 '===== SESSION N =====' 형태로 구분되어 주어집니다. 이들을 하나의 일관된 기록으로 정리·통합해 주세요. 합쳐진 작업을 주제와 시간 순서로 정리하고, 중복을 제거하고, 구체적인 결정·코드 변경·실행한 명령·미해결 사항을 보존하세요. 이 작업을 이어받을 사람에게 브리핑하듯 작성하세요. 한국어로 작성하세요. 정리된 본문만 출력하고, 서두나 부가 설명은 넣지 마세요.",
	},

	// trash / delete
	"trash.confirm_prompt":   {"Move this session to trash? [y/N]", "이 세션을 휴지통으로 옮길까요? [y/N]"},
	"trash.permdel_prompt":   {"Permanently delete this session? [y/N]", "이 세션을 영구 삭제할까요? [y/N]"},
	"trash.moved":            {"✓ moved to trash · t to view trash, r to restore", "✓ 휴지통으로 이동 · t 로 휴지통 보기, r 로 복구"},
	"trash.empty":            {"trash is empty", "휴지통이 비어 있음"},
	"trash.title":            {"trash", "휴지통"},
	"trash.permdel_done":     {"✓ permanently deleted", "✓ 영구 삭제됨"},
	"trash.restore_done":     {"✓ restored", "✓ 복구됨"},
	"trash.error":            {"✗ %v", "✗ %v"},
	"trash.no_target":        {"no session selected (move cursor onto a session)", "선택된 세션 없음 (커서를 세션 위로 이동)"},
	"trash.banner":           {"TRASH — esc or t to return", "휴지통 — esc 또는 t 로 돌아가기"},
	"trash.cleanup_orphans":  {"csm: consolidated %d orphan sub-agent dirs into trash", "csm: 분리된 sub-agent 디렉토리 %d개를 휴지통으로 통합"},

	// prune
	"prune.usage_missing_days": {"csm prune: missing <days> argument", "csm prune: <days> 인자가 필요합니다"},
	"prune.usage_bad_days":     {"csm prune: invalid <days> %q (want non-negative integer)", "csm prune: <days> %q 는 0 이상의 정수여야 합니다"},
	"prune.none":               {"no sessions older than %d days (pinned sessions excluded)", "%d일 이전 세션 없음 (핀 제외)"},
	"prune.preview_header":     {"%d sessions older than %d days will be %s", "%d개 세션이 %d일 이전 — %s 됩니다"},
	"prune.preview_range":      {"  oldest: %s   newest: %s", "  최오래된: %s   최신: %s"},
	"prune.preview_projects":   {"  projects: %s", "  프로젝트: %s"},
	"prune.dest_trash":         {"moved to trash", "휴지통으로 이동"},
	"prune.dest_permanent":     {"permanently deleted", "영구 삭제"},
	"prune.confirm":            {"proceed? [y/N]", "진행할까요? [y/N]"},
	"prune.cancelled":          {"prune cancelled", "취소됨"},
	"prune.dry_run_done":       {"(dry-run — no changes made)", "(dry-run — 변경 없음)"},
	"prune.done_trash":         {"✓ moved %d sessions to trash", "✓ %d개 세션을 휴지통으로 이동"},
	"prune.done_permanent":     {"✓ permanently deleted %d sessions", "✓ %d개 세션을 영구 삭제"},
	"prune.partial_fail":       {"%d sessions failed — see errors above", "%d개 세션 실패 — 위 에러 참조"},

	// SDK / agent session filter
	"agents.hidden":          {"agent sessions hidden", "에이전트 세션 숨김"},
	"agents.shown":           {"agent sessions shown", "에이전트 세션 표시"},
	"agents.hidden_count":    {"+%d agent sessions hidden (a to show)", "+%d개 에이전트 세션 숨김 (a 키로 표시)"},

	// help overlay
	"help.title":             {"KEYS", "키 안내"},
	"help.dismiss":           {"press ? or any key to close", "? 또는 아무 키나 눌러 닫기"},
	"help.section.navigate":  {"Navigate", "이동"},
	"help.section.session":   {"Session", "세션"},
	"help.section.manage":    {"Manage", "관리"},
	"help.section.filter":    {"Filter", "필터"},
	"help.section.other":     {"Other", "기타"},
	"help.move_cursor":       {"move cursor", "커서 이동"},
	"help.drill":             {"drill into project / back", "드릴 인 / 아웃"},
	"help.half_page":         {"half page", "반페이지"},
	"help.top_bottom":        {"first / last session", "처음 / 마지막"},
	"help.open":              {"open selected session", "선택한 세션 열기"},
	"help.export":            {"export raw JSONL (verbatim copy)", "원본 JSONL 그대로 export"},
	"help.mark":              {"mark/unmark for merge", "합치기 선택 toggle"},
	"help.merge":             {"consolidate marked sessions via claude", "선택 세션을 claude 로 정리·통합"},
	"help.pin":               {"toggle pin", "고정 toggle"},
	"help.delete":            {"move to trash (or permanent delete in trash view)", "휴지통으로 이동 (휴지통 뷰에선 영구 삭제)"},
	"help.trash_toggle":      {"toggle trash view", "휴지통 뷰 toggle"},
	"help.restore":           {"restore selected (in trash view)", "선택 복구 (휴지통 뷰)"},
	"help.toggle_agents":     {"show/hide SDK agent sessions", "SDK 에이전트 세션 보이기/숨기기"},
	"help.filter_start":      {"start fuzzy filter", "fuzzy 필터 시작"},
	"help.unwind":            {"cancel / unwind one level", "취소 / 한 단계 뒤로"},
	"help.help":              {"toggle this help", "이 도움말 toggle"},
	"help.quit":              {"quit without selecting", "선택하지 않고 종료"},

	// context-sensitive footer hint shown in the header's secondary help slot
	"help.trash_hint":        {"d permanently delete · r restore · esc back", "d 영구 삭제 · r 복구 · esc 돌아가기"},

	// pin
	"pin.added":           {"★ pinned", "★ 고정됨"},
	"pin.removed":         {"☆ unpinned", "☆ 고정 해제"},
	"pin.error":           {"✗ pin error: %v", "✗ pin 오류: %v"},
	"msgs":                {"msgs", "메시지"},
	"dup.suffix":          {"+%d similar", "+%d 유사"},
	"status.open":         {"open", "열기"},
	"status.copy":         {"copy path", "경로 복사"},

	// sub-agent drill-down
	"subagent.group_prefix": {"↳ sub-agents of", "↳ sub-agents:"},
	"subagent.none":         {"this session spawned no sub-agents", "이 세션은 sub-agent 를 띄우지 않았음"},
	"subagent.badge":        {"↳%d agents (s)", "↳%d 에이전트 (s)"},
	"help.subagent":         {"open sub-agent view", "sub-agent 보기"},

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
