# Agent 관리 UI 미리보기

Mattermost에 webapp이 포함된 Plugin을 설치하고 활성화한 뒤 `/agent create` 또는 `/agent list`를 실행합니다.

- 시스템 관리자는 생성, 설정, 활성화/비활성화, 삭제를 사용할 수 있습니다. 일반 사용자는 목록만 조회합니다.
- 설정은 `docs/agent_definition.md`의 agent 구조를 따릅니다. 역할, 모델, 여섯 종류의 프롬프트, 도구, 협업, 출력, 행동, 권한 등의 설정을 제공합니다.
- 배열과 객체 설정은 JSON으로 입력합니다. 모델 provider/name은 webapp/src/models.ts의 임시 목록에서 선택합니다. Provider 변경 시 모델 선택이 초기화됩니다.
- 설정 변경은 브라우저 메모리에만 유지되며 새로고침 시 사라집니다. 다른 브라우저나 사용자와 공유되지 않습니다.
- Bot 계정, DB, Registry, Memory, Task, 실행 정책은 연결하지 않습니다. 활성화와 삭제는 임시 목록에만 반영합니다. Memory 보존 선택은 실제 데이터에 영향을 주지 않습니다.
- Messenger 연결 정보와 Lifecycle 정보는 읽기 전용이며 Agent ID는 생성 이후 변경할 수 없습니다.

검증: `npm run check-types`, `npm run build` (webapp 폴더), `go test ./server/...` (Plugin 루트).
