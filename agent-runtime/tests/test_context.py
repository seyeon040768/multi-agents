import pytest
from langchain_core.messages import AIMessage, HumanMessage, ToolMessage
from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver
from agent_runtime.context.budget import ContextBudget
from agent_runtime.context.estimator import ConservativeEstimator
from agent_runtime.context.turns import get_complete_turns
from agent_runtime.graph import build_graph, invoke_turn, resume_turn, ApprovalPending
from agent_runtime.schema import GenerateRequest
from test_memory import turn
from test_approval import resume


class SummaryModel:
    def __init__(self):
        self.calls = []
        self.summaries = []
        self.fail_summary = False

    def bind_tools(self, tools):
        return self

    async def ainvoke(self, messages):
        if messages[0].content.startswith('Summarize the conversation'):
            self.summaries.append(messages)
            if self.fail_summary:
                raise RuntimeError('summary unavailable')
            return AIMessage(content='Facts and decisions retained.')
        self.calls.append(messages)
        return AIMessage(content='answer ' + 'a' * 250)


def req(n, size=250, budget=3000):
    data = turn(f'fact-{n} ' + 'x' * size)
    data['post_id'] = f'{n:026d}'
    data['max_context_tokens'] = budget
    return GenerateRequest(**data)


async def test_short_then_long_incremental_restart_and_budget(tmp_path):
    model = SummaryModel()
    path = str(tmp_path / 'db')
    async with AsyncSqliteSaver.from_conn_string(path) as saver:
        graph = build_graph(lambda _: model, saver)
        await invoke_turn(graph, req(0))
        assert not model.summaries
        for n in range(1, 10):
            await invoke_turn(graph, req(n))
        state = await graph.aget_state({'configurable': {'thread_id': req(9).thread_id}})
        assert state.values['summary'] == 'Facts and decisions retained.'
        cursor = state.values['summarized_until']
        assert len(state.values['messages']) == 20  # Full history remains durable.
        assert model.calls[-1][-1].id == req(9).post_id
        assert any(m.id == req(8).post_id for m in model.calls[-1])
        summary_count = len(model.summaries)
    async with AsyncSqliteSaver.from_conn_string(path) as saver:
        graph = build_graph(lambda _: model, saver)
        await invoke_turn(graph, req(10))
        assert 'Conversation Summary' in model.calls[-1][0].content
        assert 'Facts and decisions retained.' in model.calls[-1][0].content
        state = await graph.aget_state({'configurable': {'thread_id': req(10).thread_id}})
        if state.values['summarized_until'] != cursor:
            assert 'Previous summary:\nFacts and decisions retained.' in model.summaries[-1][1].content
        # Each historical message appears in exactly one summarizer input.
        all_inputs = ''.join(call[1].content for call in model.summaries)
        for n in range(6):
            assert all_inputs.count(f'fact-{n} ') == 1
        before = len(model.calls) + len(model.summaries)
        await invoke_turn(graph, req(10))
        assert len(model.calls) + len(model.summaries) == before
    budget = ContextBudget(3000)
    for call in model.calls + model.summaries:
        assert budget.count_messages(call) <= budget.input_limit
    assert summary_count > 0


def test_complete_tool_turn_and_incomplete_history():
    history = [HumanMessage(content='question', id='a'),
               AIMessage(content='', tool_calls=[{'id':'c', 'name':'echo', 'args':{}}]),
               ToolMessage(content='result', tool_call_id='c'),
               AIMessage(content='answer', id='reply:a'), HumanMessage(content='next', id='b')]
    turns = get_complete_turns(history, 'b')
    assert len(turns[0]) == 4
    budget = ContextBudget(700)
    selected = budget.select('identity', None, turns)
    assert len(selected) in (2, 6)  # Never retain an orphan result.
    assert get_complete_turns(history[:2] + history[-1:], 'b') == [history[-1:]]


async def test_summary_failure_does_not_advance_cursor(tmp_path):
    model = SummaryModel()
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path/'db')) as saver:
        graph = build_graph(lambda _: model, saver)
        for n in range(3):
            await invoke_turn(graph, req(n))
        model.fail_summary = True
        with pytest.raises(RuntimeError):
            await invoke_turn(graph, req(3))
        state = await graph.aget_state({'configurable': {'thread_id': req(3).thread_id}})
        assert not state.values.get('summarized_until')
        model.fail_summary = False
        await invoke_turn(graph, req(3))
        assert 'Conversation Summary' in model.calls[-1][0].content


async def test_oversized_current_message_never_sent():
    model = SummaryModel()
    result = await invoke_turn(build_graph(lambda _: model), req(0, size=4000))
    assert '입력 한도' in result['response']['text']
    assert not model.calls


async def test_pending_approval_preserves_summary_and_resume(tmp_path):
    from test_tools import request
    class Protected(SummaryModel):
        async def ainvoke(self, messages):
            if messages[-1].content == 'protected':
                return AIMessage(content='', tool_calls=[{'name':'debug_echo','args':{'text':'ok'},'id':'call'}])
            if isinstance(messages[-1], ToolMessage):
                self.calls.append(messages)
                return AIMessage(content='resumed')
            return await super().ainvoke(messages)
    model = Protected()
    path = str(tmp_path/'db')
    async with AsyncSqliteSaver.from_conn_string(path) as saver:
        graph = build_graph(lambda _: model, saver)
        for n in range(8):
            await invoke_turn(graph, req(n, budget=4000))
        current = req(8, budget=4000)
        current.messages[-1].content = 'protected'
        current.tools = request(require_confirmation=['debug-echo']).tools
        approval = (await invoke_turn(graph, current))['response']['interrupt']
        config = {'configurable': {'thread_id': current.thread_id}}
        before = (await graph.aget_state(config)).values
        summary_calls = len(model.summaries)
        with pytest.raises(ApprovalPending):
            await invoke_turn(graph, req(9, budget=4000))
    async with AsyncSqliteSaver.from_conn_string(path) as saver:
        graph = build_graph(lambda _: model, saver)
        result = await resume_turn(graph, resume(current, approval, 'reject'))
        after = (await graph.aget_state(config)).values
        assert result['response']['text'] == 'resumed'
        assert after['summary'] == before['summary']
        assert after['summarized_until'] == before['summarized_until']
        assert len(model.summaries) == summary_calls
        assert isinstance(model.calls[-1][-1], ToolMessage)


async def test_overlong_summary_output_is_bounded(tmp_path):
    class HugeSummary(SummaryModel):
        async def ainvoke(self, messages):
            if messages[0].content.startswith('Summarize'):
                return AIMessage(content='요약' * 1000)
            return await super().ainvoke(messages)
    model = HugeSummary()
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path/'db')) as saver:
        graph = build_graph(lambda _: model, saver)
        for n in range(6):
            await invoke_turn(graph, req(n))
        state = await graph.aget_state({'configurable': {'thread_id': req(5).thread_id}})
        assert ConservativeEstimator().count(state.values['summary']) <= 600
        assert all(ContextBudget(3000).count_messages(c) <= 2700 for c in model.calls)


async def test_budget_reduction_recompresses_existing_summary_without_new_old_turns(tmp_path):
    from langchain_core.messages import HumanMessage
    model = SummaryModel()
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path/'db')) as saver:
        graph = build_graph(lambda _: model, saver)
        config = {'configurable': {'thread_id': req(0).thread_id}}
        await graph.aupdate_state(config, {
            'messages': [HumanMessage(content='previous', id='old'), AIMessage(content='done', id='reply:old')],
            'summary': 'historical facts ' * 200, 'summarized_until': 'reply:old'}, as_node='generate')
        await invoke_turn(graph, req(0, size=30, budget=2000))
        state = (await graph.aget_state(config)).values
        assert model.summaries
        assert state['summarized_until'] == 'reply:old'
        assert len(state['summary'].encode()) <= 400
        assert 'historical facts' in ''.join(c[1].content for c in model.summaries)
        assert ContextBudget(2000).count_messages(model.calls[-1]) <= 1800


async def test_large_history_summary_inputs_are_chunked_and_bounded(tmp_path):
    model = SummaryModel()
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path/'db')) as saver:
        graph = build_graph(lambda _: model, saver)
        config = {'configurable': {'thread_id': req(0).thread_id}}
        history = []
        for n in range(10):
            history += [HumanMessage(content='large fact ' + 'z' * 1800, id=f'old-{n}'),
                        AIMessage(content='done', id=f'reply:old-{n}')]
        await graph.aupdate_state(config, {'messages': history}, as_node='generate')
        await invoke_turn(graph, req(0, size=10))
        assert len(model.summaries) > 1
        assert all(ContextBudget(3000).count_messages(c) <= 2700 for c in model.summaries + model.calls)


async def test_completed_tool_turn_moves_to_summary_as_a_unit(tmp_path):
    model = SummaryModel()
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path/'db')) as saver:
        graph = build_graph(lambda _: model, saver)
        config = {'configurable': {'thread_id': req(0).thread_id}}
        tool_turn = [HumanMessage(content='old request ' + 'q' * 1800, id='old'),
                    AIMessage(content='', tool_calls=[{'id':'c','name':'echo','args':{'text':'x'}}], id='call'),
                    ToolMessage(content='tool outcome', tool_call_id='c', id='result'),
                    AIMessage(content='done', id='reply:old')]
        await graph.aupdate_state(config, {'messages': tool_turn + [HumanMessage(content='recent',id='recent'), AIMessage(content='done',id='reply:recent')]}, as_node='generate')
        await invoke_turn(graph, req(0, size=100))
        state = (await graph.aget_state(config)).values
        assert state['summarized_until'] == 'reply:old'
        assert 'tool outcome' in ''.join(c[1].content for c in model.summaries)
        assert not any(isinstance(m, ToolMessage) for m in model.calls[-1])
        assert any(m.id == 'recent' for m in model.calls[-1])
