# Agent Bridge 구현 명세 및 개발 현황

## 1. 현재 구현 범위

Mattermost Agent Bridge Plugin과 별도 Python LangGraph 실행 서비스로 Agent 관리부터 단일 Agent 텍스트 대화까지 제공한다.

2026-10-06 기준으로 **KV CRUD → Mattermost Bot Provisioning → 단일 Agent 채팅 MVP**를 구현하고 로컬 Mattermost에 배포했다. 기존 Gemini Agent의 실제 DM 답변까지 검증했다.

- Agent 설정은 Mattermost Plugin KV Store에 영속 저장한다.
- Modal을 열 때마다 서버 목록을 조회하고, 생성·수정·활성화·삭제 성공 후 목록을 다시 조회한다.
- 브라우저 새로고침과 Plugin 재시작 후에도 같은 KV 데이터를 읽는다.
- 실제 Mattermost Bot 생성·프로필 동기화·활성화·비활성화·삭제와 Bot user ID → Agent ID 역방향 조회를 제공한다.
- 단일 Agent의 DM·mention을 처리하고 Prompt와 Mattermost Thread Context를 LangGraph에 전달해 Bot으로 같은 Thread에 답변한다.
- OpenAI·Gemini·Claude·Ollama 어댑터를 제공하며 Agent 설정의 Provider와 모델을 사용한다. 실제 외부 호출은 Gemini로 검증했다.
- 인증 정보는 실행 서비스 환경 변수로 관리하고, 플러그인은 별도의 Runtime URL/Token으로 실행 서비스에 인증한다.
- Tool 실행, 장기 Memory, Multi-Agent 위임 및 Task 처리는 후속 범위다.

## 2. 설정 구조의 기준

Agent 설정의 필드, 계층, 타입, 기본값과 입력 계약은 [agent_definition.md](./agent_definition.md)를 기준으로 한다. 이 문서는 UI·서버·실행 서비스의 구현 설명이며 설정 정의를 독립적으로 변경하지 않는다. 설정 변경 시 기준 문서를 먼저 수정하고 구현과 이 문서를 함께 갱신한다. UI는 해당 문서의 `agent` 내부 구조를 편집하며, 현재 YAML 파일의 Import/Export나 최상위 스키마 버전(`version: "1.0"`) 편집은 제공하지 않는다.

기존 설계의 단일 System Prompt, Model Registry ID, 별도 Memory Policy·Response Policy·Task Limits를 현재 데이터 구조에 추가하지 않는다. 여러 Prompt와 상세 정책은 `agent_definition.md`에 정의된 항목을 사용한다. Agent Template 계층은 두지 않는다.

설정 초기값은 webapp의 `agent-bridge/webapp/src/defaults.ts`와 서버의 `agent-bridge/server/agent/defaults.json`에서 같은 계약으로 관리한다.

## 3. 명령과 사용자 흐름

Agent 관리 명령은 두 개로 제한한다.

```text
/agent create → 생성 Modal
/agent list   → Agent 목록 및 관리 Modal
```

React webapp의 Slash Command Hook이 명령을 처리하고 화면을 연다. webapp이 로딩되지 않았을 때 서버 명령 핸들러는 UI를 불러오라는 안내만 반환한다. 생성 폼 저장 시 서버가 실제 Bot을 생성한다.

### 생성

1. 시스템 관리자가 `/agent create` 또는 목록의 `[＋ Agent 생성]`을 선택한다.
2. 기본 정보와 정책을 입력한다.
3. 서버 권한 검사와 입력 검증을 통과하면 CAS로 Agent를 KV에 저장한다.
4. Bot을 생성하고 연결 정보와 runtime 상태를 저장한 뒤 목록으로 돌아간다. 실패 시 Agent는 ERROR 상태로 남는다.

### 목록 및 설정

목록에는 다음 정보를 표시한다.

- 표시 이름: `display_name`이 있으면 사용하고, 없으면 `name` 사용
- `messenger.username`에 저장한 실제 `@agent-<id>`와 설명, 연결 전에는 Bot 연결 대기 표시
- 역할, Model Provider / API 모델 ID, 허용 도구 수
- `runtime.status`와 실패 원인. `lifecycle.enabled`는 관리자가 원하는 상태

관리자는 각 항목의 `[설정]`, `[활성화 / 비활성화]`, `[삭제]` 버튼을 사용할 수 있다. 설정은 생성과 같은 폼을 사용하며 Agent ID를 변경할 수 없다.

### 삭제

삭제 버튼을 누르면 확인 화면을 표시한다. Agent 이름, ID, Long-term Memory 보존 선택, 취소 및 삭제 버튼을 제공한다.

조회한 version을 포함한 DELETE 요청으로 DELETING 상태를 저장하고, Bot 영구 삭제와 reverse mapping 제거 후 KV Agent를 삭제한다. Bot 삭제 실패 시 ERROR와 Agent 정보를 유지하고 목록을 다시 조회한다. Memory 기능은 아직 구현되지 않았으며 보존 선택은 실제 데이터 처리에 영향을 주지 않는다.

## 4. 권한

| 사용자 | 조회 | 생성·수정·활성화·비활성화·삭제 |
|---|---|---|
| 일반 사용자 | 가능 | 불가 |
| Mattermost 시스템 관리자 (`system_admin`) | 가능 | 가능 |

webapp의 현재 사용자 역할에 따라 관리 UI를 표시한다. 일반 사용자가 `/agent create`를 실행해도 생성 폼은 표시하지 않는다.

서버는 Mattermost가 전달한 사용자 ID로 인증을 확인한다. 읽기 API는 로그인한 사용자에게 허용하고, 모든 변경 API는 서버에서 사용자 역할의 system_admin을 검증한다.

## 5. 폼 구성 및 입력 방식

입력란에는 무엇을 입력할지 설명하는 회색 Placeholder와 예시를 표시한다. Placeholder는 입력값이 비어 있을 때 보인다.

역할, 모델, 프롬프트, 도구 영역은 기본으로 펼치고 나머지 상세 영역은 접힌 상태로 제공한다. 필요하면 사용자가 펼쳐 수정한다.

| 영역 | 항목 및 입력 방식 |
|---|---|
| Identity | ID, 이름, 표시 이름, 설명은 텍스트 입력. Avatar는 emoji/url 입력, tags는 JSON 배열 |
| Role | type은 드롭다운, title은 텍스트, specialties는 JSON 배열, capabilities는 체크리스트 |
| Model | provider/name은 연동 드롭다운, parameters는 숫자 입력, fallback.enabled는 체크박스, fallback.models는 JSON 배열 |
| Prompts | identity, task_instruction, reasoning_instruction, collaboration_instruction, communication_instruction, output_instruction은 여러 줄 입력 |
| Tools | enabled는 체크박스. allowed, denied, require_confirmation은 각각 체크리스트 |
| Context | instructions는 여러 줄 입력, sources는 체크리스트, max_context_tokens는 숫자 입력 |
| Collaboration | enabled와 기능별 권한은 체크박스, max_delegation_depth는 숫자 입력 |
| Communication | default_message_type, mention_policy, reply_policy는 텍스트. allowed_message_types는 체크리스트 |
| Output | format은 드롭다운, language는 텍스트, schema는 JSON 객체 또는 null, include 항목은 체크박스 |
| Behavior | autonomy는 드롭다운, 행동 정책은 체크박스, max_retries는 숫자 입력 |
| Permissions | 파일·네트워크·외부 작업·Agent 권한은 체크박스 |
| Messenger | 연결 정보는 읽기 전용, profile.display_name/avatar_url은 텍스트 입력 |
| Lifecycle | enabled, version, created_at, updated_at은 읽기 전용 |
| Metadata | JSON 객체 입력 |

JSON 입력란은 JSON 문법과 배열/객체 형태를 검증한다. 개별 JSON 원소의 세부 스키마 검증은 아직 제공하지 않는다.

### 필수 항목 및 검증

필수 항목에는 `*`를 표시한다.

| 항목 | 검증 |
|---|---|
| `id` | 영문 소문자로 시작하는 3–32자의 영문 소문자·숫자·점·밑줄·하이픈. KV CAS로 중복 생성 방지 |
| `name` | 공백만 입력할 수 없음 |
| `model.name` | 현재 Provider의 사전 모델 목록에서 선택 |
| `prompts.identity` | 공백만 입력할 수 없음 |

Agent ID는 생성 후 읽기 전용이다. 나머지 이름, 역할, 모델, 프롬프트와 정책은 설정 화면에서 수정할 수 있다. 숫자 항목의 범위 검증과 정책 간 정합성 검증은 후속 단계에서 보완한다.

### enabled 연동

`enabled`를 가진 그룹에서 체크를 해제하면 같은 그룹의 나머지 입력 요소를 비활성화한다. 하위 그룹에도 적용하며, `enabled` 체크박스 자체는 다시 체크할 수 있도록 유지한다.

- `tools.enabled` → 허용·금지·승인 필요 도구 체크리스트
- `model.fallback.enabled` → 대체 모델 입력
- `collaboration.enabled` → 협업 기능 체크박스 및 위임 깊이

비활성화해도 기존 입력값은 유지하고 재활성화하면 다시 편집할 수 있다. `lifecycle.enabled`는 읽기 전용이며 목록의 활성화·비활성화 버튼으로 변경한다.

## 6. Provider별 모델 목록

UI의 사전 모델 목록은 `agent-bridge/webapp/src/models.ts`에서 관리하고 서버 검증 목록과 일치시킨다. LangGraph 실행 서비스의 Provider 어댑터는 연결했으며, 모델 목록을 실행 서비스에서 동적으로 조회하는 API는 후속 범위다.

| Provider 값 | 표시 이름 | `model.name`에 저장하는 ID |
|---|---|---|
| `openai` | GPT-5.4 | `gpt-5.4` |
| `anthropic` | Claude Sonnet 4.6 | `claude-sonnet-4-6` |
| `google` | Gemini 3 Flash (Preview) | `gemini-3-flash-preview` |
| `ollama` | Qwen3 8B (Local) | `qwen3:8b` |

Provider를 선택하면 해당 Provider의 모델만 표시한다. Provider를 바꾸면 `model.name`을 비우고 다시 선택하도록 한다. 화면에는 표시 이름과 API 모델 ID를 함께 보여주고 설정에는 실제 ID를 저장한다.

예:

```yaml
model:
  provider: google
  name: gemini-3-flash-preview
```

기존 설정의 모델이 목록에 없으면 그 값을 표시하되 저장 전 등록된 모델을 다시 선택하게 한다. 모델 선택 UI는 API 연결이나 모델 실행 가능 여부를 확인하지 않는다.

## 7. 체크리스트 목록

선택값은 각 필드의 문자열 배열로 유지한다. 사전 목록에 없는 기존 선택값도 추가 항목으로 표시해 보존한다.

### Tools (임시 목록)

`tools.allowed`, `tools.denied`, `tools.require_confirmation`에 같은 목록을 제공한다. 세 목록은 독립적으로 선택하며 중복 선택의 정책 검증은 아직 하지 않는다.

| 이름 | ID |
|---|---|
| Web Search | `web-search` |
| File Reader | `file-reader` |
| PDF Reader | `pdf-reader` |
| Code Executor | `code-executor` |
| Vector Search | `vector-search` |

### Context sources

| 이름 | 값 |
|---|---|
| 프로젝트 | `project` |
| 대화 | `conversation` |
| 스레드 | `thread` |
| 지식 저장소 | `knowledge-base` |

### Allowed message types

| 이름 | 값 |
|---|---|
| 메시지 | `message` |
| 질문 | `question` |
| 답변 | `answer` |
| 요청 | `request` |
| 피드백 | `feedback` |
| 검토 | `review` |
| 결정 | `decision` |
| 요약 | `summary` |

### Role capabilities (임시 목록)

| 이름 | 값 |
|---|---|
| 작업 수행 | `task-execution` |
| 검토 | `review` |
| 요약 | `summarization` |
| 코드 작성 | `code-generation` |

## 8. 기본값과 읽기 전용 정보

전체 초기값은 `defaults.ts`와 `agent_definition.md`를 참고한다. 주요 기본값은 다음과 같다.

- 역할 `worker`, Model Provider `openai`, 모델 이름 미선택
- Model temperature `0.2`, max_tokens `8000`, top_p `null`
- Fallback 비활성화, 도구 활성화 및 선택 목록 비어 있음
- Collaboration 활성화, can_delegate 비활성화, max_delegation_depth `1`
- 기본 메시지 유형 `message`, 허용 유형은 8개 모두 선택
- Mention 정책 `allowed`, Reply 정책 `thread`
- Output format `text`, language `auto`, include 항목은 모두 비활성화
- Behavior autonomy `medium`, 재시도 활성화, max_retries `2`
- 파일 읽기와 Agent 메시지 권한 활성화. 파일 쓰기·네트워크·외부 작업·Agent 위임 권한 비활성화
- Messenger provider `mattermost`, bot `true`, 연결 ID와 Username은 `null`
- Lifecycle enabled `true`, version `1`, 생성·수정 시간은 최초 저장 전 `null`
- Runtime status `PROVISIONING`, error `null`

최초 KV 생성 시 서버가 enabled=true, version=1과 UTC 생성·수정 시간을 설정한다. Bot 연결과 runtime 저장마다 version이 추가로 증가한다. 설정 저장 또는 활성화 변경 시 version을 증가시키고 수정 시간을 갱신한다. created_at과 Messenger 연결 정보는 서버가 보존한다. 수정·활성화·삭제는 조회한 version을 전달하며, version 불일치 또는 CAS 실패는 409로 반환한다.

다음 정보는 읽기 전용이다.

```text
messenger.provider
messenger.user_id
messenger.username
messenger.bot
runtime.status
runtime.error
lifecycle.enabled
lifecycle.version
lifecycle.created_at
lifecycle.updated_at
```

## 9. 구현 파일과 빌드

| 파일 | 역할 |
|---|---|
| `agent-bridge/webapp/src/index.tsx` | 명령 Hook, Modal, 목록, 입력 폼, 체크리스트, 서버 관리 동작 |
| `agent-bridge/webapp/src/defaults.ts` | Agent 구조와 기본값 |
| `agent-bridge/webapp/src/models.ts` | Provider별 사전 모델 목록 |
| `agent-bridge/webapp/src/style.css` | Modal, 입력 안내, 비활성화 표시, 체크리스트 스타일 |
| `agent-bridge/server/command/command.go` | `/agent` 등록 및 webapp 미로딩 시 안내 |
| `agent-bridge/webapp/src/api/agents.ts` | Agent REST API client와 사용자용 오류 처리 |
| `agent-bridge/server/agent/` | Agent 모델·기본값·검증·Service·KV Store·CAS·Bot 연결 및 역방향 매핑 |
| `agent-bridge/server/agent_api.go`, `server/api.go` | Agent REST API·인증·관리자 권한·HTTP 오류 처리 |
| `agent-bridge/server/messenger/mattermost/` | Bot 생성·프로필·활성 상태·삭제와 Thread 답변 게시 |
| `agent-bridge/server/orchestrator/` | DM/mention Resolver·Agent 상태 검사·Context/모델/답변 연결 |
| `agent-bridge/server/prompt/`, `server/context/` | System Prompt 조합·Thread 최근 메시지·역할 변환·mention 제거 |
| `agent-bridge/server/modelclient/` | Provider 공통 요청/응답 계약과 LangGraph HTTP client |
| `agent-bridge/server/plugin.go`, `server/configuration.go` | 메시지 Hook·worker·취소 처리·Runtime URL/Token 설정 |
| `agent-bridge/plugin.json` | 서버와 webapp bundle, Runtime URL/Token 설정 선언 |
| `agent-runtime/agent_runtime/` | LangGraph 그래프·Provider 어댑터·인증된 HTTP 실행 API |
| `agent-runtime/pyproject.toml`, `requirements*.lock` | Python 의존성과 검증한 버전 |
| `agent-runtime/Dockerfile`, `compose.yaml` | 실행 서비스 이미지 및 Mattermost 네트워크 연결 |
| `agent-runtime/tests/`, `agent-bridge/server/**/*test.go` | 외부 LLM 없는 단위 테스트와 메시지 처리 테스트 |

검증 명령:

```bash
cd agent-bridge/webapp
npm run check-types
npm run build

cd ..
GOCACHE=/tmp/agentbridge-go-cache go test -race ./server/...

cd ../agent-runtime
# Python 가상 환경과 테스트 의존성 설치 후 실행
.venv/bin/python -m pytest -q

cd ..
git diff --check
```

설치용 Plugin 패키지 생성:

```bash
cd agent-bridge
make dist
```

`dist/com.seyeon.agentbridge-<버전>.tar.gz`를 Mattermost System Console의 Plugin Management에서 업로드하고 활성화한다. 업데이트 후 브라우저를 새로고침한다.

## 10. 향후 구현 항목

다음 기능은 현재 구현에 포함하지 않는다. 먼저 Thread Context·Summary Memory·Token 관리를 보완하고 Tool 실행을 추가한다.

1. Thread Context 요약, tokenizer 기반 `max_context_tokens` 관리 및 장기 Memory
2. Tool Calling·실행·권한/승인 정책과 중앙 Model·Tool Registry의 동적 목록 조회
3. Multi-Agent 협업·위임 및 Task 실행·취소
4. 동일 Thread의 동시 질문 순서 보장, 영속 메시지 중복 방지 및 재시작 중 요청 복구
5. 상세 정책 정합성·스키마 검증, 비용·실행 시간 제한, fallback·retry 정책 적용
6. Channel membership 자동 관리, 생성 시 DM 자동 열기 및 Memory 보존·삭제
7. YAML Import/Export, 복제, 검색·필터 및 Audit Log

Agent 관리 데이터는 현재 Mattermost Plugin KV를 사용한다. 별도 PostgreSQL 저장 계층은 구현하지 않았다. UI에서 보존하는 도구·협업·행동 정책 전체가 실행 단계에 적용되는 것은 아니다.
후속 구현에서도 `/agent create`와 `/agent list`를 UI 진입점으로 유지하고, 설정 구조는 `agent_definition.md`를 기준으로 한다.

## 11. Agent REST API 및 저장 계층

경로는 `/plugins/com.seyeon.agentbridge/api/v1` 아래다.

| Method | 경로 | 응답 | 권한 |
|---|---|---|---|
| GET | /agents | 200 Agent 배열 | 로그인 사용자 |
| GET | /agents/{id} | 200 Agent | 로그인 사용자 |
| POST | /agents | 201 Agent | system_admin |
| PUT | /agents/{id} | 200 Agent | system_admin |
| DELETE | /agents/{id}?version=N | 204 | system_admin |
| POST | /agents/{id}/enable | 200 Agent | system_admin |
| POST | /agents/{id}/disable | 200 Agent | system_admin |

PUT은 Agent의 lifecycle.version을 사용한다. enable/disable body는 `{"version": N}`이다.
에러는 `{"error":{"code":"AGENT_VERSION_CONFLICT","message":"..."}}` 형태다.
400은 입력 오류, 401은 미인증, 403은 권한 부족, 404는 미존재,
409는 중복 생성 또는 버전 충돌, 500은 내부 저장 오류다.

`server/agent`의 모델·검증·Service·Store·KVAgentStore로 계층을 나눈다.
`webapp/src/api/agents.ts`가 HTTP 호출을 담당한다. KV 키는
`agent:v1:<id>` 및 `agent:index:v1`이며 내부 예약 prefix를 사용하지 않는다.

인덱스는 CAS로 ID를 예약한 뒤 Agent를 CAS 생성한다. 생성 중 중단되면
인덱스에 없는 Agent가 생기지 않으며, 존재하지 않는 Agent ID는 List에서 건너뛴다.
동시 삭제와 같은 ID 재생성 시 새 Agent가 목록에서 사라지는 문제를 피하기 위해
삭제된 ID도 인덱스에 유지한다. 따라서 인덱스는 한 번 사용한 ID의 집합이며
삭제된 Agent JSON은 KVCompareAndDelete로 실제 제거한다. 인덱스 정리는 후속 작업이다.

API는 요청을 1 MiB로 제한하고 알 수 없는 JSON 필드 및 타입 오류를 거부한다.
모델 자격 증명은 Agent에 정의하지 않는다. Provider 인증 정보는 LangGraph 실행 서비스의 환경 변수로 관리한다.
숫자 범위와 상세 정책 정합성 검증은 아직 구현하지 않았다.

## 12. Mattermost Bot Provisioning

`AgentService → BotProvisioner → Mattermost Plugin API`로 연결한다.
서버가 username을 `agent-<id>`로 결정하며 클라이언트 연결 정보와 runtime을 신뢰하지 않는다.
Bot 표시 이름은 messenger.profile.display_name, display_name, name 순으로 선택한다.
Agent의 이름·표시 이름·설명·avatar·messenger.profile 변경 시 Bot 프로필을 동기화한다.
모델 또는 Prompt만 수정한 경우 Bot API를 호출하지 않는다.

Mattermost의 EnsureBotUser는 플러그인 KV의 단일 botuser 키를 재사용하므로 여러 Agent에 사용하지 않는다.
독립 Bot은 CreateBot으로 만들고 기존 소유 Bot은 PatchBot으로 갱신한다.
같은 username의 일반 사용자나 다른 소유자의 Bot은 채택하거나 변경하지 않는다.
참조: [Mattermost EnsureBot 구현](https://github.com/mattermost/mattermost/blob/master/server/channels/app/bot.go).

runtime은 서버 전용 `{status, error}`이며 상태는 PROVISIONING, ACTIVE, DISABLED, ERROR, DELETING이다.
Agent 생성은 PROVISIONING 저장 → Bot 생성 → user_id/username 저장 → reverse mapping 저장 → ACTIVE 저장 순서다.
실패 시 ERROR와 원인을 저장하며 생성 API는 저장된 Agent를 201로 반환한다.
수정·활성화 API도 외부 작업 실패 시 ERROR Agent를 200으로 반환한다. 삭제 실패는 500이며 Agent를 보존한다.
KV 자체가 불가하면 실패 상태 기록도 불가능할 수 있다. 이미 기록한 중간 상태와 연결을 사용해 재시도한다.

Agent별 클러스터 잠금과 CAS로 오래된 요청이 Bot을 변경하기 전에 차단한다.
중간 상태와 연결도 각각 CAS 저장하므로 한 요청에서 version이 여러 번 증가할 수 있다.
UI는 응답/목록의 최신 version을 사용하며 고정 증가량을 가정하지 않는다.
역방향 키 `agent:bot:v1:<user_id>` 값은 Agent ID 원문이며 CAS로 저장·삭제한다.
Bot을 이미 영구 삭제한 뒤 KV 삭제가 실패해도 다음 삭제는 idempotent하게 완료할 수 있다.

ERROR 또는 중단된 PROVISIONING은 목록의 Bot 연결 재시도로 복구한다.
KV만 있는 이전 Agent도 같은 버튼으로 계정을 생성한다. Plugin 재시작은 기존 연결을 그대로 읽으며 자동 재생성하지 않는다.
Bot 생성 직후 연결 저장 전에 중단된 경우 서버가 username과 소유권으로 계정을 찾아 재사용하거나 삭제한다.

avatar.url 또는 messenger.profile.avatar_url은 public HTTPS 이미지로 동기화한다.
프로필의 URL이 우선이며 최대 2 MiB, PNG/JPEG/GIF, 각 변 4096픽셀까지 허용한다.
사설·loopback 주소 접근과 HTTP redirect는 차단한다. URL이 없으면 Agent ID 기반 기본 색상 이미지를 사용한다.
avatar.emoji는 설정으로 보존하며 Bot 프로필 사진에 문자로 렌더링하지 않는다.
BotProvisioner는 계정 관리만 담당한다. 메시지 처리와 모델 호출은 별도 Orchestrator 및 LangGraph 실행 서비스가 담당한다.

## 13. 단일 Agent 채팅 MVP

`MessageHasBeenPosted → Resolver → Prompt/Thread Context → LangGraph HTTP → Agent Bot reply`로 처리한다.
System·Bot·plugin-generated 메시지는 제외한다. 일반 채널 메시지와 그룹 DM은 무시한다.
1:1 DM 또는 정확한 `@agent-<id>` mention을 처리하며 여러 Agent를 함께 mention한 요청은 무시한다.
채널에서는 Bot이 이미 참여해야 한다. Bot을 초대하지 않은 private 채널 내용을 외부 모델에 전달하지 않는다.
채널의 후속 Thread 질문도 Agent를 mention해야 하며, Agent와의 DM Thread에서는 mention 없이 처리한다.

역방향 KV로 Agent를 찾고 enabled=true, runtime=ACTIVE인 경우에만 실행한다.
현재 요청보다 이전인 동일 채널 Thread의 최근 19개 게시글과 현재 메시지를 시간순으로 구성한다.
Agent Bot 게시글은 assistant, 나머지는 user 역할이며 Agent mention은 제거한다.
시스템 프롬프트에는 identity·role·각 instruction·출력 형식/언어와 이번 단계의 기능 제한을 포함한다.
현재 메시지는 중복 포함하지 않고, 삭제·system·에러 응답 및 다른 채널 게시글은 Context에서 제외한다.
Context 텍스트는 최대 64 KiB, 시스템 프롬프트는 최대 16 KiB로 제한한다.
max_context_tokens에 맞춘 tokenizer·요약, 파일 내용 및 Context Source 로딩은 후속 범위다.

플러그인은 4개 worker와 최대 64개 대기 요청을 사용한다. 큐가 가득 차면 Bot으로 재시도 안내를 보낸다.
요청 timeout은 플러그인 90초, runtime 80초이며 Plugin 비활성화 시 실행 중 요청을 취소한다.
답변은 질문의 root_id(루트 질문이면 질문 ID)를 사용하며 최대 16,000자 이내로 게시한다.
모델 호출 후 Agent 상태를 다시 확인해 비활성화·삭제된 Agent의 답변을 억제한다.
모델 오류는 사용자용 일반 메시지로 알리고 내부 오류 본문·API Key·Prompt는 게시하지 않는다.
Provider 오류 로그에는 provider와 예외 타입만 남긴다.
대기 큐는 메모리이므로 Plugin 재시작 중 요청을 복구하지 않는다. Agent/Bot 연결은 KV에서 그대로 읽는다.
동일 Thread의 동시 질문 순서 보장, 메시지 중복 이벤트에 대한 영속 deduplication은 후속 범위다.

실행 서비스는 저장소의 `agent-runtime/`에서 Python LangGraph StateGraph로 구현한다.
그래프는 START → generate → END이며 Tool 노드·Tool binding·checkpointer가 없다.
Agent의 provider/name 그대로 모델을 선택한다. openai, google(Gemini), anthropic(Claude), ollama(로컬)를 지원한다.
자동 fallback이나 OpenAI로의 Provider 치환은 하지 않는다. 관리 UI의 기존 사전 모델 목록을 유지한다.
Provider 환경 변수·실행 방법·Plugin 설정은 [실행 서비스 README](../agent-runtime/README.md)를 따른다.


## 14. 실행 서비스 설정 및 현재 배포

Provider 인증 정보는 `agent-runtime/.env` 등 실행 서비스의 환경 변수로 설정한다.
Agent KV에는 API Key를 저장하지 않는다. `.env`는 Git 및 Docker build context에서 제외한다.

| 용도 | 설정 |
|---|---|
| 실행 서비스 요청 인증 | `AGENT_RUNTIME_TOKEN` |
| OpenAI | `OPENAI_API_KEY` |
| Gemini | `GOOGLE_API_KEY` 또는 `GEMINI_API_KEY` |
| Claude | `ANTHROPIC_API_KEY` |
| 로컬 Ollama | `OLLAMA_BASE_URL` |
| Mattermost Docker 네트워크 | `MATTERMOST_DOCKER_NETWORK` |

Mattermost System Console의 Multi Agent Bridge Plugin 설정에서 `LangGraph Runtime URL`과
`LangGraph Runtime Token`을 설정한다. Token은 실행 서비스의 `AGENT_RUNTIME_TOKEN`과 일치해야 한다.
Runtime Token은 플러그인의 secret 설정이며 Provider API Key와 별개다.
실행 서비스는 Token이 없으면 시작을 거부하고 `POST /v1/generate`에 Bearer 인증을 요구한다.
`GET /healthz`는 서비스 연결 확인용이다.

현재 로컬 배포는 다음과 같다. 다른 환경에서는 주소와 네트워크를 환경에 맞게 지정한다.

| 항목 | 현재 값 |
|---|---|
| Mattermost | 11.7.0, `docker-mattermost-1` |
| 실행 서비스 | `agent-runtime-agent-runtime-1` |
| 공통 Docker 네트워크 | `docker_default` |
| 플러그인에서 접속하는 Runtime URL | `http://agent-runtime:8000` |
| 호스트 health check | `http://127.0.0.1:8000/healthz` |
| 실제 검증 Agent | `test-agent`, `@agent-test-agent` |
| 실제 검증 모델 | `google / gemini-3-flash-preview` |

Gemini 인증은 사용자가 지정한 저장소 루트 `.env`의 키를 실행 서비스 환경에 반영했다.
플러그인 패키지 업로드·활성화와 실행 서비스 Docker 이미지 빌드·기동을 완료했다.
구체적인 재배포 방법은 [실행 서비스 README](../agent-runtime/README.md)를 따른다.

Bot 생성만으로 사용자의 좌측 DM 목록에 자동 표시되지는 않는다.
새 다이렉트 메시지에서 Bot username을 검색해 대화를 열고 질문하면 된다.
채널 대화는 Bot을 초대한 뒤 `@agent-<id>`를 mention한다.
Agent 생성 시 DM 자동 열기나 자동 채널 참여는 구현하지 않았다.

## 15. 완료한 검증과 남은 운영 확인

2026-10-06에 기존 Gemini Agent로 실제 호출과 Mattermost DM 흐름을 검증했다.

- LangGraph 그래프 → Gemini 실제 호출: HTTP 200과 `안녕하세요` 응답 확인.
- Mattermost에서 `seyeon`이 `연결 테스트입니다.`를 전송하고 실제 `agent-test-agent` Bot이 `안녕하세요`라고 답변.
- 답변의 root_id가 질문 ID와 일치해 같은 Thread에 게시됨을 확인.
- 해당 Thread의 Bot 답변이 한 개로 유지돼 자기 답변을 반복 처리하지 않음을 확인.
- Plugin disable/enable 후 Agent의 ACTIVE 상태와 기존 Bot user_id/username이 유지됨을 확인.

자동 검증도 완료했다.

| 검증 | 결과 |
|---|---|
| Go `go test -race ./server/...` | 통과: KV/CAS·권한·Bot 관리·Resolver·Prompt/Context·Orchestrator·Hook·HTTP·답변 게시 |
| Python `pytest` | 13개 통과: 네 Provider 라우팅·SDK 생성·인증·timeout/cancel·오류 본문 비노출·Tool 호출 거부 |
| `npm run check-types` | 통과 |
| `npm run build` | 통과 |
| `make dist` | 통과: 5개 플랫폼 서버 바이너리와 webapp 패키징 |
| Docker 실행 서비스 이미지 빌드 및 `/healthz` | 통과 |
| `git diff --check` | 통과 |

자동 테스트에서는 외부 LLM을 호출하지 않고 mock을 사용한다.
OpenAI·Claude·Ollama의 실제 인증 및 모델 응답, 채널 mention과 여러 차례의 실제 Thread 후속 대화는 추가 운영 확인 대상이다.
Plugin 재활성화 후 연결 정보 유지까지 확인했으며 재활성화 뒤 새 질문의 실제 답변은 별도 확인 대상이다.
상세 증거와 테스트 범위는 [채팅 MVP 검증 기록](agent_chat_validation.md)에 남겼다.
