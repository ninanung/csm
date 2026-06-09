<pre>
 ██████╗ ███████╗███╗   ███╗
██╔════╝ ██╔════╝████╗ ████║
██║      ███████╗██╔████╔██║
██║      ╚════██║██║╚██╔╝██║
╚██████╗ ███████║██║ ╚═╝ ██║
 ╚═════╝ ╚══════╝╚═╝     ╚═╝
</pre>

# csm

[English](README.md) | **한국어**

[Claude Code](https://docs.claude.com/en/docs/claude-code) 세션을 둘러보고 재개하는 작은 CLI. 기본 `claude --resume` picker 가 식별 정보가 부족한 평평한 리스트만 보여주고, 프로젝트 간 이동마다 일일이 `cd` 해야 했던 불편을 풀기 위해 만들었다.

## 무엇을 하나

- `~/.claude/projects/` 아래 모든 세션을 프로젝트(cwd basename)별로 그룹핑해서 보여준다.
- 각 세션의 첫 사용자 메시지·git 브랜치·마지막 활동 시각·메시지 수를 함께 표시. 한눈에 "이 세션이 뭐였는지" 식별 가능.
- `/` fuzzy 검색 (프로젝트명 + 첫 메시지 대상).
- 중요한 세션은 `p` 로 고정 — 최상단 `★ Pinned` 섹션 + 원래 그룹에도 ★ 마커로 표시.
- 5개로 부족하면 `→` 또는 `▾ N개 더` 토글에서 `Enter` 로 그 프로젝트만 전체 보기로 drill-down. `←` 또는 `Esc` 로 복귀.
- **SDK / orchestration 세션 (worktree 에이전트, `entrypoint=sdk-cli`) 은 기본 숨김** — 본인이 직접 띄운 세션만 보이게. `a` 키로 토글, 숨겨진 개수는 헤더에 노출.
- **반복 워크플로우 그루핑** — 같은 프로젝트에서 첫 메시지가 동일한 세션(예: `spec-to-plan` 반복 실행)은 가장 최근 한 개로 묶고 `+N 유사` 뱃지. drill-down 하면 모두 개별로 보임.
- **Sub-agent 인식** — `Task` 로 sub-agent 를 띄운 세션은 `↳N agents (s)` 뱃지 표시. `s` 키로 sub-agent 목록 진입 (agentType / description / 첫 메시지). Enter 시 jsonl 을 OS 기본 뷰어로 오픈.
- 세션을 원본 JSONL 그대로 export (`e`) — Claude Code 가 쓴 바이트를 그대로 복사. sub-agent · tool-result 사이드카가 있으면 폴더 형태로 묶어 export — `cp -r` 로 `~/.claude/projects/` 에 그대로 round-trip 가능. `csm download` 로 전체 세션을 디렉토리 트리(`_index.md` TOC 포함) 또는 zip 으로 — 백업·재임포트 용도.
- 더 이상 안 쓰는 세션은 `d` 로 복구 가능한 휴지통으로. sub-agent 사이드카도 함께 이동돼 orphan 이 남지 않음. `t` 가 휴지통 뷰 토글, 안에서 `r` 복구, 한 번 더 `d` 로 영구 삭제.
- **오래된 세션 일괄 정리** — `csm prune <days>` 로 N일 이전 세션을 한 번에 휴지통으로. 핀 세션은 보호. 미리보기 + 확인을 거치며 `-y` / `--force` 로 스킵 가능.
- 세션 선택 시:
  - 그 세션의 원래 cwd 로 자동 `cd`,
  - git 브랜치를 안전하게 정렬 (working tree 깨끗 + 브랜치 존재 시에만 checkout; 그 외엔 워닝),
  - `claude --resume <id>` 로 exec. 파일 경로·git 명령·도구 호출이 세션이 멈춘 그 자리와 모두 맞아떨어진다.

## 설치

### Homebrew (macOS / Linux)

```bash
brew install ninanung/tap/csm
```

### Go

Go 1.21+ 필요.

```bash
go install github.com/ninanung/csm@latest
```

`$GOBIN` (또는 `$GOPATH/bin`, 보통 `~/go/bin`) 이 `PATH` 에 있는지 확인:

```bash
export PATH="$HOME/go/bin:$PATH"
```

### 소스에서 빌드

```bash
git clone https://github.com/ninanung/csm ~/Documents/dev/my/csm
cd ~/Documents/dev/my/csm
go install .
```

## 사용법

### 기본 (standalone)

어느 셸에서든 실행. 세션 고르고 `Enter` — csm 이 올바른 디렉토리에서 `claude --resume` 을 exec.

```bash
csm
```

### Print 모드 (어댑터용)

`<session-id>\t<cwd>` 를 stdout 에 출력하고 종료, claude 는 실행하지 않음. 외부 스크립트가 선택 결과를 받아 쓸 때 유용.

```bash
csm --print
```

### 언어

인터페이스(헤더·푸터 힌트·브랜치 프롬프트·에러 메시지)는 **영어**와 **한국어** 둘 다 지원.

기본 동작: `CSM_LANG` → `LC_ALL` → `LC_MESSAGES` → `LANG` 순으로 자동 감지. 'ko*' 로 시작하면 한국어, 그 외엔 영어.

명시적 지정:

```bash
csm --lang ko
csm --lang en
```

영구 설정:

```bash
export CSM_LANG=ko
```

### 키 바인딩

| 키 | 동작 |
| --- | --- |
| `↑` / `↓` / `j` / `k` | 이동 |
| `→` / `←` / `l` / `h` | drill in / out |
| `Enter` | 세션 선택 (또는 `▾ N개 더` 에서 drill-in; sub-agent 행에서는 jsonl 을 OS 뷰어로 오픈) |
| `/` | 필터 모드 진입 |
| `e` | 커서 세션 export — 원본 JSONL 그대로 (이후 `o` 열기 / `c` 경로 복사) |
| `p` | 고정 toggle |
| `s` | sub-agent 뷰 진입 (`↳N agents` 뱃지가 있는 행에서) |
| `a` | SDK / orchestration 세션 표시 / 숨김 toggle (기본 숨김) |
| `d` | 휴지통으로 이동 (복구 가능; 휴지통 뷰에서는 두 번 눌러야 영구 삭제) |
| `t` | 휴지통 뷰 toggle |
| `r` / `u` | 휴지통에서 복구 |
| `?` | 전체 키 안내 오버레이 (아무 키나 눌러 닫기) |
| `Ctrl-D` / `Ctrl-U` | 반페이지 아래/위 |
| `g` / `G` (또는 `Home` / `End`) | 첫 / 마지막 세션 |
| `Esc` | 한 단계씩 되돌리기 (status → sub-agent → drill → 휴지통 → 종료) |
| `q` | 선택하지 않고 종료 |

마우스 휠로도 스크롤됨.

## 브랜치 정렬 — 안전 규칙

세션 선택 시 csm 은 다음을 **모두** 만족할 때만 git 브랜치를 자동 전환한다:

- working tree 가 깨끗하고,
- 기록된 브랜치가 로컬에 존재하고,
- rebase / merge / cherry-pick 진행 중이 아니고,
- 그 브랜치가 다른 worktree 에 점유돼 있지 않음.

그 외 경우는 한 줄 워닝만 출력하고 브랜치 전환 없이 진행. 로컬 상태를 망가뜨리지 않고 claude 세션을 띄움.

### 브랜치가 로컬에 없을 때 — 인터랙티브 프롬프트

세션의 기록된 브랜치가 로컬에 없으면 작은 picker 가 등장:

- **현재 브랜치 유지** — 그대로 진행
- **기존 로컬 브랜치에서 선택** — 두 번째 picker 에서 브랜치 선택 (committerdate 역순)
- **중단** — claude 시작하지 않음

`↑/↓ + Enter` 로 선택. `Esc` 면 중단 처리.

## 동작 원리

Claude Code 는 각 세션을 다음 위치에 JSON-Lines 파일로 저장한다:

```
~/.claude/projects/<encoded-cwd>/<session-uuid>.jsonl
```

세션이 `Task` 도구로 sub-agent 를 띄우면 같은 위치에 사이드카 디렉토리가 생성됨:

```
~/.claude/projects/<encoded-cwd>/
├── <session-uuid>.jsonl         ← 메인 세션 (user + assistant 라인)
└── <session-uuid>/              ← 사이드카 컨테이너 (해당 시)
    ├── subagents/agent-*.jsonl  ← 각 Task spawn 의 대화
    │  + agent-*.meta.json       ← {agentType, description, toolUseId}
    └── tool-results/            ← 캡처된 도구 출력
```

메인 jsonl 의 각 라인은 `cwd`·`gitBranch`·`timestamp`·`entrypoint` 등 메타를 포함. csm 은 메인을 스캔하고 사이드카 디렉토리를 walk 해서 sub-agent 갯수와 최신 활동 시각을 계산. [bubbletea](https://github.com/charmbracelet/bubbletea) TUI 로 렌더링.

### Export 와 download

원본 JSONL 을 그대로 복사. Claude Code 가 쓴 바이트 그대로 — 백업·재임포트 용도. 세션에 `<uuid>/` 사이드카 (sub-agent · tool-result) 가 있으면 export 결과는 디스크 구조를 그대로 보존한 폴더 — round-trip 가능.

```bash
csm export <session-id>             # → ~/Downloads/<auto>.jsonl
                                    #   (사이드카 있을 땐 ~/Downloads/<auto>/)
csm export <session-id> -o out.jsonl
csm export <session-id> -o -        # stdout (jq 등 파이프)

csm download                        # → ~/Downloads/csm-<date>/<project>/...
csm download --zip                  # → ~/Downloads/csm-<date>.zip
csm download --since 2026-06-01 --project csm --min-msgs 5
```

Picker 안에서 `e` 누르면 기본 위치에 export 후 footer 에 경로 표시 (`c` 로 경로 복사).

### 오래된 세션 일괄 정리

`csm prune <days>` — 마지막 활동이 N일 이전인 세션을 휴지통으로 일괄 이동. 핀(★) 세션은 기본 보호.

```bash
csm prune 30                        # 미리보기 + 확인
csm prune 30 --dry-run              # 미리보기만, 변경 없음
csm prune 30 -y                     # 확인 스킵 (cron / 스크립트)
csm prune 30 --permanent            # 휴지통 X, 영구 삭제
csm prune 30 --include-pinned       # 핀 세션까지 포함
csm prune 30 --project NAME         # 특정 프로젝트만
```

플래그 순서 자유 — `csm prune 30 --dry-run` 과 `csm prune --dry-run 30` 둘 다 동작.

### 정리 / housekeeping

`csm cleanup` — 이전 csm 버전이 메인 jsonl 만 휴지통으로 옮기면서 `~/.claude/projects/` 에 남긴 orphan sub-agent 디렉토리를 휴지통으로 통합. 현재 휴지통 흐름은 자동 처리하므로 이 커맨드는 이미 leak 된 케이스용 안전·idempotent one-shot.

## 상태

현재 릴리스: **v0.3.2**. Picker, 자동 `cd`, 안전한 브랜치 정렬, 친절한 empty state, 셸 자동완성, drill-down, export / download (sub-agent bundling 포함), 휴지통 (sub-agent dir 동반 처리), 핀, SDK 에이전트 필터, 첫 메시지 그루핑, sub-agent drill-down, 일괄 prune.

다음은 의도적으로 미구현:

- 사후 rename / 라벨 편집 UI (Phase 2.x)
- 멀티플렉서 인지 popup 통합 (Phase 2.x, standalone UX 검증 후)
- 원격 백업 sync (Phase 3)
- AI 요약 export 모드 (Phase 3)

## 라이선스

MIT — [LICENSE](LICENSE) 참고.
