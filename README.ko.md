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
| `Enter` | 선택 |
| `/` | 필터 모드 진입 |
| `Esc` | 필터 모드 종료 (필터 중 아니면 csm 자체 종료) |
| `Ctrl-D` / `Ctrl-U` | 반페이지 아래/위 |
| `Ctrl-F` / `Ctrl-B` (또는 `PgDn` / `PgUp`) | 풀페이지 아래/위 |
| `g` / `G` (또는 `Home` / `End`) | 첫 / 마지막 세션 |
| `q` | 선택하지 않고 종료 |

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

각 라인은 `cwd`·`gitBranch`·`timestamp` 등 메타를 포함한 메시지. csm 은 이 파일들을 스캔해서 세션 요약을 추출하고, [bubbletea](https://github.com/charmbracelet/bubbletea) TUI 로 렌더링.

## 상태

이건 Phase 1 릴리스로, 핵심 picker + 자동 `cd` + 안전한 브랜치 정렬 + 한·영 i18n 까지 커버. 다음 항목은 의도적으로 빠져 있음:

- 사후 rename 과 태깅 (Phase 2)
- 세션 아카이브 / 삭제 (Phase 2)
- 멀티플렉서 인지 popup 통합 (Phase 2, standalone UX 검증 후)
- 원격 백업 sync (Phase 3)

## 라이선스

미정.
