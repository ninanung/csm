# CLAUDE.md — csm 코드 작성 규칙

이 문서는 **csm 코드를 변경할 때 따라야 하는 규칙**만 적는다. 프로젝트가 무엇인지, 어떻게 설치/사용하는지는 README 를 참조한다. 일반 Go/bubbletea 베스트 프랙티스는 적지 않는다.

## 빠른 명령

```bash
go build ./...                    # 컴파일 확인
go install .                      # ~/go/bin 에 설치
go run . [args]                   # 소스에서 실행 (테스트용)
go run . --lang ko                # 한국어 UI 확인
go run . --lang en                # 영어 UI 확인
csm version                       # splash 출력 확인
```

릴리스는 절대 수동으로 하지 않는다 — 태그만 푸시한다:

```bash
git tag -a vX.Y.Z -m "..."
git push origin vX.Y.Z
# GitHub Actions 가 GoReleaser 로 자동 빌드·release·brew Formula 갱신
```

## 파일별 책임 — 새 코드 어디에 둘지

| 파일 | 책임 | 새 코드 이 파일에 넣는 기준 |
|---|---|---|
| `main.go` | 진입점, 플래그 파싱, 세션 선택 후 cd + checkout + exec 흐름 | 새 CLI 플래그·서브커맨드 |
| `session.go` | `~/.claude/projects/**/*.jsonl` 파싱과 `Session` 모델 | JSONL 새 필드 추출, 정렬 로직 |
| `git.go` | `GitState` 검사 + 로컬 브랜치 목록 등 git 헬퍼 | git 상태 검사 추가 |
| `tui.go` | bubbletea `Model` (메인 picker) | 키 바인딩, 렌더링, 뷰포트 |
| `prompt.go` | 별도 bubbletea picker — branch missing 인터랙티브 선택 | 추가 인터랙티브 prompt |
| `empty.go` | preflight 환경 검사 + friendly empty state 렌더링 | 신규 "환경/데이터 부재" 케이스 |
| `completion.go` | bash·zsh·fish 자동완성 스크립트 + `completion` 서브커맨드 | 플래그·서브커맨드 추가 시 세 스크립트 동시 갱신 |
| `export.go` | 원본 JSONL verbatim 복사 (`ExportSessionToFile` / `CopySession`) + 파일명 slug | 파일명 규칙·copy 동작 |
| `cmd_export.go` | `csm export <id>` CLI 서브커맨드 | export 관련 새 플래그 |
| `cmd_download.go` | `csm download` 일괄 export (dir·zip) + `_index.md` TOC | bulk 옵션·필터 추가 |
| `merge.go` | N개 세션을 로컬 `claude -p` 로 정리·통합 (`MergeConsolidate`) → 최신(타겟) 세션에 시딩 + 나머지는 휴지통 | 정리 프롬프트·시딩 규칙 |
| `cmd_merge.go` | `csm merge <id> <id>…` CLI 서브커맨드 | merge 관련 새 플래그 |
| `trash.go` | `~/.claude/csm/trash/` 이동·복구·영구 삭제·`LoadTrashSessions` | 휴지통 정책 변경 |
| `pins.go` | `~/.claude/csm/pins.json` 사이드카 read/write/toggle | Pin 관련 메타 필드 |
| `os_helpers.go` | `openInOS` / `copyToClipboard` cross-platform | OS 통합 추가 |
| `i18n.go` | `T()` 룩업 + 영/한 번역 사전 | **모든** 신규 사용자 노출 문자열 |
| `version.go` | `Version` (ldflags 주입), splash 출력 | splash 레이아웃 변경 |

새 책임이 위 분류 어디에도 안 맞을 때만 새 파일을 만든다. 카테고리당 한 파일을 유지.

## 코어 규칙 (Critical)

### 1. 모든 사용자 노출 문자열은 `T()` 로

```go
// ❌
fmt.Fprintln(os.Stderr, "csm: branch not found")

// ✅
fmt.Fprintln(os.Stderr, T("msg.branch_missing"))
```

새 키를 `i18n.go` 의 `i18n` 맵에 영문/한국어 **둘 다** 등록한다. 영문만 추가하고 한국어 비워두지 않는다.

키 네이밍: 점 구분 네임스페이스 — `msg.*`, `branch.*`, `footer.*`, `header.*`, `time.*` 등 기존 그룹에 맞춰 넣는다.

### 2. TUI 출력은 stderr, stdout 은 결과 전용

```go
prog := tea.NewProgram(m, tea.WithOutput(os.Stderr))
```

`stdout` 은 `--print` 모드의 `<id>\t<cwd>` 와 `csm version` 의 splash 가 점유한다. TUI 가 stdout 으로 새면 어댑터 스크립트가 깨진다. 새 TUI 모델 추가 시에도 동일.

### 3. `syscall.Exec` 로 claude 실행 (자식 프로세스 아님)

```go
syscall.Exec(claudePath, []string{"claude", "--resume", id}, os.Environ())
```

현재 프로세스를 replace 해야 셸 PID 트리가 깨끗하고 claude 가 cwd 를 정상 인계받는다. `exec.Command(...).Run()` 으로 바꾸지 않는다.

### 4. 모든 git mutation 은 안전 가드 통과 후

`git checkout` 같은 워킹 트리 변경 명령은 `CheckGitState` 로 다음 조건 검증 후에만:
- 워킹 트리 clean
- 대상 브랜치 존재 (없으면 `promptMissingBranch` 분기)
- rebase / merge / cherry-pick 진행 중 아님
- 다른 worktree 에 점유 안 됨

새 git mutation 추가 시 `main.go` 의 기존 `switch` 케이스 패턴을 그대로 따른다.

### 5. 브랜치 추출은 *마지막* 시점

`session.go` 의 JSONL 파싱에서 `gitBranch` 는 매 메시지마다 덮어쓴다. 첫 값을 보존하지 않는다. 세션 도중 브랜치가 바뀌었으면 마지막 상태로 복귀해야 자연스러움.

## 패턴 규칙 (Warning)

### 6. bubbletea Model 변경 후 rebuildContent + scrollToCursor

커서/필터/사이즈/필터링 상태 등 viewport 에 영향을 주는 모든 변경 직후:

```go
m.cursor = newCursor
m.rebuildContent()
m.scrollToCursor()
```

순서 중요: rebuild → scroll. rebuild 가 `rowLines[]` 를 갱신해야 scroll 이 올바른 라인 계산.

필터링 상태 토글은 헤더 높이를 바꾸므로 `m.resize()` 도 함께 호출:

```go
m.filtering = true
m.resize()
m.rebuildContent()
m.scrollToCursor()
```

### 7. 문자열 폭 측정은 컨텍스트별로 다른 함수

- **ANSI 코드 포함된 렌더링된 문자열**: `lipgloss.Width(s)`
- **plain text (한글·이모지 등 wide char 포함)**: `runewidth.StringWidth(s)`
- **truncation**: `runewidth.Truncate(s, w, "…")` — 바이트 슬라이싱 금지

ANSI 가 섞이면 `runewidth` 가 오작동한다. 두 함수 헷갈리지 않게 사용 시점 변수명에서 `Plain` 접미사 등을 명시.

### 8. Lipgloss 스타일은 패키지 변수로 중앙화

스타일을 함수 안에서 인라인 생성하지 않는다:

```go
// ❌
text := lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Render(s)

// ✅
var styleAccent = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
// ... 함수 안에서:
text := styleAccent.Render(s)
```

**배치 기준**:
- **splash 출력 전용** 스타일 (`styleLogo`, `styleTagline`, `styleCmd` 등): `version.go`
- **그 외 모든 TUI 스타일**: `tui.go` 상단 `var (...)` 블록

공유가 필요하면 `version.go` 가 export 한 변수를 tui.go 가 참조하는 방향. (현재 `styleAccent`, `styleVersion` 이 이 패턴.)

### 9. 외부 명령은 `exec.Command("...", "-C", dir, ...)` 패턴

```go
out, err := exec.Command("git", "-C", target, "checkout", branch).CombinedOutput()
```

`os.Chdir` 으로 작업 디렉토리를 옮긴 뒤 명령을 호출하지 않는다 (단, 마지막의 `claude --resume` 직전 cd 는 의도된 예외 — 그 시점부터 claude 가 해당 cwd 에서 동작해야 하므로).

### 10. 서브커맨드는 `flag.Parse` 전에 `os.Args[1]` 분기

```go
if len(os.Args) > 1 {
    switch os.Args[1] {
    case "version", "--version", "-v":
        printSplash(os.Stdout); return
    }
}
flag.Parse()
```

cobra / urfave-cli 같은 프레임워크 도입 금지 — 규모상 과함. 서브커맨드가 5개를 넘어가면 그때 재검토.

**신규 플래그 추가 시 두 군데 동시 갱신**:
1. `flag.String / Bool(...)` 호출 (`main()` 안)
2. 파일 상단 `const usage` 의 안내 문구

빠뜨리면 `csm -h` 가 새 플래그를 안 알려주는 silent 문서 부채.

### 11. `Version` 은 `var`, 절대 수동 변경 금지

```go
var Version = "dev"   // ldflags 로 build 시 주입
```

`const` 로 바꾸지 않는다 (ldflags 가 못 박는다). 수동으로 값 수정하지 않는다 — git 태그가 단일 진실.

## 마이너 규칙 (Suggestion)

### 12. 주석 최소화

이미 정착된 컨벤션 — 코드 자체가 자명하면 주석을 달지 않는다. *왜* 그렇게 했는지가 비자명할 때만 짧게.

### 13. 새 의존성은 강한 이유 없이 추가 금지

현재 의존성: `bubbletea`, `bubbles`, `lipgloss`, `mattn/go-runewidth`, `sahilm/fuzzy`. 새 패키지 추가는 표준 라이브러리로 못 푸는 명확한 이유가 있을 때만.

### 14. 테스트는 아직 없음 — 추가 시 표준 `testing` 으로

테이블 기반 Go 테스트. testify 같은 무거운 프레임워크 도입 안 함.

## 기능 영역 경계 — Phase 분할 존중

코드에 *없는 것* 이 의도된 건지 미완성인지 헷갈리기 쉬워서 명시:

| 기능 | 상태 |
|---|---|
| 사후 rename / 라벨 편집 | Phase 2.x (pins.json 의 Label 필드는 있음 — UI 미구현) |
| 멀티플렉서 popup 통합 (tmux/cmux/zellij) | Phase 2.x (standalone UX 검증 후) |
| 원격 백업 sync | Phase 3 |
| AI 요약 export 모드 (`--summarize`) | Phase 3 |
| 자동 테스트 스위트 | 미정 |

위 항목을 즉흥적으로 추가하지 않는다. 추가 결정은 사용자와 합의 후.

**Phase 2A 완료 항목** (v0.3.0):
- 단일 세션 export (CLI + TUI `e`)
- 일괄 download (dir / zip / single-file + 필터)
- 세션 제거 (휴지통 + 영구 삭제 2단계, TUI `d`/`t`/`r`)
- Pin (사이드카, TUI `p`, ★ Pinned 섹션 + inline 별표)
- 세션 합치기 (CLI `csm merge` + TUI `space` 마킹 → `m`) — N개 세션을 claude 로 정리·통합해 최신 세션에 시딩, 나머지는 휴지통

## 외부 의존 데이터 — 스키마 변경 시 영향

Claude Code 의 JSONL 포맷이 우리가 의존하는 필드:

```
~/.claude/projects/<encoded-cwd>/<uuid>.jsonl
```

라인별 `rawLine` 의 필드:
- `type` ("user" / "assistant" / 메타)
- `cwd`
- `gitBranch`
- `timestamp` (RFC3339)
- `message.content` (string 또는 `[{type, text}]`)

이 스키마가 깨지면 `session.go` 의 `rawLine` / `rawMessage` / `contentBlock` 구조체 갱신 필요. 첫 사용자 메시지 추출 로직 (`cleanText`) 도 새 메시지 메타에 영향받을 수 있음.

## 릴리스 절차

1. main 브랜치가 안정 상태인지 확인 (`go build` 통과)
2. `git tag -a vX.Y.Z -m "<changelog 한 줄>"`
3. `git push origin vX.Y.Z`
4. GitHub Actions 가 자동으로:
   - 4개 플랫폼 바이너리 빌드 (darwin/linux × amd64/arm64)
   - GitHub Release 생성 + 아카이브 업로드
   - `ninanung/homebrew-tap` 에 Formula 갱신 커밋
5. 잠시 후 사용자: `brew upgrade csm`

전제: `HOMEBREW_TAP_GITHUB_TOKEN` 시크릿이 csm 레포에 등록돼 있어야 함. PAT 만료 시 재발급 + 시크릿 갱신.

## 절대 하지 말 것

위 Critical 규칙 (1~5) 위반이 최우선 금지. 그 외 추가로:

- 새 의존성을 합의 없이 추가 (규칙 13)
- Phase 2/3 기능을 합의 없이 미리 구현 (Phase 경계 절)
