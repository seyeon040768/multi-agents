# Agent Bridge LangGraph Runtime

Mattermost Plugin은 Agent KV 조회·메시지 라우팅·Prompt/Thread Context·Bot 게시를 담당한다.
이 서비스는 LangGraph `START → generate → END`에서 Agent가 선택한 모델을 호출한다.
OpenAI·Gemini·Claude·Ollama 중 하나로 제한하거나 자동 전환하지 않는다.
Tool, delegation, 장기 Memory, streaming 및 자동 fallback은 이번 범위에 포함하지 않는다.

| Agent provider | Adapter | 실행 환경 설정 |
|---|---|---|
| openai | ChatOpenAI | OPENAI_API_KEY |
| google | ChatGoogleGenerativeAI | GOOGLE_API_KEY 또는 GEMINI_API_KEY |
| anthropic | ChatAnthropic | ANTHROPIC_API_KEY |
| ollama | ChatOllama | OLLAMA_BASE_URL |

Agent에는 provider/name/parameters만 저장한다. API Key와 모델 서버 주소는 요청이나 Agent KV로 받지 않는다.
로컬 모델은 이번 단계에서 Ollama를 통해 연결한다. 설치된 모델 이름과 Agent의 model.name이 일치해야 한다.
관리 UI의 모델 카탈로그는 현재 각 Provider당 기존 모델 하나를 제공한다.
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
오류 redaction, timeout/cancel, Tool 호출 거부를 검증한다.
실제 SDK 어댑터도 네 종류 모두 생성하지만 외부 API를 호출하지 않는다.
Mattermost Resolver/Prompt/Context/Orchestrator/HTTP/Bot reply 테스트는
`GOCACHE=/tmp/agentbridge-go-cache go test -race ./server/...`로 `agent-bridge`에서 실행한다.
외부 Provider의 실제 인증·모델 접근 권한과 Mattermost 배포 후 대화는 별도 확인이 필요하다.

구현 근거: [LangGraph Graph API](https://docs.langchain.com/oss/python/langgraph/quickstart),
[Gemini](https://docs.langchain.com/oss/python/integrations/chat/google_generative_ai),
[Claude](https://docs.langchain.com/oss/python/integrations/chat/anthropic),
[Ollama](https://docs.langchain.com/oss/python/integrations/chat/ollama),
[OpenAI GPT-5.4](https://developers.openai.com/api/docs/models/gpt-5.4).
