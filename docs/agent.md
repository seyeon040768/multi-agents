# Agent 관리 UI 구현 명세

## 1. 현재 구현 범위

Mattermost Agent Bridge Plugin에서 Agent 생성 및 관리 화면을 제공한다. 현재 버전은 **UI 미리보기**이며 Orchestrator는 아직 구현하지 않았다.

- Agent 설정과 목록은 React 컴포넌트의 브라우저 메모리에만 유지한다.
- Modal을 닫았다가 다시 열면 임시 목록을 유지한다. 브라우저 새로고침 또는 webapp 재로딩 시 초기화한다.
- 다른 사용자나 브라우저와 목록을 공유하지 않는다.
- KV 저장소, PostgreSQL, 외부 API에 Agent 설정을 저장하지 않는다.
- 실제 Mattermost Bot 생성·변경·삭제, LLM 실행, Tool 실행, Memory 및 Task 처리는 하지 않는다.

UI에는 위 제한을 알리는 안내 문구를 표시한다. 활성화·비활성화·삭제는 임시 데이터에만 반영한다.

## 2. 설정 구조의 기준

Agent 설정의 필드, 계층, 타입, 기본값과 입력 계약은 [agent_definition.md](./agent_definition.md)를 기준으로 한다. 이 문서는 UI 구현 설명이며 설정 정의를 독립적으로 변경하지 않는다. 설정 변경 시 기준 문서를 먼저 수정하고 구현과 이 문서를 함께 갱신한다. UI는 해당 문서의 `agent` 내부 구조를 편집하며, 현재 YAML 파일의 Import/Export나 최상위 스키마 버전(`version: "1.0"`) 편집은 제공하지 않는다.

기존 설계의 단일 System Prompt, Model Registry ID, 별도 Memory Policy·Response Policy·Task Limits를 현재 데이터 구조에 추가하지 않는다. 여러 Prompt와 상세 정책은 `agent_definition.md`에 정의된 항목을 사용한다. Agent Template 계층은 두지 않는다.

설정 초기값은 `agent-bridge/webapp/src/defaults.ts`에서 관리한다.

## 3. 명령과 사용자 흐름

Agent 관리 명령은 두 개로 제한한다.

```text
/agent create → 생성 Modal
/agent list   → Agent 목록 및 관리 Modal
```

React webapp의 Slash Command Hook이 명령을 처리하고 화면을 연다. webapp이 로딩되지 않았을 때 서버 명령 핸들러는 UI를 불러오라는 안내만 반환한다. 명령으로 Bot을 직접 생성하지 않는다.

### 생성

1. 시스템 관리자가 `/agent create` 또는 목록의 `[＋ Agent 생성]`을 선택한다.
2. 기본 정보와 정책을 입력한다.
3. 입력값 검증을 통과하면 임시 목록에 Agent를 추가한다.
4. 목록 화면으로 돌아간다.

### 목록 및 설정

목록에는 다음 정보를 표시한다.

- 표시 이름: `display_name`이 있으면 사용하고, 없으면 `name` 사용
- `@{id}` 형식의 임시 식별 표시와 설명 (실제 Bot Username 연결 아님)
- 역할, Model Provider / API 모델 ID, 허용 도구 수
- `lifecycle.enabled`에 따른 Active / Disabled 상태

관리자는 각 항목의 `[설정]`, `[활성화 / 비활성화]`, `[삭제]` 버튼을 사용할 수 있다. 설정은 생성과 같은 폼을 사용하며 Agent ID를 변경할 수 없다.

### 삭제

삭제 버튼을 누르면 확인 화면을 표시한다. Agent 이름, ID, Long-term Memory 보존 선택, 취소 및 삭제 버튼을 제공한다.

확인 후 임시 목록에서만 제거한다. Memory 보존 선택은 UI 미리보기이며 실제 데이터 보존·삭제에 영향을 주지 않는다.

## 4. 권한

| 사용자 | 조회 | 생성·수정·활성화·비활성화·삭제 |
|---|---|---|
| 일반 사용자 | 가능 | 불가 |
| Mattermost 시스템 관리자 (`system_admin`) | 가능 | 가능 |

webapp의 현재 사용자 역할에 따라 관리 UI를 표시한다. 일반 사용자가 `/agent create`를 실행해도 생성 폼은 표시하지 않는다.

현재는 영속 저장이나 서버 변경 API가 없으므로 UI 수준의 권한 제어다. 향후 서버 API를 연결할 때 서버에서도 동일한 권한을 검증해야 한다.

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
| `id` | 영문 소문자로 시작하는 3–32자의 영문 소문자·숫자·점·밑줄·하이픈. 임시 목록에서 중복 불가 |
| `name` | 공백만 입력할 수 없음 |
| `model.name` | 현재 Provider의 사전 모델 목록에서 선택 |
| `prompts.identity` | 공백만 입력할 수 없음 |

Agent ID는 생성 후 읽기 전용이다. 나머지 이름, 역할, 모델, 프롬프트와 정책은 설정 화면에서 수정할 수 있다. 숫자 항목의 범위 검증과 정책 간 정합성 검증은 향후 서버 연결 시 보완한다.

### enabled 연동

`enabled`를 가진 그룹에서 체크를 해제하면 같은 그룹의 나머지 입력 요소를 비활성화한다. 하위 그룹에도 적용하며, `enabled` 체크박스 자체는 다시 체크할 수 있도록 유지한다.

- `tools.enabled` → 허용·금지·승인 필요 도구 체크리스트
- `model.fallback.enabled` → 대체 모델 입력
- `collaboration.enabled` → 협업 기능 체크박스 및 위임 깊이

비활성화해도 기존 입력값은 유지하고 재활성화하면 다시 편집할 수 있다. `lifecycle.enabled`는 읽기 전용이며 목록의 활성화·비활성화 버튼으로 변경한다.

## 6. Provider별 모델 목록

UI의 사전 모델 목록은 `agent-bridge/webapp/src/models.ts`에서 관리한다. 중앙 Orchestrator Registry와 API 조회는 아직 연결하지 않았다.

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

임시 생성 시 생성·수정 시간을 브라우저에서 설정한다. 설정 저장 또는 활성화 변경 시 version을 증가시키고 수정 시간을 갱신한다. 이는 UI 데이터 변경 기록이며 서버 Optimistic Locking은 구현하지 않았다.

다음 정보는 읽기 전용이다.

```text
messenger.provider
messenger.user_id
messenger.username
messenger.bot
lifecycle.enabled
lifecycle.version
lifecycle.created_at
lifecycle.updated_at
```

## 9. 구현 파일과 빌드

| 파일 | 역할 |
|---|---|
| `agent-bridge/webapp/src/index.tsx` | 명령 Hook, Modal, 목록, 입력 폼, 체크리스트, 임시 관리 동작 |
| `agent-bridge/webapp/src/defaults.ts` | Agent 구조와 기본값 |
| `agent-bridge/webapp/src/models.ts` | Provider별 사전 모델 목록 |
| `agent-bridge/webapp/src/style.css` | Modal, 입력 안내, 비활성화 표시, 체크리스트 스타일 |
| `agent-bridge/server/command/command.go` | `/agent` 등록 및 webapp 미로딩 시 안내 |
| `agent-bridge/plugin.json` | 서버와 webapp bundle 선언 |

검증 명령:

```bash
cd agent-bridge/webapp
npm run check-types
npm run build

cd ..
go test ./server/...
git diff --check
```

설치용 Plugin 패키지 생성:

```bash
cd agent-bridge
make dist
```

`dist/com.seyeon.agentbridge-<버전>.tar.gz`를 Mattermost System Console의 Plugin Management에서 업로드하고 활성화한다. 업데이트 후 브라우저를 새로고침한다.

## 10. 향후 구현 항목

다음 기능은 현재 UI 구현에 포함하지 않는다.

1. Orchestrator 및 Agent 설정 영속 저장, PostgreSQL 연동
2. 중앙 Model·Tool Registry 연결 및 실제 실행 가능 목록 조회
3. Mattermost Bot 생성·정보 동기화·활성화·비활성화·삭제
4. 생성 중 PROVISIONING, 정상 ACTIVE, DISABLED, ERROR, DELETING 등의 서버 Lifecycle 및 실패 보상·복구
5. 서버 권한 검증, 설정 Version 기반 동시 수정 충돌 방지
6. Task 실행·취소, Channel membership 정리, 실제 Memory 보존·삭제
7. LLM 및 Tool 실행, 정책 적용, 비용·Token·시간 제한
8. 정책 간 정합성과 상세 스키마 검증
9. YAML Import/Export, 복제, 검색·필터, Audit Log 등 부가 관리 기능

후속 구현에서도 `/agent create`와 `/agent list`를 UI 진입점으로 유지하고, 설정 구조는 `agent_definition.md`를 기준으로 한다.
