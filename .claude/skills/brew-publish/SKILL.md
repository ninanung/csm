---
name: brew-publish
description: |
  csm 의 새 버전을 릴리스한다 — semver 결정·changelog 생성·태그 푸시·GitHub Actions 워치·Homebrew Formula 갱신 검증을 한 흐름으로.
  "릴리스", "publish", "brew publish", "새 버전", "tag release", "v0.x 배포" 등 트리거.
  csm 레포 내부에서만 동작 (homebrew-tap·GoReleaser 셋업이 이 프로젝트 고유).
---

# brew-publish — csm 릴리스 절차

사용자가 새 버전 릴리스를 요청하면 아래 순서를 그대로 따른다. 각 단계의 실패는 즉시 보고하고 진행 멈춘다.

## 1단계: 사전 검증

이 순서대로 빠르게 점검:

```bash
cd ~/Documents/dev/my/csm

# 1-a. 클린 워킹 트리
git status --porcelain
# 출력 있으면: "uncommitted 변경이 있습니다 — 먼저 commit/stash 해주세요" 안내 후 중단

# 1-b. main 브랜치
git branch --show-current
# main 아니면: "main 에서만 릴리스합니다 (현재: <branch>)" 안내 후 중단

# 1-c. 빌드 통과
go build ./...
# 실패하면: 출력 보여주고 중단

# 1-d. 원격 동기화
git fetch origin
git status -sb
# 'ahead' 면 push 권유, 'behind' 면 pull 권유 후 중단
```

## 2단계: 직전 태그 + 변경 분석

```bash
LAST=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
echo "Last tag: $LAST"
git log "$LAST"..HEAD --oneline
```

직전 태그와 HEAD 사이의 커밋들을 수집. 0개면 "새 커밋 없음" 안내 후 중단.

## 3단계: semver 버전 제안

커밋 메시지를 분석해서 다음 버전 후보를 결정:

| 커밋 패턴 | 권장 bump |
|---|---|
| `BREAKING`·`!:` 표기·인터페이스 제거 | major |
| 새 기능·새 서브커맨드·새 플래그·새 키 바인딩 | minor |
| 버그픽스·문서·내부 리팩토링·UI 미세 조정 | patch |

여러 카테고리 섞이면 가장 큰 쪽으로 (e.g., minor + patch → minor).

`$LAST` 를 `vMAJOR.MINOR.PATCH` 로 파싱해서 다음 버전 계산 후 사용자에게 한 줄 확인:

> "직전 v0.2.0 이후 N개 커밋, [신기능 X / 픽스 Y / 리팩 Z]. **v0.3.0** 으로 진행할까요?"

사용자가 다른 버전 원하면 그걸 사용.

## 4단계: changelog 작성

릴리스 노트로 쓸 짧은 글:
- 한 줄 헤드라인 (제목)
- 1~5개 핵심 변경 bullet
- 영문 우선 (GitHub release 와 brew tap 가독성)

각 bullet 은 "사용자에게 의미있는 변화" 기준. 내부 리팩토링·문서 갱신은 묶거나 생략.

작성한 글을 사용자에게 보여주고 OK 받기.

## 5단계: 태그 + 푸시

승인 후:

```bash
git tag -a vX.Y.Z -m "$(cat <<'EOF'
vX.Y.Z — <한 줄 헤드라인>

Highlights:
- ...
- ...
EOF
)"
git push origin vX.Y.Z
```

## 6단계: GitHub Actions 워치

```bash
sleep 5
RUN_ID=$(gh run list -R ninanung/csm --limit 1 --json databaseId -q '.[0].databaseId')
gh run watch "$RUN_ID" -R ninanung/csm --exit-status
```

실패하면 워크플로우 URL + 마지막 로그 보여주고 중단. 트러블슈팅 후 재태그 가능.

## 7단계: 결과 검증

```bash
# GitHub Release 확인
gh release view vX.Y.Z -R ninanung/csm

# Formula 갱신 확인 (version 라인)
gh api repos/ninanung/homebrew-tap/contents/Formula/csm.rb --jq '.content' | base64 -d | grep -E '^\s*version'
```

`version "X.Y.Z"` 가 보이면 OK.

## 8단계: 사용자 안내

릴리스 정리:

```
🎉 vX.Y.Z published

- GitHub Release: https://github.com/ninanung/csm/releases/tag/vX.Y.Z
- 바이너리: darwin/linux × amd64/arm64
- Formula: ninanung/homebrew-tap 자동 갱신됨

사용자 업그레이드:
  brew upgrade csm
또는 새 설치:
  brew install ninanung/tap/csm
```

## 실패·중단 처리

- **태그 푸시 후 Actions 실패**: 태그 자체는 남음. 코드 픽스 → 재푸시는 안 됨 (동일 태그 push 금지). 다음 patch 버전으로 재진행.
- **태그 잘못 만듦** (push 전): `git tag -d vX.Y.Z` 로 로컬 삭제 후 재시작.
- **태그 push 후 취소 필요**: `git push origin --delete vX.Y.Z` + GitHub Release 수동 삭제. 단 brew Formula 가 이미 갱신됐으면 homebrew-tap 에서 별도 revert PR 필요.

## 짚어둘 점

- `HOMEBREW_TAP_GITHUB_TOKEN` PAT 가 csm 레포 시크릿에 있어야 함. 만료 시 갱신 안내.
- 사용자에게 "릴리스 완료" 메시지 전송은 **검증 단계 모두 통과 후에만** 한다. Actions in_progress 인 상태에서 '완료' 라고 말하지 않는다 (self-verification: hedge 회피).
- 외부 액션 (태그 푸시) 직전엔 changelog + 버전을 사용자에게 명시 확인 받기. 자동 진행 X.
