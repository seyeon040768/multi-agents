import React, {useEffect, useRef, useState} from 'react';
import {Agent, freshAgent} from './defaults';
import {modelCatalog} from './models';
import './style.css';
import {listAgents, createAgent, updateAgent, deleteAgent, enableAgent, disableAgent} from './api/agents';

const pluginId = 'com.seyeon.agentbridge';
type Store = {getState: () => any; subscribe: (callback: () => void) => () => void};
type Registry = {registerRootComponent: (component: React.ComponentType) => void; registerSlashCommandWillBePostedHook: (hook: (message: string, args: any) => any) => void};
const choices: Record<string, string[]> = {'role.type': ['leader', 'worker', 'reviewer', 'specialist', 'coordinator', 'custom'], 'model.provider': Object.keys(modelCatalog), 'output.format': ['text', 'markdown', 'json', 'structured'], 'behavior.autonomy': ['low', 'medium', 'high']};
const readonly = new Set(['messenger.provider', 'messenger.user_id', 'messenger.username', 'messenger.bot', 'lifecycle.version', 'lifecycle.created_at', 'lifecycle.updated_at', 'lifecycle.enabled', 'runtime.status', 'runtime.error']);
const toolOptions = [
    {id: 'debug-echo', name: 'Debug Echo', description: '입력 문자열을 반환해 Tool 연결을 검증합니다'},
    {id: 'web-search', name: 'Web Search', description: '웹에서 정보를 검색합니다'},
    {id: 'file-reader', name: 'File Reader', description: '현재 메시지에 첨부된 텍스트 파일을 읽습니다 (파일 읽기 권한 필요)'},
    {id: 'pdf-reader', name: 'PDF Reader', description: 'PDF 문서를 읽습니다 (준비 중)'},
    {id: 'code-executor', name: 'Code Executor', description: '코드를 실행합니다 (준비 중)'},
    {id: 'vector-search', name: 'Vector Search', description: '저장된 지식을 검색합니다 (준비 중)'},
];
type ChecklistOption = {id: string; name: string; description: string};
const checklistOptions: Record<string, ChecklistOption[]> = {
    'context.sources': [
        {id: 'project', name: '프로젝트', description: '프로젝트에서 제공된 정보를 사용합니다'},
        {id: 'conversation', name: '대화', description: '현재 대화의 정보를 사용합니다'},
        {id: 'thread', name: '스레드', description: '현재 스레드의 정보를 사용합니다'},
        {id: 'knowledge-base', name: '지식 저장소', description: '등록된 지식 자료를 사용합니다'},
    ],
    'communication.allowed_message_types': [
        {id: 'message', name: '메시지', description: '일반 메시지'},
        {id: 'question', name: '질문', description: '정보를 확인하는 질문'},
        {id: 'answer', name: '답변', description: '질문에 대한 답변'},
        {id: 'request', name: '요청', description: '작업이나 도움 요청'},
        {id: 'feedback', name: '피드백', description: '결과에 대한 의견'},
        {id: 'review', name: '검토', description: '독립적인 검토 결과'},
        {id: 'decision', name: '결정', description: '판단 및 결정 전달'},
        {id: 'summary', name: '요약', description: '핵심 내용 정리'},
    ],
    'role.capabilities': [
        {id: 'task-execution', name: '작업 수행', description: '할당받은 작업을 수행합니다'},
        {id: 'review', name: '검토', description: '문서와 결과를 검토합니다'},
        {id: 'summarization', name: '요약', description: '자료와 대화의 핵심을 정리합니다'},
        {id: 'code-generation', name: '코드 작성', description: '코드를 작성합니다'},
    ],
};
function Checklist({field, value, disabled, change}: {field: string; value: string[]; disabled: boolean; change: (value: string[]) => void}) {
    const options = checklistOptions[field];
    const items = [...options, ...value.filter((id) => !options.some((option) => option.id === id)).map((id) => ({id, name: id, description: '기존 설정에 포함된 항목'}))];
    return <div id={field} role='group' aria-labelledby={`${field}-label`} className='agent-tool-options'>{items.map((item) => <label className='agent-tool-option' key={item.id}>
        <input type='checkbox' checked={value.includes(item.id)} disabled={disabled} onChange={(e) => change(e.target.checked ? [...value, item.id] : value.filter((id) => id !== item.id))}/>
        <span>{item.name}<small>{item.description}</small></span>
    </label>)}</div>;
}
function ToolPolicy({value, disabled, change}: {value: {enabled: boolean; allowed: string[]; denied: string[]; require_confirmation: string[]}; disabled: boolean; change: (path: string, value: any) => void}) {
    const ids = [...new Set([...toolOptions.map((item) => item.id), ...(value.allowed || []), ...(value.denied || []), ...(value.require_confirmation || [])])];
    const selectPolicy = (id: string, policy: string) => {
        const allowed = (value.allowed || []).filter((item) => item !== id);
        const denied = (value.denied || []).filter((item) => item !== id);
        const confirmation = (value.require_confirmation || []).filter((item) => item !== id);
        if (policy === 'deny') {denied.push(id);} else {allowed.push(id);}
        if (policy === 'confirm') {confirmation.push(id);}
        change('tools', {...value, allowed, denied, require_confirmation: confirmation});
    };
    return <div className='agent-tool-options'><p>도구마다 하나의 정책을 선택하세요. 승인 필요 도구는 승인을 받은 뒤 실행됩니다.</p>{ids.map((id) => {
        const item = toolOptions.find((option) => option.id === id);
        const policy = value.denied?.includes(id) || !value.allowed?.includes(id) ? 'deny' : value.require_confirmation?.includes(id) ? 'confirm' : 'allow';
        return <label key={id} className='agent-tool-option'><span>{item?.name || id}<small>{item?.description || '기존 설정에 포함된 도구'}</small></span><select aria-label={`${item?.name || id} 실행 정책`} disabled={disabled} value={policy} onChange={(e) => selectPolicy(id, e.target.value)}><option value='allow'>가능 · 자동 실행</option><option value='deny'>불가능</option><option value='confirm'>허가 필요</option></select></label>;
    })}</div>;
}
const requiredFields = new Set(['id', 'name', 'model.name', 'prompts.identity']);
const hints: Record<string, string> = {
    id: '예: methodology-reviewer (영문 소문자로 시작, 3–32자)',
    name: '예: Methodology Reviewer — Agent 이름',
    display_name: '예: 방법론 검토자 — 사용자에게 표시할 이름',
    description: 'Agent의 목적과 담당 업무를 설명해주세요',
    'avatar.emoji': '예: 🔬 — Agent를 나타내는 이모지',
    'avatar.url': '예: https://example.com/avatar.png — 이미지 주소',
    tags: '예: ["research", "review"] — 분류 태그',
    'role.title': '예: 연구 방법론 전문가 — 역할 제목',
    'role.specialties': '예: ["통계", "실험 설계"] — 전문 분야',
    'role.capabilities': '예: ["논문 검토"] — 수행 가능한 작업',
    'model.name': '사용할 모델 이름을 입력해주세요',
    'model.parameters.temperature': '응답의 다양성 정도를 숫자로 입력해주세요 (예: 0.2)',
    'model.parameters.max_tokens': '응답의 최대 토큰 수를 입력해주세요 (예: 8000)',
    'model.parameters.top_p': '확률 누적 기준을 입력해주세요 (예: 0.9, 선택 사항)',
    'model.fallback.models': '예: [{"provider":"anthropic","name":"모델 이름"}] — 대체 모델',
    'prompts.identity': 'Agent의 역할, 전문성, 관점과 기본 행동 원칙을 입력해주세요',
    'prompts.task_instruction': '작업을 수행할 때 따라야 할 기본 원칙을 입력해주세요',
    'prompts.reasoning_instruction': '근거와 불확실성을 판단하는 원칙을 입력해주세요',
    'prompts.collaboration_instruction': '다른 Agent와 협업하는 방식을 입력해주세요',
    'prompts.communication_instruction': '사용자 및 Agent와 소통하는 원칙을 입력해주세요',
    'prompts.output_instruction': '결과의 형식과 상세 수준을 입력해주세요',
    'tools.allowed': '예: ["web-search", "file-reader"] — 허용할 도구 ID',
    'tools.denied': '예: ["code-executor"] — 금지할 도구 ID',
    'tools.require_confirmation': '예: ["code-executor"] — 실행 전 승인이 필요한 도구 ID',
    'context.instructions': '제공되는 컨텍스트를 다루는 원칙을 입력해주세요',
    'context.sources': '예: ["project", "conversation", "thread"] — 컨텍스트 출처',
    'context.max_context_tokens': '컨텍스트의 최대 토큰 수를 입력해주세요 (선택 사항)',
    'collaboration.max_delegation_depth': '작업 위임의 최대 깊이를 입력해주세요 (예: 1)',
    'communication.default_message_type': '예: message — 기본 메시지 유형',
    'communication.allowed_message_types': '예: ["message", "question", "answer"] — 허용할 메시지 유형',
    'communication.mention_policy': '예: allowed — 멘션 허용 정책',
    'communication.reply_policy': '예: thread — 답변 위치 정책',
    'output.language': '예: auto, ko, en — 출력 언어',
    'output.schema': '예: {"type":"object"} — 출력 JSON Schema (선택 사항)',
    'behavior.max_retries': '실패 시 최대 재시도 횟수를 입력해주세요 (예: 2)',
    'messenger.profile.display_name': '메신저 프로필에 표시할 이름을 입력해주세요',
    'messenger.profile.avatar_url': '예: https://example.com/avatar.png — 프로필 이미지 주소',
    metadata: '예: {"project":"research"} — 추가 메타데이터',
};
function Fields({value, path = '', change, editing, parentDisabled = false}: {value: any; path?: string; change: (path: string, value: any) => void; editing: boolean; parentDisabled?: boolean}) {
    return <>{Object.entries(value).map(([key, val]) => {
        const field = path ? `${path}.${key}` : key;
        if (path === 'tools' && ['allowed', 'denied', 'require_confirmation'].includes(key)) {return key === 'allowed' ? <ToolPolicy key='tool-policy' value={value} disabled={parentDisabled || value.enabled === false} change={change}/> : null;}
        const groupDisabled = parentDisabled || (value.enabled === false && key !== 'enabled');
        if (val && typeof val === 'object' && !Array.isArray(val) && key !== 'metadata') {
            return <details key={field} open={['role', 'model', 'prompts', 'tools'].includes(field)}><summary>{key.replace(/_/g, ' ')}</summary><Fields value={val} path={field} change={change} editing={editing} parentDisabled={groupDisabled}/></details>;
        }
        const disabled = groupDisabled || readonly.has(field) || (field === 'id' && editing);
        const required = requiredFields.has(field);
        const placeholder = readonly.has(field) ? '연결 또는 저장 시 자동 설정됩니다' : hints[field] || `${key.replace(/_/g, ' ')} 값을 입력해주세요`;
        return <div className='agent-field' key={field}><label id={`${field}-label`} htmlFor={checklistOptions[field] ? undefined : field}><span>{key.replace(/_/g, ' ')}{required && <span className='agent-required' aria-label='필수'> *</span>}</span></label>
            {checklistOptions[field] ? <Checklist field={field} value={val as string[]} disabled={disabled} change={(next) => change(field, next)}/> : field === 'model.name' ? <select id={field} value={String(val)} required disabled={disabled} onChange={(e) => change(field, e.target.value)}><option value=''>모델을 선택해주세요</option>{!modelCatalog[value.provider]?.some((model) => model.id === val) && val !== '' && <option value={String(val)} disabled>{String(val)} (목록에 없음 — 다시 선택해주세요)</option>}{(modelCatalog[value.provider] || []).map((model) => <option key={model.id} value={model.id}>{model.label} · {model.id}</option>)}</select> : typeof val === 'boolean' ? <input id={field} type='checkbox' checked={val} disabled={disabled} onChange={(e) => change(field, e.target.checked)}/> : choices[field] ? <select id={field} value={String(val)} disabled={disabled} onChange={(e) => change(field, e.target.value)}>{choices[field].map((choice) => <option key={choice}>{choice}</option>)}</select> : Array.isArray(val) || key === 'metadata' || field === 'output.schema' ? <JsonField id={field} value={val} disabled={disabled} placeholder={placeholder} change={(next) => change(field, next)}/> : <textarea id={field} required={required} rows={field.startsWith('prompts.') || field === 'context.instructions' ? 4 : 1} value={val === null ? '' : String(val)} disabled={disabled} placeholder={placeholder} onChange={(e) => change(field, typeof val === 'number' || ['model.parameters.top_p', 'context.max_context_tokens'].includes(field) ? (e.target.value === '' ? null : Number(e.target.value)) : e.target.value)}/>}
        </div>;
    })}</>;
}
function JsonField({id, value, disabled, placeholder, change}: {id: string; value: any; disabled: boolean; placeholder: string; change: (value: any) => void}) {
    const empty = value === null || (Array.isArray(value) ? value.length === 0 : typeof value === 'object' && Object.keys(value).length === 0);
    const [text, setText] = useState(empty ? '' : JSON.stringify(value));
    return <textarea id={id} value={text} disabled={disabled} placeholder={placeholder} rows={2} onChange={(e) => {setText(e.target.value); try {const next = e.target.value.trim() === '' ? (Array.isArray(value) ? [] : value === null ? null : {}) : JSON.parse(e.target.value); if (Array.isArray(value) ? !Array.isArray(next) : next !== null && (typeof next !== 'object' || Array.isArray(next))) {throw new Error();} e.target.setCustomValidity(''); change(next);} catch {e.target.setCustomValidity('올바른 JSON 형식으로 입력해주세요.');}}}/>;
}
function App({store}: {store: Store}) {
    const [open, setOpen] = useState(false);
    const [agents, setAgents] = useState<Agent[]>([]);
    const [draft, setDraft] = useState<Agent | null>(null);
    const [editing, setEditing] = useState(false);
    const [deleting, setDeleting] = useState<Agent | null>(null);
    const [preserve, setPreserve] = useState(false);
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    const refresh = async () => {const latest = await listAgents(); setAgents(latest); setDeleting((current) => current ? latest.find((item) => item.id === current.id) || null : null);};
    const run = async (operation: () => Promise<unknown>, done?: () => void) => {setBusy(true); setError(''); try {await operation(); done?.(); await refresh();} catch (err) {setError(err instanceof Error ? err.message : '요청에 실패했습니다.'); try {await refresh();} catch {/* Keep the original operation error. */}} finally {setBusy(false);}};
    const [, redraw] = useState(0);
    const closeButton = useRef<HTMLButtonElement>(null);
    const state = store.getState();
    const user = state.entities?.users?.profiles?.[state.entities?.users?.currentUserId];
    const admin = (user?.roles || '').split(' ').includes('system_admin');
    useEffect(() => store.subscribe(() => redraw((n) => n + 1)), [store]);
    useEffect(() => {
        const listener = (event: Event) => {setOpen(true); setError(''); setDeleting(null); setDraft((event as CustomEvent).detail === 'create' ? freshAgent() : null); setEditing(false); void run(async () => {});};
        window.addEventListener('agent-ui-open', listener);
        return () => window.removeEventListener('agent-ui-open', listener);
    }, []);
    useEffect(() => {if (open) {closeButton.current?.focus();}}, [open]);
    if (!open) {return null;}
    const close = () => {setOpen(false); setDraft(null); setDeleting(null);};
    const change = (path: string, value: any) => setDraft((previous) => {const next = JSON.parse(JSON.stringify(previous)); const keys = path.split('.'); let obj = next; keys.slice(0, -1).forEach((key) => {obj = obj[key];}); obj[keys[keys.length - 1]] = value; if (path === 'model.provider') {next.model.name = '';} return next;});
    return <div className='agent-ui-backdrop' onKeyDown={(e) => {
        if (e.key === 'Escape') {close();}
        if (e.key === 'Tab') {const elements = Array.from(e.currentTarget.querySelectorAll<HTMLElement>('button, input, select, textarea')).filter((el) => !el.hasAttribute('disabled') && el.getClientRects().length); const first = elements[0]; const last = elements[elements.length - 1]; if (e.shiftKey && document.activeElement === first) {e.preventDefault(); last?.focus();} else if (!e.shiftKey && document.activeElement === last) {e.preventDefault(); first?.focus();}}
    }}><section aria-busy={busy} className='agent-ui' role='dialog' aria-modal='true' aria-labelledby='agent-title'>
        <header><h2 id='agent-title'>{deleting ? 'Agent 삭제' : draft ? editing ? 'Agent 설정' : 'Agent 생성' : `AI Agents · ${agents.length}`}</h2><button ref={closeButton} onClick={close} aria-label='닫기'>×</button></header>
        <p className='agent-notice'>Agent 설정은 Mattermost Plugin KV Store에 저장됩니다.</p>
        {!admin && <p>일반 사용자는 Agent 목록만 조회할 수 있습니다.</p>}
        {busy && <p role='status'>불러오는 중…</p>}
        {!busy && <button onClick={() => {setDraft(null); setDeleting(null); void run(async () => {});}}>최신 목록 불러오기</button>}
        {error && <p role='alert' className='agent-error'>{error}</p>}
        {deleting && admin ? <div><p><strong>{deleting.display_name || deleting.name}</strong>을 삭제하시겠습니까?</p><p>Agent ID: {deleting.id}</p><p>연결된 Mattermost Bot 계정도 영구 삭제됩니다.</p><label><input type='checkbox' checked={preserve} onChange={(e) => setPreserve(e.target.checked)}/>Long-term Memory 보존</label><p>Memory 기능은 아직 구현되지 않았으며 현재 이 옵션은 실제 데이터 처리에 영향을 주지 않습니다.</p><footer><button onClick={() => setDeleting(null)}>취소</button><button disabled={busy} className='danger' onClick={() => {if (!admin) {return;} void run(() => deleteAgent(deleting.id, deleting.lifecycle.version), () => setDeleting(null));}}>삭제</button></footer></div> : draft && admin ? <form onSubmit={(e) => {
            e.preventDefault(); if (busy || !admin) {return;}
            if (!/^[a-z][a-z0-9._-]{2,31}$/.test(draft.id)) {setError('Agent ID는 영문 소문자로 시작하는 3–32자의 영문 소문자, 숫자, 점, 밑줄, 하이픈으로 입력해주세요.'); return;}
            if (!draft.name.trim() || !draft.model.name.trim() || !draft.prompts.identity.trim()) {setError('이름, 모델 이름, Identity Prompt를 입력해주세요.'); return;}
            if (!modelCatalog[draft.model.provider]?.some((model) => model.id === draft.model.name)) {setError('선택한 Provider의 모델 목록에서 모델을 선택해주세요.'); return;}
            if (!editing && agents.some((agent) => agent.id === draft.id)) {setError('이미 사용 중인 Agent ID입니다.'); return;}
            void run(() => editing ? updateAgent(draft) : createAgent(draft), () => setDraft(null));
        }}><p>*는 필수 입력 항목입니다. 기본 정보를 입력하세요. 상세 정책은 아래 펼침 영역에서 수정할 수 있습니다. 도구는 각각 가능·불가능·허가 필요 중 하나를 선택하세요. Web Search·Debug Echo·File Reader를 실행할 수 있으며, File Reader는 파일 읽기 권한도 필요합니다. 허가 필요 도구는 요청자 또는 시스템 관리자의 승인 후 실행됩니다. 컨텍스트 출처·메시지 유형·Capabilities는 체크리스트에서 선택할 수 있습니다. 준비 중인 도구와 Capabilities는 향후 구현됩니다. 나머지 목록과 객체 항목은 JSON 형식입니다.</p><Fields value={draft} change={change} editing={editing}/><footer><button type='button' onClick={() => {setDraft(null); setError('');}}>취소</button><button disabled={busy} className='primary' type='submit'>{editing ? '저장' : 'Agent 생성'}</button></footer></form> : <div>
            {!agents.length && <div className='agent-empty'><h3>등록된 Agent가 없습니다</h3><p>역할과 모델, 프롬프트를 설정해 첫 Agent를 만들어보세요.</p></div>}
            {agents.map((agent) => <article key={agent.id}><h3>{agent.display_name || agent.name}</h3><p>{agent.messenger.username ? `@${agent.messenger.username}` : 'Bot 연결 대기'} · {agent.description}</p><dl><dt>역할</dt><dd>{agent.role.type}</dd><dt>모델</dt><dd>{agent.model.provider} / {agent.model.name}</dd><dt>도구</dt><dd>{agent.tools.allowed?.length || 0}</dd><dt>상태</dt><dd>{agent.runtime?.status || (agent.lifecycle.enabled ? 'PROVISIONING' : 'DISABLED')}{agent.runtime?.error && <p role='alert'>{agent.runtime.error}</p>}</dd></dl>{admin && <div className='agent-actions'>{(!agent.messenger.user_id || ['ERROR', 'PROVISIONING', 'DELETING'].includes(agent.runtime?.status || '')) && <button disabled={busy} onClick={() => {void run(() => (agent.lifecycle.enabled ? enableAgent : disableAgent)(agent.id, agent.lifecycle.version));}}>Bot 연결 재시도</button>}<button disabled={busy} onClick={() => {const next = JSON.parse(JSON.stringify(agent)); next.tools.allowed = (next.tools.allowed || []).filter((id: string) => !(next.tools.denied || []).includes(id)); next.tools.require_confirmation = (next.tools.require_confirmation || []).filter((id: string) => next.tools.allowed.includes(id)); setDraft(next); setEditing(true); setError('');}}>설정</button><button disabled={busy} onClick={() => {void run(() => (agent.lifecycle.enabled ? disableAgent : enableAgent)(agent.id, agent.lifecycle.version));}}>{agent.lifecycle.enabled ? '비활성화' : '활성화'}</button><button disabled={busy} className='danger' onClick={() => {setDeleting(agent); setPreserve(false);}}>삭제</button></div>}</article>)}
            {admin && <footer><button disabled={busy} className='primary' onClick={() => {setDraft(freshAgent()); setEditing(false); setError('');}}>＋ Agent 생성</button></footer>}
        </div>}
    </section></div>;
}
class Plugin {
    initialize(registry: Registry, store: Store) {
        registry.registerRootComponent(() => <App store={store}/>);
        registry.registerSlashCommandWillBePostedHook((message, args) => {
            const command = message.trim();
            if (!/^\/agent(?:\s|$)/.test(command)) {return {message, args};}
            if (command === '/agent create' || command === '/agent list') {window.dispatchEvent(new CustomEvent('agent-ui-open', {detail: command.endsWith('create') ? 'create' : 'list'})); return {message: '', args};}
            return {message, args};
        });
    }
}
declare global {interface Window {registerPlugin: (id: string, plugin: Plugin) => void}}
window.registerPlugin(pluginId, new Plugin());
