---
name: csm
description: |
  Claude Code 세션 picker — 같은 페인에서 다른 세션으로 이동한다.
  "csm", "세션 바꿔", "세션 이동", "다른 세션 열어", "switch session" 등 요청 시 자동 트리거.
  cwd 와 git branch 자동 정렬. tmux/cmux 환경에서 send-keys 로 swap 시퀀스 주입.
autoTrigger: true
---

# csm — Session switcher

사용자의 세션 전환 요청 시 아래 절차를 그대로 따른다.

## 1단계: 세션 목록 조회

Bash 도구로 실행:

```bash
csm --list-json
```

JSON 배열을 받는다. 각 항목 필드:
- `id` — 세션 UUID
- `cwd` — 세션의 작업 디렉토리 (절대경로)
- `project` — cwd basename
- `branch` — 세션 시작 시 git 브랜치
- `first_message` — 첫 사용자 메시지 (한 줄)
- `last_activity` — 마지막 활동 시각 (RFC3339)
- `message_count` — 메시지 수

명령이 없거나 에러면 사용자에게 안내: "csm 바이너리가 PATH에 없습니다. `go install github.com/.../csm@latest` 또는 직접 설치해주세요."

## 2단계: 사용자에게 표시

현재 cwd 확인:

```bash
pwd
```

표시 규칙:
- 프로젝트별로 그룹핑 (project 필드 기준)
- 현재 cwd 의 project 그룹을 **맨 위**에 배치
- 그룹 내에선 `last_activity` 역순 (최근 위)
- 각 항목: `번호. <first_message 50자> | branch | <상대시간>`

표시 예시:
```
**web-idus-com** (현재 프로젝트)
1. ICW-13985 작가 앱 안내사항 수정... | feat/ICW-13985 | 2시간 전
2. self-verification refactor...        | main             | 1일 전

**claude-config**
3. csm 도구 설계 논의...                | main             | 30분 전
4. setup.sh TEAM_DOCS 마커 코멘트...    | main             | 1일 전
```

## 3단계: 사용자 선택 받기

번호로 받거나 키워드로 받는다. 다음 케이스 처리:
- 명확한 번호 (예: "2") → 그 번호 선택
- 키워드 (예: "figma") → first_message 부분일치로 후보 좁혀 다시 확인
- 모호하면 다시 물음

선택된 세션의 `id` 와 `cwd` 를 기억한다.

## 4단계: 환경 감지 + swap 시퀀스 실행

먼저 환경 감지:

```bash
if [ -n "${CMUX_SURFACE_ID:-}" ]; then echo cmux
elif [ -n "${TMUX:-}" ]; then echo tmux
else echo plain
fi
```

선택된 세션의 cwd 가 실재하는지 확인:

```bash
test -d "<선택한 cwd>" && echo ok || echo missing
```

### cwd 가 있을 때

#### cmux 환경
```bash
cmux send "/exit" && cmux send-key Enter && sleep 0.5 && \
cmux send "cd '<선택한 cwd>' && claude --resume <선택한 id>" && cmux send-key Enter
```

#### tmux 환경
```bash
tmux send-keys "/exit" Enter \; \
  set-buffer "" \; \
  send-keys "cd '<선택한 cwd>' && claude --resume <선택한 id>" Enter
```

> sleep 0.3 가 필요할 수 있다 — claude 가 /exit 받고 종료되는 데 약간의 지연이 있음.

#### plain 환경 (멀티플렉서 없음)
사용자에게 안내만 출력:
```
멀티플렉서가 감지되지 않습니다. 직접 실행하세요:

/exit
cd <선택한 cwd> && claude --resume <선택한 id>
```

### cwd 가 없을 때

워닝 표시 후 다음 선택지 제공:
1. 현재 cwd 에서 시작 (`cd` 생략)
2. 취소

사용자 답에 따라 진행.

## 5단계: 종료 메시지

swap 시퀀스 실행 후 짧게:
```
세션 이동: <project> / <first_message 일부>
```

자세한 설명은 불필요. 사용자는 곧 새 claude 세션으로 진입함.

## 트러블슈팅

- **`csm: command not found`** → PATH 확인 안내
- **JSON 파싱 실패** → 사용자에게 raw 출력 보여주고 csm 업데이트 권유
- **빈 세션 목록** → "아직 세션이 없습니다" 안내
