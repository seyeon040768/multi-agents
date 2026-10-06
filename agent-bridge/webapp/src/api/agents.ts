import {Agent} from '../defaults';

const base = '/plugins/com.seyeon.agentbridge/api/v1/agents';
const messages: Record<string, string> = {
    AGENT_VERSION_CONFLICT: '이 Agent는 다른 곳에서 수정되었습니다. 최신 정보를 다시 불러와 주세요.',
    AGENT_ALREADY_EXISTS: '이미 사용 중인 Agent ID입니다.',
    AGENT_NOT_FOUND: 'Agent를 찾을 수 없습니다. 최신 목록을 다시 불러와 주세요.',
    INVALID_AGENT_ID: 'Agent ID 형식을 확인해주세요.',
    INVALID_AGENT_NAME: 'Agent 이름을 입력해주세요.',
    INVALID_PROMPT: 'Identity Prompt를 입력해주세요.',
    INVALID_MODEL: '등록된 Provider와 모델을 선택해주세요.',
    INVALID_VERSION: '최신 Agent 정보를 다시 불러와 주세요.',
    INVALID_JSON: '설정의 JSON 형식과 필드 타입을 확인해주세요.',
    UNAUTHORIZED: '로그인 후 다시 시도해주세요.',
    INTERNAL_ERROR: 'Agent 저장소 요청에 실패했습니다. 다시 시도해주세요.',
    FORBIDDEN: '시스템 관리자만 Agent를 변경할 수 있습니다.',
};
async function request<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
    const response = await fetch(base + path, {method, credentials: 'same-origin', headers: {'Content-Type': 'application/json', 'X-Requested-With': 'XMLHttpRequest'}, body: body === undefined ? undefined : JSON.stringify(body)});
    if (!response.ok) {
        const payload = await response.json().catch(() => ({}));
        throw new Error(messages[payload.error?.code] || payload.error?.message || 'Agent 요청에 실패했습니다.');
    }
    return response.status === 204 ? undefined as T : response.json();
}
export const listAgents = () => request<Agent[]>('');
export const getAgent = (id: string) => request<Agent>(`/${encodeURIComponent(id)}`);
export const createAgent = (agent: Agent) => request<Agent>('', 'POST', agent);
export const updateAgent = (agent: Agent) => request<Agent>(`/${encodeURIComponent(agent.id)}`, 'PUT', agent);
export const deleteAgent = (id: string, version: number) => request<void>(`/${encodeURIComponent(id)}?version=${version}`, 'DELETE');
export const enableAgent = (id: string, version: number) => request<Agent>(`/${encodeURIComponent(id)}/enable`, 'POST', {version});
export const disableAgent = (id: string, version: number) => request<Agent>(`/${encodeURIComponent(id)}/disable`, 'POST', {version});
