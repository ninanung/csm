---
name: csm
description: |
  Claude Code 세션 picker — 같은 페인에서 다른 세션으로 이동한다.
  "csm", "세션 바꿔", "세션 이동", "다른 세션 열어", "switch session" 등 요청 시 자동 트리거.
  현재 claude 를 종료시키고 같은 페인에서 csm TUI(키보드 picker)를 띄운다.
  사용자가 키보드로 선택하면 csm 이 cwd 자동 정렬 후 claude --resume.
autoTrigger: true
---

# csm — Session switcher

사용자의 세션 전환 요청 시 아래 절차를 그대로 따른다.

## 절차

이 skill 의 목적은 **claude 안에서 csm TUI 가 자동으로 등장하도록 트리거**하는 것이다. claude 가 picker 를 직접 그리지 않는다. csm 의 자체 키보드 TUI 가 그 역할을 한다.

### 1단계: 환경 감지

Bash 도구로:

```bash
if [ -n "${CMUX_SURFACE_ID:-}" ]; then echo cmux
elif [ -n "${TMUX:-}" ]; then echo tmux
else echo plain
fi
```

### 2단계: csm 가용성 확인

```bash
command -v csm >/dev/null && echo ok || echo missing
```

missing 이면 사용자에게 안내:
```
csm 명령을 찾을 수 없습니다.
설치: cd ~/Documents/dev/my/csm && go install . (그리고 $HOME/go/bin 을 PATH에)
```

### 3단계: 환경별 swap 시퀀스

#### cmux 환경

```bash
cmux send "/exit" && cmux send-key Enter
( nohup bash -c 'sleep 1.5 && cmux send "csm" && cmux send-key Enter' >/dev/null 2>&1 & )
```

#### tmux 환경

```bash
tmux send-keys "/exit" Enter
( nohup bash -c 'sleep 1.5 && tmux send-keys "csm" Enter' >/dev/null 2>&1 & )
```

> **detached 가 필수**. /exit 와 csm 두 키를 한 번에 보내면 claude 가 둘 다 슬럽해서 csm 이 shell 에 닿지 않는다. 두 번째 send 를 백그라운드 nohup 으로 분리해서 claude 가 죽은 *뒤에* shell 에 도달하게 한다.

#### plain 환경

자동 send 불가. 사용자에게 안내만:
```
멀티플렉서가 없어서 자동 전환은 어렵습니다. 직접 실행하세요:

  /exit
  csm
```

### 4단계: 짧은 응답

bash 명령 실행 후 한두 줄로만:
```
csm 띄울게요. 곧 키보드로 세션 선택하세요.
```

리스트 출력·세션 식별·선택 처리 등은 **모두 csm TUI 가 알아서 함**. 이 skill 은 trigger 만 담당.

## 동작 원리

1. Bash 명령이 cmux/tmux send 로 현재 페인에 `/exit` 키 시퀀스를 주입한다.
2. claude 가 현재 턴(이 skill 실행) 마치고 다음 입력으로 `/exit` 받음 → 종료.
3. 셸 프롬프트 등장.
4. 두 번째 주입된 `csm` 키가 셸에 들어감 → csm 바이너리 실행.
5. csm 이 bubbletea TUI 등장 (프로젝트 그룹, 검색, 메타 표시).
6. 사용자 키보드 선택 (↑↓ Enter `/` filter 등).
7. csm 이 자동으로 `cd <cwd>` + `exec claude --resume <id>` → 새 세션 시작.

## 트러블슈팅

- **csm TUI 가 안 뜸**: PATH 확인 (`which csm`). 또는 sleep 시간 늘려보기.
- **swap 시퀀스가 키 buffer 에서 꼬임**: claude 가 응답 중에 /exit 받는 경우 발생 가능. 사용자가 한 번 더 trigger 하면 복구.
- **cmux/tmux 가 아닌 환경**: 자동 전환 불가, plain 안내 메시지 출력.
