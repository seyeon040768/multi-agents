# 단일 Agent 채팅 MVP 검증

2026-10-06, 로컬 Mattermost 11.7.0 및 별도 LangGraph runtime에서 검증했다.

## 실제 검증

- 기존 `test-agent`의 `google / gemini-3-flash-preview` 설정과 Bot 연결을 사용했다.
- 루트 `.env`의 GOOGLE_API_KEY를 실행 서비스 환경에 설정했다. 키는 Agent KV·플러그인 설정·로그에 넣지 않았다.
- 실제 LangGraph 그래프에서 Gemini를 호출해 HTTP 200 및 `안녕하세요` 응답을 확인했다.
- Docker 실행 서비스를 `docker_default` 네트워크에 띄우고 Mattermost Plugin의 Runtime URL/Token을 설정했다.
- 새 플러그인 패키지를 설치하고 활성화했다.
- `seyeon`이 Mattermost DM으로 보낸 `연결 테스트입니다.`에 실제 `agent-test-agent` Bot이 `안녕하세요`라고 답했다.
- 질문 ID는 `pgku3zz1jtbobd7wbenrcig3yw`, 답변 ID는 `metmy7thofn4jexmckt7imjacw`이다.
- 답변 root_id가 질문 ID와 일치하고, 재확인 시 해당 Thread의 Bot 답변은 한 개였다.
- Plugin disable/enable로 재활성화한 뒤 Agent ACTIVE 상태와 기존 Bot user_id/username이 유지됨을 확인했다.

실제 Thread의 여러 차례 후속 대화·채널 mention·다른 Provider의 유효한 인증과 응답은 추가 운영 확인 대상이다.
동일 Thread의 동시 질문 순서 보장 및 영속 중복 이벤트 처리는 이번 단계에 포함하지 않는다.

## 자동 검증

- `GOCACHE=/tmp/agentbridge-go-cache go test -race ./server/...`: 통과.
- Python `pytest`: 13개 통과. 외부 LLM 없이 LangGraph 실행·네 Provider 라우팅·인증·timeout/cancel·에러 redaction·Tool 호출 거부를 검증했다.
- 네 종류의 실제 SDK 어댑터 생성 및 파라미터 설정: 통과. 자동 테스트에서는 외부 호출하지 않았다.
- Resolver: DM·정확한 mention·일반 메시지 무시·Bot/System/plugin 메시지 무시·미참여 private 채널 거부·복수 Agent mention 무시.
- Prompt/Context: prompt section 구성·역할 변환·최근 20개·현재 메시지 중복 제외·다른 채널/미래/삭제 메시지 제외·mention 정규화.
- Orchestrator: Agent 활성 상태 검사·실제 Thread reply 구조·사용자용 오류·재처리 방지·취소.
- Hook: 대상 요청만 큐에 추가·큐 overflow 안내·worker 종료.
- `npm run check-types`: 통과.
- `npm run build`, `make dist`: 통과. 5개 플랫폼 바이너리와 webapp을 패키징했다.
- Docker runtime image build 및 `/healthz`: 통과.
- `git diff --check`: 통과.

배포·설정·지원 범위는 [runtime README](../agent-runtime/README.md)와 [Agent 명세](agent.md)를 참고한다.
