import json
import pytest
from langchain_core.messages import AIMessage, ToolMessage
from langchain_core.tools import tool
from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver
from agent_runtime.graph import build_graph, invoke_turn, model_context
from agent_runtime.schema import GenerateRequest, ToolConfig
from agent_runtime.tools.registry import resolve_tools, ToolSpec, TOOL_REGISTRY
from test_runtime import payload


def request(**policy):
    data = payload()
    data['tools'] = {'enabled': True, 'allowed': ['debug-echo'], **policy}
    return GenerateRequest(**data)


@pytest.mark.parametrize('policy,expected', [({}, ['debug_echo']), ({'enabled': False}, []),
    ({'denied': ['debug-echo']}, []), ({'require_confirmation': ['debug-echo']}, []),
    ({'allowed': ['unknown']}, [])])
def test_resolution(policy, expected):
    assert [s.function_name for s in resolve_tools(request(**policy).tools)] == expected


class LoopModel:
    def __init__(self, name='debug_echo', args=None):
        self.bound = []
        self.calls = []
        self.name = name
        self.args = {'text': 'private-echo-value'} if args is None else args

    def bind_tools(self, tools):
        self.bound.append([t.name for t in tools])
        return self

    async def ainvoke(self, messages):
        self.calls.append(messages)
        if isinstance(messages[-1], ToolMessage):
            return AIMessage(content='final answer', usage_metadata={'input_tokens': 2, 'output_tokens': 1, 'total_tokens': 3})
        return AIMessage(content='', tool_calls=[{'name': self.name, 'args': self.args, 'id': 'call-1'}],
                         additional_kwargs={'signature': 'preserve-provider-metadata'},
                         usage_metadata={'input_tokens': 3, 'output_tokens': 2, 'total_tokens': 5})


@pytest.mark.parametrize('provider', ['google', 'openai', 'anthropic', 'ollama'])
async def test_provider_independent_loop_and_checkpoint(tmp_path, provider, caplog):
    caplog.set_level('INFO', logger='agent_runtime.tools')
    model = LoopModel()
    req = request()
    req.provider = provider
    path = str(tmp_path/'db')
    async with AsyncSqliteSaver.from_conn_string(path) as saver:
        graph = build_graph(lambda _: model, saver)
        result = await invoke_turn(graph, req)
        assert result['response'] == {'text': 'final answer', 'input_tokens': 5, 'output_tokens': 3}
        assert model.bound == [['debug_echo'], ['debug_echo']]
        assert model.calls[-1][-1].content == 'private-echo-value'
        assert model.calls[-1][-2].additional_kwargs['signature'] == 'preserve-provider-metadata'
        assert 'private-echo-value' not in caplog.text
        assert 'duration_ms=' in caplog.text
    async with AsyncSqliteSaver.from_conn_string(path) as saver:
        graph = build_graph(lambda _: model, saver)
        cached = await invoke_turn(graph, req)
        assert cached['response'] == result['response']
        assert len(model.calls) == 2
        req.post_id = 'n'*26
        await invoke_turn(graph, req)
        assert any(isinstance(m, ToolMessage) for m in model.calls[2])


@pytest.mark.parametrize('policy,name', [({'enabled': False}, 'debug_echo'),
    ({'denied': ['debug-echo']}, 'debug_echo'),
    ({'require_confirmation': ['debug-echo']}, 'debug_echo'),
    ({'allowed': []}, 'debug_echo'), ({}, 'shell')])
async def test_forged_calls_cannot_execute(policy, name):
    executed = []
    @tool
    def debug_echo(text: str) -> str:
        """Echo text."""
        executed.append(text)
        return text
    registry = {'debug-echo': ToolSpec('debug-echo', 'debug_echo', debug_echo, 'Echo')}
    model = LoopModel(name)
    result = await invoke_turn(build_graph(lambda _: model, registry=registry), request(**policy))
    assert result['response']['text'] == 'final answer'
    assert not executed
    assert json.loads(model.calls[-1][-1].content)['error'] == 'TOOL_NOT_ALLOWED'


async def test_errors_and_invalid_arguments_are_tool_results(caplog):
    @tool
    def failing(text: str) -> str:
        """Fail safely."""
        raise RuntimeError('secret credential')
    registry = {'debug-echo': ToolSpec('debug-echo', 'failing', failing, 'Fail')}
    for args in [{'text': 'hello'}, {'missing': 'argument'}]:
        model = LoopModel('failing', args)
        result = await invoke_turn(build_graph(lambda _: model, registry=registry), request())
        assert result['response']['text'] == 'final answer'
        assert json.loads(model.calls[-1][-1].content)['error'] == 'TOOL_EXECUTION_FAILED'
    assert 'secret credential' not in caplog.text


async def test_budget_bounds_execution_and_resets_next_turn(tmp_path):
    executed = []
    @tool
    def counting(text: str) -> str:
        """Count executions."""
        executed.append(text)
        return 'ok'
    class Endless(LoopModel):
        async def ainvoke(self, messages):
            return AIMessage(content='', tool_calls=[{'name': 'counting', 'args': {'text': 'x'}, 'id': f'call-{len(executed)}'}])
    model = Endless()
    registry = {'debug-echo': ToolSpec('debug-echo', 'counting', counting, 'Count')}
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path/'db')) as saver:
        graph = build_graph(lambda _: model, saver, registry=registry)
        req = request()
        result = await invoke_turn(graph, req)
        assert len(executed) == 10
        assert '한도' in result['response']['text']
        req.post_id = 'n'*26
        await invoke_turn(graph, req)
        assert len(executed) == 20


async def test_web_search_http_result_is_bounded(monkeypatch):
    import httpx
    from agent_runtime.tools.web_search import web_search
    original = httpx.AsyncClient
    def handler(req):
        assert req.url.host == 'api.search.brave.com'
        assert req.headers['X-Subscription-Token'] == 'test-key'
        return httpx.Response(200, json={'web': {'results': [{'title': 'Source', 'url': 'https://example.com', 'description': 'snippet'}]}})
    monkeypatch.setenv('BRAVE_SEARCH_API_KEY', 'test-key')
    monkeypatch.setattr(httpx, 'AsyncClient', lambda **kwargs: original(transport=httpx.MockTransport(handler), **kwargs))
    result = json.loads(await web_search.ainvoke({'query': 'current news'}))
    assert result['results'][0]['title'] == 'Source'
    monkeypatch.delenv('BRAVE_SEARCH_API_KEY')
    with pytest.raises(RuntimeError, match='unavailable'):
        await web_search.ainvoke({'query': 'news'})


async def test_multiple_calls_and_denied_priority():
    class Batch(LoopModel):
        async def ainvoke(self, messages):
            self.calls.append(messages)
            if isinstance(messages[-1], ToolMessage):
                assert [m.tool_call_id for m in messages[-2:]] == ['one', 'two']
                assert messages[-2].content == 'ok'
                assert 'TOOL_NOT_ALLOWED' in messages[-1].content
                return AIMessage(content='done')
            return AIMessage(content='', tool_calls=[
                {'name': 'debug_echo', 'args': {'text': 'ok'}, 'id': 'one'},
                {'name': 'web_search', 'args': {'query': 'blocked'}, 'id': 'two'}])
    model = Batch()
    result = await invoke_turn(build_graph(lambda _: model), request(allowed=['debug-echo', 'web-search'], denied=['web-search']))
    assert result['response']['text'] == 'done'


async def test_retry_after_model_failure_removes_incomplete_tool_exchange(tmp_path):
    model = LoopModel()
    calls = 0
    original = model.ainvoke
    async def fail_once(messages):
        nonlocal calls
        calls += 1
        if calls == 2:
            raise RuntimeError('provider failed after tool')
        return await original(messages)
    model.ainvoke = fail_once
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path/'db')) as saver:
        graph = build_graph(lambda _: model, saver)
        req = request()
        with pytest.raises(RuntimeError):
            await invoke_turn(graph, req)
        result = await invoke_turn(graph, req)
        assert result['response']['text'] == 'final answer'
        state = await graph.aget_state({'configurable': {'thread_id': req.thread_id}})
        assert len(state.values['messages']) == 4


async def test_tool_cancellation_propagates():
    import asyncio
    cancelled = []
    started = asyncio.Event()
    @tool
    async def slow(text: str) -> str:
        """Wait for cancellation."""
        started.set()
        try:
            await asyncio.sleep(30)
        finally:
            cancelled.append(True)
        return text
    registry = {'debug-echo': ToolSpec('debug-echo', 'slow', slow, 'Slow')}
    task = asyncio.create_task(invoke_turn(build_graph(lambda _: LoopModel('slow'), registry=registry), request()))
    await asyncio.wait_for(started.wait(), timeout=1)
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task
    assert cancelled == [True]


async def test_large_tool_results_end_with_controlled_answer():
    @tool
    def large(text: str) -> str:
        """Return a large result."""
        return 'x' * (24 * 1024)
    class Model(LoopModel):
        async def ainvoke(self, messages):
            return AIMessage(content='', tool_calls=[{'name': 'large', 'args': {'text': 'x'}, 'id': str(len(messages))}])
    registry = {'debug-echo': ToolSpec('debug-echo', 'large', large, 'Large')}
    result = await invoke_turn(build_graph(lambda _: Model(), registry=registry), request())
    assert '입력 한도' in result['response']['text']


def test_go_null_slices_are_empty_and_safe():
    config = ToolConfig(enabled=True, allowed=None, denied=None, require_confirmation=None)
    assert resolve_tools(config) == []


async def test_oversized_parallel_batch_stops_without_execution():
    class Batch(LoopModel):
        async def ainvoke(self, messages):
            return AIMessage(content='', tool_calls=[{'name': 'debug_echo', 'args': {'text': 'x'}, 'id': str(n)} for n in range(11)])
    result = await invoke_turn(build_graph(lambda _: Batch()), request())
    assert '한도' in result['response']['text']
