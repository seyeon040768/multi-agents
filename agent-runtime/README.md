# Agent Bridge LangGraph Runtime

Mattermost Plugin은 Agent KV 조회·메시지 라우팅·System Prompt·현재 메시지·Bot 게시를 담당한다.
이 서비스는 LangGraph `START → generate → END`에서 Agent가 선택한 모델을 호출한다.
OpenAI·Gemini·Claude·Ollama 중 하나로 제한하거나 자동 전환하지 않는다.
정책 기반 Tool Calling을 지원한다. 승인/interrupt, delegation, 장기 Memory, streaming 및 자동 fallback은 후속 범위다.

| Agent provider | Adapter | 실행 환경 설정 |
|---|---|---|
| openai | ChatOpenAI | OPENAI_API_KEY |
| google | ChatGoogleGenerativeAI | GOOGLE_API_KEY 또는 GEMINI_API_KEY |
| anthropic | ChatAnthropic | ANTHROPIC_API_KEY |
| ollama | ChatOllama | OLLAMA_BASE_URL |

Agent에는 provider/name/parameters만 저장한다. API Key와 모델 서버 주소는 요청이나 Agent KV로 받지 않는다.
로컬 모델은 이번 단계에서 Ollama를 통해 연결한다. 설치된 모델 이름과 Agent의 model.name이 일치해야 한다.
관리 UI의 Google 모델 목록에는 Gemini 3 Flash (Preview)와 Gemini 3.1 Flash-Lite를 제공한다.
Claude에 top_p가 지정되면 temperature 대신 top_p를 전달한다.
기존 OpenAI gpt-5.4에는 sampling 호환성을 위해 Responses API + reasoning effort none을 사용한다.
Gemini는 Vertex AI가 아닌 Gemini Developer API를 사용한다.

## 실행

Python 3.12 이상 또는 Docker가 필요하다. `.env.example`을 `.env`로 복사하고 사용할 Provider의 키와
임의의 긴 `AGENT_RUNTIME_TOKEN`을 설정한다. 이미 `.env`가 있다면 덮어쓰지 않는다.
`.env`는 Git 및 Docker build context에서 제외된다.

```bash
cd agent-runtime
cp -n .env.example .env
# .env를 편집한 후:
docker compose up -d --build
```

이 저장소의 Mattermost 컨테이너가 사용하는 `docker_default` 네트워크에 실행 서비스를 연결한다.
다른 배포에서는 `.env`의 `MATTERMOST_DOCKER_NETWORK`를 실제 Mattermost 네트워크로 바꾼다.
Mattermost 컨테이너에서 런타임 URL은 `http://agent-runtime:8000`이다.
호스트에서 실행하는 Mattermost는 `http://127.0.0.1:8000`으로 접속할 수 있다.
컨테이너 내부의 localhost는 호스트나 다른 컨테이너가 아니다.
원격 배포는 HTTPS 또는 신뢰하는 사설 네트워크에서 실행한다.

Docker 없이 실행하려면 환경 변수를 별도로 설정한 뒤:

```bash
python3 -m venv .venv
.venv/bin/pip install -r requirements.lock
.venv/bin/uvicorn agent_runtime.app:app --host 127.0.0.1 --port 8000
```

서비스는 인증 token이 없으면 시작을 거부한다. `/healthz`는 연결 확인용이고
`POST /v1/generate`는 `Authorization: Bearer <AGENT_RUNTIME_TOKEN>`이 필요하다.
Provider 인증이 빠진 요청은 일반 502 오류로 끝나며 SDK 오류 본문을 노출하지 않는다.
플러그인 90초, 서비스 80초 timeout을 사용한다. 클라이언트 연결 종료 시 모델 task를 취소한다.

## Mattermost 연결

1. `agent-bridge`에서 `make dist`로 생성한 플러그인 패키지를 설치한다.
2. System Console의 Multi Agent Bridge Plugin 설정에 `LangGraph Runtime URL`과
   `LangGraph Runtime Token`을 입력한다. Token은 `.env`의 AGENT_RUNTIME_TOKEN과 같아야 한다.
3. `/agent list`에서 Agent가 ACTIVE이고 사용할 모델이 설정돼 있는지 확인한다.
4. Bot과 1:1 DM을 열어 질문하거나, Bot을 채널에 초대한 뒤 `@agent-<id> 질문`을 게시한다.
5. Bot이 같은 Thread에 답변하는지 확인한다. DM의 Thread 후속 질문은 mention 없이 가능하다.
   일반 채널에서는 후속 질문에도 mention이 필요하다.

현재 기존 `test-agent`의 google/gemini-3-flash-preview 설정은 바꾸지 않는다.
GOOGLE_API_KEY를 설정하면 같은 provider/model로 호출한다.
API Key는 Agent 설정 화면이나 Prompt에 입력하지 않는다.
대기 요청은 Plugin 재시작 시 유실될 수 있으며 재전송해야 한다. 설정과 Bot 연결은 KV에 유지된다.

## 테스트

```bash
.venv/bin/pip install -r requirements-test.lock
.venv/bin/python -m pytest -q
```

실제 LangGraph 실행에 mock 모델을 주입해 네 Provider 선택, 인증, Context 구조,
오류 redaction, timeout/cancel, Tool loop·정책 차단·호출 한도와 SQLite 재연결·대화 복원·Agent/Thread 분리·중복 요청·저장 실패 처리를 검증한다.
실제 SDK 어댑터도 네 종류 모두 생성하지만 외부 API를 호출하지 않는다.
Mattermost Resolver/Prompt/Context/Orchestrator/HTTP/Bot reply 테스트는
`GOCACHE=/tmp/agentbridge-go-cache go test -race ./server/...`로 `agent-bridge`에서 실행한다.
외부 Provider의 실제 인증·모델 접근 권한과 Mattermost 배포 후 대화는 별도 확인이 필요하다.

구현 근거: [LangGraph Graph API](https://docs.langchain.com/oss/python/langgraph/quickstart),
[Gemini](https://docs.langchain.com/oss/python/integrations/chat/google_generative_ai),
[Claude](https://docs.langchain.com/oss/python/integrations/chat/anthropic),
[Ollama](https://docs.langchain.com/oss/python/integrations/chat/ollama),
[OpenAI GPT-5.4](https://developers.openai.com/api/docs/models/gpt-5.4).

## Thread Memory / Checkpoint

Go는 `agent_id`, `root_post_id`, `post_id`, 최신 System Prompt와 현재 user 메시지만 보낸다.
루트 게시글은 post.id, 답글은 root_id를 사용하며 런타임이
`mattermost:{agent_id}:{root_post_id}`를 LangGraph thread_id로 만든다.
`MessagesState`의 add_messages reducer로 user/assistant를 누적한다.
모델 설정과 System Prompt는 매 요청의 실행 context로 전달하므로 변경된 Agent 설정을 즉시 사용한다.
기존 Mattermost 쓰레드 기록은 자동 가져오지 않는다. 이 버전부터 처리한 대화가 checkpoint에 쌓인다.

`AsyncSqliteSaver`가 SQLite 파일에 state를 기록한다. Docker는 `checkpoints` named volume을
`/app/data`에 연결하고 기본 파일은 `/app/data/checkpoints.sqlite`이다.
컨테이너 재시작·재생성·일반 `docker compose down` 이후에도 volume을 유지한다.
`docker compose down -v`는 checkpoint volume까지 삭제한다.
로컬 실행에서는 `CHECKPOINT_DB=./data/checkpoints.sqlite`를 설정한다.
DB 디렉터리는 런타임 사용자에게 쓰기 권한이 있어야 한다.

같은 thread_id는 조회부터 graph 완료까지 직렬 실행한다. 현재 배포는 **단일 프로세스 / uvicorn worker 1개**를 전제로 한다.
여러 프로세스·복제본으로 확장할 때는 Postgres checkpointer와 분산 직렬화가 필요하다.
서로 다른 thread_id는 최대 4개까지 병렬 실행한다. 같은 쓰레드의 순서는 런타임 도착 순서이며
Mattermost 게시 시각에 따른 정렬이나 Plugin queue의 영속 복구는 아직 제공하지 않는다.
입력 ID는 Mattermost post.id, 답변 ID는 reply:{post.id}로 고정한다.
이미 완료한 post.id를 재전송하면 checkpoint의 답변을 반환하여 모델을 다시 호출하지 않는다.
Mattermost CreatePost 자체의 중복 방지와 실패한 게시의 자동 재시도는 별도 후속 범위다.

모델 입력은 시스템 프롬프트 16 KiB + 완결된 대화 단위로 최근 20개 메시지/64 KiB (현재 Tool 실행 turn은 메시지 수 제한에서 제외)로 제한한다.
전체 state와 checkpoint 이력은 유지하며 실제 tokenizer, retention, 요약은 아직 적용하지 않는다.
최근 메시지 선택은 graph.model_context에 모아 이후 summarization을 붙일 수 있다.
Mattermost는 사용자에게 보이는 원본, checkpoint는 Agent 실행 상태다.
Mattermost post 수정·삭제 또는 Agent 삭제는 checkpoint 자동 삭제로 연결하지 않는다.

checkpoint 초기화·읽기·쓰기 오류는 일반 503으로 처리하고 내부 경로나 대화 내용을 노출하지 않는다.
저장 실패 후 모델을 자동 재호출하거나 메모리 없는 응답으로 대체하지 않는다.
초기화 실패 시 서비스는 계속 실행되며 /healthz가 degraded를 반환한다.
저장 경로를 복구한 뒤 런타임을 재시작한다. 요청 중 저장 실패는 다른 요청의 처리를 중단하지 않는다.
모델 성공 후 Mattermost 게시 전 장애가 발생하면 실행 상태와 실제 대화 기록이 다를 수 있다.

설계 근거: [LangGraph Persistence](https://docs.langchain.com/oss/python/langgraph/persistence),
[AsyncSqliteSaver](https://github.com/langchain-ai/langgraph/blob/main/libs/checkpoint-sqlite/langgraph/checkpoint/sqlite/aio.py).


## 정책 기반 Tool Calling

Go가 KV Agent의 `tools` 전체를 `/v1/generate`에 전달한다. 설정 ID와 모델 함수 이름은 분리한다.

| 설정 ID | 함수 이름 | 구현 |
|---|---|---|
| debug-echo | debug_echo | 입력 문자열 반환; 연결 검증용 |
| web-search | web_search | Brave Search 웹 검색, 제목/URL/snippet 최대 5개 |

네 Provider 모두 LangChain `bind_tools`와 동일한 LangGraph 실행 경로를 사용한다.
실제 모델/로컬 모델 자체도 function calling을 지원해야 한다.
`enabled=false`는 Tool을 노출하지 않는다. `allowed - denied - require_confirmation`과 Registry의
교집합만 bind하며 policy_check와 실행 직전에 재검사한다. 미등록 Tool 및 승인 필요 Tool은 실행하지 않는다.

그래프: `START → generate → policy_check → tools → generate → END`.
Tool 실패/잘못된 인자/정책 차단은 비밀 정보를 제거한 ToolMessage로 반환하며 최종 답변을 다시 요청한다.
요청당 호출 시도 최대 10회 (병렬 호출도 각각 계산), Tool당 12초 제한, 웹 요청 10초,
검색 HTTP 응답 최대 1 MiB, Tool 결과 최대 24 KiB. 한도 이후에는 Tool을 더 실행하지 않고 그래프를 종료한다.
호출 및 결과는 체크포인트에 보존하며 메시지 창을 자를 때 Tool call/result 쌍을 분리하지 않는다.
Provider metadata를 보존해 Gemini thought signature 등의 후속 호출 정보를 유지한다.

`web-search`를 사용하려면 `agent-runtime/.env`에 `BRAVE_SEARCH_API_KEY`를 설정하고 런타임을 재생성한다.
Agent KV에는 검색 키도 저장하지 않는다. 검색 서비스는 모델 Provider와 독립적이다.
설정하지 않으면 검색 실패 ToolMessage가 반환된다. file-reader/pdf-reader/code-executor/vector-search는
UI의 향후 설정 항목으로 남아 있으며 Registry에 없으므로 실행되지 않는다.

로그: agent_id, thread_id, tool_id, tool_call_id, 인자 필드명, status, duration_ms, 고정 error code.
인자 값·전체 결과·SDK 예외 본문·credential은 기록하지 않는다.
현재 Tool은 조회/검증용이다. 미완료 요청 재시도는 일부 Tool을 다시 실행할 수 있으며,
향후 외부 쓰기 Tool에는 별도 멱등성/승인 처리가 필요하다.
