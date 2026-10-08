import json
import httpx
import pytest
from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver
from agent_runtime.graph import build_graph, invoke_turn, resume_turn
from agent_runtime.schema import GenerateRequest, ResumeRequest
from agent_runtime.tools.file_reader import read_file, file_context, FileContext
from agent_runtime.context.budget import ContextBudget
from langchain_core.utils.function_calling import convert_to_openai_tool
from test_runtime import payload
from test_tools import LoopModel

FILE_ID = 'f' * 26


def request(**policy):
    return GenerateRequest(**{**payload(), 'tools': {'enabled': True, 'allowed': ['file-reader'], **policy},
        'permissions': {'files': {'read': True, 'write': False}}, 'requester_user_id': 'u'*26,
        'channel_id': 'c'*26, 'attachments': [{'file_id': FILE_ID, 'name': 'project.md', 'mime_type': 'text/markdown', 'size': 120}]})


@pytest.fixture
def upstream(monkeypatch):
    requests = []
    result = {'file_id': FILE_ID, 'name': 'project.md', 'mime_type': 'text/markdown', 'size': 120,
              'content': 'Private file: Go, LangGraph, Mattermost KV, Gemini', 'truncated': False}
    status = [200]
    original = httpx.AsyncClient
    def handler(req):
        assert req.url == 'http://bridge/plugins/com.seyeon.agentbridge/api/internal/files/read'
        assert req.headers['Authorization'] == 'Bearer test-runtime-secret'
        body = json.loads(req.content)
        requests.append(body)
        response = dict(result)
        if status[0] == 200:
            data = response['content'].encode()
            limit = body['max_content_bytes']
            response['content'] = data[:limit].decode('utf-8', errors='ignore')
            response['truncated'] = len(data) > limit
        return httpx.Response(status[0], json=response)
    monkeypatch.setenv('AGENT_BRIDGE_URL', 'http://bridge/plugins/com.seyeon.agentbridge')
    monkeypatch.setenv('AGENT_RUNTIME_TOKEN', 'test-runtime-secret')
    monkeypatch.setattr(httpx, 'AsyncClient', lambda **kwargs: original(transport=httpx.MockTransport(handler), **kwargs))
    return requests, result, status


async def test_file_loop_metadata_only_and_safe_logging(upstream, caplog):
    caplog.set_level('INFO', logger='agent_runtime.tools')
    model = LoopModel('read_file', {'file_id': FILE_ID})
    req = request()
    result = await invoke_turn(build_graph(lambda _: model), req)
    assert result['response']['text'] == 'final answer'
    assert 'project.md' in model.calls[0][0].content
    assert 'Private file:' not in model.calls[0][0].content
    assert json.loads(model.calls[-1][-1].content)['content'] == upstream[1]['content']
    assert upstream[0][0]['requester_user_id'] == req.requester_user_id
    assert 'Private file:' not in caplog.text and 'test-runtime-secret' not in caplog.text
    assert 'file_size=120' in caplog.text and 'file_name="project.md"' in caplog.text


@pytest.mark.parametrize('policy,permission,error', [({'enabled': False},True,'TOOL_NOT_ALLOWED'),
    ({'allowed': []},True,'TOOL_NOT_ALLOWED'), ({'denied': ['file-reader']},True,'TOOL_NOT_ALLOWED'),
    ({},False,'FILE_PERMISSION_DENIED')])
async def test_policy_and_permission_prevent_access(upstream, policy, permission, error):
    req = request(**policy)
    req.permissions.files.read = permission
    model = LoopModel('read_file', {'file_id': FILE_ID})
    await invoke_turn(build_graph(lambda _: model), req)
    assert not upstream[0]
    assert json.loads(model.calls[-1][-1].content)['code'] == error
    if not permission:
        assert 'read_file' not in ','.join(model.bound[0]) if model.bound else True


@pytest.mark.parametrize('args', [{'path':'/etc/passwd'}, {'file_id':'https://example.com/file'},
    {'file_id':'../.env'}, {'file_id':FILE_ID, 'path':'/app/.env'}, {'file_id':'x'*26}])
async def test_paths_urls_and_unrelated_ids_cannot_access(upstream,args):
    model = LoopModel('read_file',args)
    await invoke_turn(build_graph(lambda _:model),request())
    assert not upstream[0]
    assert model.calls[-1][-1].status == 'error'


@pytest.mark.parametrize('code,status', [('FILE_TOO_LARGE',413), ('FILE_TYPE_NOT_SUPPORTED',415),
    ('FILE_PERMISSION_DENIED',403), ('FILE_NOT_FOUND',404), ('FILE_NOT_ATTACHED',403)])
async def test_safe_upstream_errors(upstream,code,status):
    upstream[1].clear()
    upstream[1]['error'] = {'code':code, 'message':'upstream secret or stack trace'}
    upstream[2][0] = status
    model = LoopModel('read_file',{'file_id':FILE_ID})
    await invoke_turn(build_graph(lambda _:model),request())
    assert json.loads(model.calls[-1][-1].content)['code'] == code
    assert 'upstream secret' not in model.calls[-1][-1].content


@pytest.mark.parametrize('decision,read,status', [('approve',True,'EXECUTED'),('reject',False,'REJECTED')])
async def test_restart_resume_original_file_and_latest_permission(upstream,tmp_path,decision,read,status):
    req = request(require_confirmation=['file-reader'])
    model = LoopModel('read_file',{'file_id':FILE_ID})
    path = str(tmp_path/'checkpoint.db')
    async with AsyncSqliteSaver.from_conn_string(path) as saver:
        pending = (await invoke_turn(build_graph(lambda _:model,saver),req))['response']['interrupt']
        assert not upstream[0]
    async with AsyncSqliteSaver.from_conn_string(path) as saver:
        result = await resume_turn(build_graph(lambda _:model,saver),ResumeRequest(agent_id=req.agent_id,
            root_post_id=req.root_post_id,approval_id=pending['approval_id'],decision=decision,tools=req.tools,permissions=req.permissions))
        assert result['response']['approval_status'] == status
    assert bool(upstream[0]) == read
    if read:
        assert upstream[0][0]['file_id'] == FILE_ID
        assert upstream[0][0]['approval_id'] == pending['approval_id']


async def test_permission_revoked_during_approval(upstream,tmp_path):
    req = request(require_confirmation=['file-reader'])
    model = LoopModel('read_file',{'file_id':FILE_ID})
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path/'db')) as saver:
        graph = build_graph(lambda _:model,saver)
        pending = (await invoke_turn(graph,req))['response']['interrupt']
        req.permissions.files.read = False
        await resume_turn(graph,ResumeRequest(agent_id=req.agent_id,root_post_id=req.root_post_id,
            approval_id=pending['approval_id'],decision='approve',tools=req.tools,permissions=req.permissions))
        assert not upstream[0]
        assert json.loads(model.calls[-1][-1].content)['code'] == 'FILE_PERMISSION_DENIED'


@pytest.mark.parametrize('text', ['큰 파일 내용 '*10000, '\\"\n\t'*10000])
async def test_large_file_result_fits_context_with_truncation(upstream,text):
    req = request()
    req.max_context_tokens = 5000
    upstream[1]['content'] = text
    upstream[1]['size'] = len(text.encode())
    model = LoopModel('read_file',{'file_id':FILE_ID})
    result = await invoke_turn(build_graph(lambda _:model),req)
    assert result['response']['text'] == 'final answer'
    data = json.loads(model.calls[-1][-1].content)
    assert data['truncated'] is True and data['content']
    budget = ContextBudget(5000)
    tools = budget.estimator.count(json.dumps([convert_to_openai_tool(read_file)],ensure_ascii=False))
    assert budget.count_messages(model.calls[-1]) + tools <= budget.input_limit


async def test_parallel_files_preserve_pairs_and_context(upstream):
    from langchain_core.messages import AIMessage, ToolMessage
    class Batch(LoopModel):
        async def ainvoke(self, messages):
            self.calls.append(messages)
            if isinstance(messages[-1], ToolMessage):
                assert [m.tool_call_id for m in messages[-2:]] == ['one', 'two']
                assert all(json.loads(m.content)['truncated'] for m in messages[-2:])
                return AIMessage(content='done')
            return AIMessage(content='',tool_calls=[{'name':'read_file','args':{'file_id':FILE_ID},'id':i} for i in ['one','two']])
    req = request()
    req.max_context_tokens = 10000
    upstream[1]['content'] = '\n"큰 내용"'*10000
    model = Batch()
    result = await invoke_turn(build_graph(lambda _:model),req)
    assert result['response']['text'] == 'done'
    assert len(upstream[0]) == 2
    budget = ContextBudget(10000)
    tools = budget.estimator.count(json.dumps([convert_to_openai_tool(read_file)],ensure_ascii=False))
    assert budget.count_messages(model.calls[-1]) + tools <= budget.input_limit


async def test_upstream_cannot_add_credentials_to_result(upstream):
    upstream[1]['token'] = 'do-not-expose-this'
    model = LoopModel('read_file', {'file_id':FILE_ID})
    await invoke_turn(build_graph(lambda _:model),request())
    assert 'do-not-expose-this' not in model.calls[-1][-1].content
