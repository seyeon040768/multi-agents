# Agent 설정 정의

## 문서 우선순위와 변경 규칙

이 문서는 Agent 설정의 필드, 계층, 타입 및 기본값을 정의하는 기준 문서다. [agent.md](./agent.md)는 이 구조를 사용하는 UI와 현재 구현 범위를 설명한다. 두 문서의 설정 정의가 다르면 이 문서를 우선한다.

- 필드 추가·이름 변경·계층 변경·기본값 변경은 이 문서에 먼저 반영하고 구현과 `agent.md`를 함께 갱신한다.
- UI의 체크리스트나 드롭다운은 기존 필드의 입력 방식이다. 별도의 설정 구조로 바꾸지 않는다.
- 임시 모델·도구·Capabilities 목록은 스키마의 고정 Enum이 아니다. 실제 목록은 구현 파일에서 관리하며 확장할 수 있다.
- UI 미리보기의 임시 데이터와 향후 서버 실행 기능을 구분한다. 아직 구현하지 않은 기능을 현재 설정이나 실행 동작으로 설명하지 않는다.

## 설정 구조와 값의 의미

- 최상위 `version: "1.0"`은 YAML 스키마 버전이다. `agent.lifecycle.version`은 개별 Agent 설정 변경 버전이며 둘은 서로 다르다.
- `agent.id`는 불변 식별자다. `name`은 Agent 이름, `display_name`은 선택적인 표시 이름이며 비어 있으면 UI에서 `name`을 사용한다.
- `agent.model.provider`는 Provider 식별자이고 `agent.model.name`은 실제 API 모델 ID다. `model_id` 필드로 대체하지 않는다.
- Prompt는 `agent.prompts`의 여섯 필드로 관리한다. 별도 단일 `system_prompt` 필드를 추가하지 않는다.
- Tool은 `agent.tools.enabled`, `allowed`, `denied`, `require_confirmation`으로 관리한다. 세 선택 목록은 Tool ID 문자열 배열이다.
- File Reader 실행 권한은 `agent.permissions.files.read`다. `file_read`라는 별도 필드를 추가하지 않는다. `tools.enabled=true`·allowed 포함·denied 제외와 이 권한을 모두 만족해야 파일에 접근한다. `require_confirmation` 포함 시 기존 승인 흐름도 통과해야 한다.
- `role.capabilities`, `context.sources`, `communication.allowed_message_types`는 문자열 배열이다. 체크리스트에서도 배열 구조를 유지한다.
- Boolean 필드는 YAML Boolean으로 저장한다. 체크박스가 비활성화되어도 저장된 값을 지우지 않는다.
- `enabled`가 있는 그룹의 UI는 해당 값이 false이면 나머지 하위 입력 요소를 비활성화하고, 다시 true로 변경하면 보존된 값을 편집할 수 있게 한다.
- Bot의 기본 팀 자동 가입은 Plugin 전역 설정 `Agent Bot Team Name`으로 관리한다(기본 `happyseyeon`). Agent 스키마에 팀 설정을 추가하지 않는다.
- Messenger 연결 정보와 Lifecycle 정보는 시스템이 관리한다. `messenger.profile`은 사용자 설정이며 연결 ID와 구분한다.
- 별도 Memory Policy, Response Policy, Task Limits는 아직 정의하지 않았다. Bot ID는 messenger.user_id에 저장한다. runtime.status는 PROVISIONING, ACTIVE, DISABLED, ERROR, DELETING이며 runtime.error는 오류 문자열 또는 null이다. 두 필드는 서버가 관리한다.

## 현재 UI의 입력 계약

필수 항목은 `agent.id`, `agent.name`, `agent.model.name`, `agent.prompts.identity`다. UI에서 `*`를 표시한다. 이름과 Identity Prompt는 공백만 입력할 수 없다.

Agent ID는 `^[a-z][a-z0-9._-]{2,31}$`를 따르며 Mattermost KV에서 CAS로 중복 생성을 방지한다. 생성 이후에는 읽기 전용이다.

모델 Provider와 이름은 사전 목록에서 선택한다. Provider 변경 시 모델 이름을 비우고 다시 선택하게 한다. 목록에 없는 모델은 저장 전에 다시 선택해야 한다. 목록은 `agent-bridge/webapp/src/models.ts`와 `agent-bridge/server/agent/validation.go`에서 같은 계약으로 관리한다. 예를 들어 Gemini 3 Flash (Preview)는 아래처럼 표현한다.

```yaml
model:
  provider: google
  name: gemini-3-flash-preview
```

체크리스트 값의 현재 목록은 다음과 같다. Tool과 Capabilities는 UI용 임시 목록이다.

| 필드 (`agent.` 생략) | 선택값 |
|---|---|
| `tools.allowed`, `tools.denied`, `tools.require_confirmation` | `web-search`, `file-reader`, `pdf-reader`, `code-executor`, `vector-search` |
| `role.capabilities` | `task-execution`, `review`, `summarization`, `code-generation` |
| `context.sources` | `project`, `conversation`, `thread`, `knowledge-base` |
| `communication.allowed_message_types` | `message`, `question`, `answer`, `request`, `feedback`, `review`, `decision`, `summary` |

사전 체크리스트에 없는 기존 선택값도 유지한다. 나머지 배열·객체 항목은 현재 UI에서 JSON 형식으로 편집하지만, YAML 표현 시에도 같은 배열·객체 타입을 유지한다.

읽기 전용 항목은 다음과 같다 (`agent.` 생략).

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

`lifecycle.enabled`는 목록의 활성화·비활성화 API로 변경한다. 서버가 최초 KV 생성 시 enabled=true, version=1과 UTC 생성·수정 시간을 설정한다. Bot 작업의 중간 상태와 연결 저장마다 version을 추가로 증가시키므로 최종 생성 응답 version은 1보다 클 수 있다. 설정 저장이나 활성화 변경 시 version을 증가시키고 수정 시간을 갱신하며 created_at은 보존한다. 생성·수정 시간은 ISO 8601 문자열이며 저장 전에는 null이다. 수정·삭제·활성화 변경에는 사용자가 조회한 version이 필요하고, version 검증과 KV CAS로 동시 변경을 차단한다. 설정 저장으로 lifecycle.enabled 또는 Messenger 연결 정보를 변경할 수 없다. Agent JSON에는 모델 자격 증명 필드를 정의하지 않으며 알 수 없는 필드는 API에서 거부한다.

## YAML 구조 예시

아래는 구조와 각 필드의 의미를 설명하는 예시다. Prompt와 Context의 설명 문장은 입력 안내이며 생성 폼에 자동으로 저장하지 않는다. 실제 생성 폼은 텍스트 입력을 비운 상태로 시작한다. 필수값이 비어 있는 초기 설정은 저장 가능한 완성된 Agent가 아니다.

UI 기본값의 구현 위치는 `agent-bridge/webapp/src/defaults.ts`다.

```yaml
version: "1.0"

agent:
  # ---------------------------------------------------------------------------
  # Identity
  # ---------------------------------------------------------------------------
  id: agent-id

  name: "Agent Name"
  display_name: "에이전트 표시 이름"

  description: >
    이 에이전트의 목적과 역할을 설명한다.

  avatar:
    emoji: null
    url: null

  tags: []


  # ---------------------------------------------------------------------------
  # Role
  # ---------------------------------------------------------------------------
  role:
    type: worker
    # leader | worker | reviewer | specialist | coordinator | custom

    title: ""

    specialties: []

    capabilities: []
    # 문자열 ID 배열. 현재 UI는 임시 목록의 체크리스트로 선택한다.


  # ---------------------------------------------------------------------------
  # Model
  # ---------------------------------------------------------------------------
  model:
    provider: openai
    name: ""
    # 선택한 Provider의 실제 API 모델 ID. 생성 시 반드시 선택한다.

    parameters:
      temperature: 0.2
      max_tokens: 8000
      top_p: null

    fallback:
      enabled: false

      models: []
      # - provider: anthropic
      #   name: ""
      # - provider: google
      #   name: ""


  # ---------------------------------------------------------------------------
  # Prompt
  # ---------------------------------------------------------------------------
  prompts:

    identity: |
      에이전트가 누구인지 정의한다.

      역할, 전문성, 관점, 기본적인 행동 성향 등을 기술한다.


    task_instruction: |
      작업을 수행할 때 따라야 할 기본 원칙을 정의한다.

      어떤 정보를 확인해야 하는지,
      어떤 방식으로 판단해야 하는지,
      어떤 행동을 우선해야 하는지 등을 기술한다.


    reasoning_instruction: |
      문제를 분석하고 판단할 때 따라야 할 원칙을 정의한다.

      충분한 근거가 없는 내용을 사실처럼 단정하지 않는다.
      주어진 컨텍스트를 우선적으로 사용한다.
      불확실한 내용은 불확실하다고 표현한다.


    collaboration_instruction: |
      다른 에이전트와 협업할 때의 행동 방식을 정의한다.

      필요한 경우 다른 에이전트에게 질문할 수 있다.
      자신의 역할이나 전문 범위를 벗어나는 작업은
      적절한 에이전트에게 요청할 수 있다.

      다른 에이전트의 결과를 참고할 수 있지만
      무조건적으로 신뢰하지 않고 필요한 경우 검토한다.


    communication_instruction: |
      사용자 또는 다른 에이전트와 메시지를 주고받을 때의
      커뮤니케이션 원칙을 정의한다.

      메시지는 목적과 결론이 명확해야 한다.
      질문이 필요한 경우 무엇이 필요한지 구체적으로 표현한다.


    output_instruction: |
      최종 결과를 어떤 형식과 수준으로 작성할지 정의한다.

      결과는 이해하기 쉬운 형태로 작성한다.
      필요한 경우 근거, 판단, 불확실성 등을 함께 포함한다.


  # ---------------------------------------------------------------------------
  # Tools
  # ---------------------------------------------------------------------------
  tools:
    enabled: true

    allowed: []
    # - web-search
    # - file-reader
    # - code-executor

    denied: []

    require_confirmation: []
    # 특정 tool 실행 전에 별도 승인이나 정책 검사가 필요한 경우


  # ---------------------------------------------------------------------------
  # Knowledge / Context
  # ---------------------------------------------------------------------------
  context:
    instructions: |
      작업 시 제공되는 컨텍스트를 어떻게 다룰지 정의한다.

    sources: []
    # - project
    # - conversation
    # - thread
    # - knowledge-base

    # Runtime 입력 Context 예산. null이면 16000, 지정 시 512~2000000.
    # 보수적 Token 추정으로 System·Summary·최근 원문·Tool 스키마를 계산한다.
    # 입력에는 예산의 90%를 사용하고 10%는 여유분으로 남긴다.
    # 모델 출력 한도는 model.parameters.max_tokens로 별도 관리한다.
    max_context_tokens: null


  # ---------------------------------------------------------------------------
  # Collaboration
  # ---------------------------------------------------------------------------
  collaboration:
    enabled: true

    can_delegate: false

    can_receive_tasks: true

    can_message_agents: true

    can_mention_agents: true

    can_create_threads: false

    can_join_threads: true

    can_review_other_agents: false

    can_request_review: true

    max_delegation_depth: 1


  # ---------------------------------------------------------------------------
  # Communication
  # ---------------------------------------------------------------------------
  communication:
    default_message_type: message

    allowed_message_types:
      - message
      - question
      - answer
      - request
      - feedback
      - review
      - decision
      - summary

    mention_policy: allowed

    reply_policy: thread


  # ---------------------------------------------------------------------------
  # Output
  # ---------------------------------------------------------------------------
  output:
    format: text
    # text | markdown | json | structured

    language: auto

    schema: null

    include:
      confidence: false
      references: false
      reasoning_summary: false


  # ---------------------------------------------------------------------------
  # Behavior
  # ---------------------------------------------------------------------------
  behavior:
    autonomy: medium
    # low | medium | high

    ask_when_uncertain: true

    ask_when_missing_context: true

    stop_when_blocked: false

    retry_on_failure: true

    max_retries: 2


  # ---------------------------------------------------------------------------
  # Permissions
  # ---------------------------------------------------------------------------
  permissions:
    files:
      # file-reader 실행에 실제 적용. 현재 요청 Post의 텍스트 첨부만 지원.
      read: true
      write: false

    network:
      access: false

    external_actions:
      allowed: false

    agents:
      message: true
      delegate: false


  # ---------------------------------------------------------------------------
  # Messenger
  # ---------------------------------------------------------------------------
  messenger:
    provider: mattermost

    user_id: null
    username: null

    bot: true

    profile:
      display_name: null
      avatar_url: null


  # ---------------------------------------------------------------------------
  # Lifecycle
  # ---------------------------------------------------------------------------
  runtime:
    status: PROVISIONING
    error: null

  lifecycle:
    enabled: true

    version: 1

    created_at: null
    updated_at: null


  # ---------------------------------------------------------------------------
  # Metadata
  # ---------------------------------------------------------------------------
  metadata: {}
```