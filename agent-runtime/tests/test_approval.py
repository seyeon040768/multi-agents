import json
from datetime import datetime, timedelta, timezone
import pytest
from fastapi.testclient import TestClient
from langchain_core.messages import ToolMessage
from langchain_core.tools import tool
from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver
from agent_runtime.app import create_app
from agent_runtime.graph import build_graph, invoke_turn, resume_turn, ApprovalPending
from agent_runtime.schema import ResumeRequest
from agent_runtime.tools.registry import ToolSpec, check_tool_policy, ToolDecision
from test_tools import LoopModel, request


def resume(req, approval, decision='approve', **policy):
    return ResumeRequest(agent_id=req.agent_id, root_post_id=req.root_post_id,
                         approval_id=approval['approval_id'], decision=decision,
                         tools=req.tools.model_copy(update=policy))


def registry(executed, fail=False):
    @tool
    def debug_echo(text: str) -> str:
        """Echo text."""
        executed.append(text)
        if fail:
            raise RuntimeError('secret')
        return text
    return {'debug-echo': ToolSpec('debug-echo', 'debug_echo', debug_echo, 'Echo')}


@pytest.mark.parametrize('provider', ['google', 'openai', 'anthropic', 'ollama'])
async def test_interrupt_restart_resume_exact_args_and_duplicate_block(tmp_path, provider):
    executed=[]
    model=LoopModel(args={'text':'exact approved argument'})
    req=request(require_confirmation=['debug-echo'])
    req.provider=provider
    path=str(tmp_path/'db')
    async with AsyncSqliteSaver.from_conn_string(path) as saver:
        graph=build_graph(lambda _:model,saver,registry(executed))
        result=await invoke_turn(graph,req)
        approval=result['response']['interrupt']
        assert result['response']['status']=='interrupted'
        assert approval['calls'][0]['args']==model.args
        assert model.bound==[['debug_echo']]
        assert not executed
        assert (await invoke_turn(graph,req))['response']['interrupt']==approval
        with pytest.raises(ApprovalPending):
            await invoke_turn(graph,req.model_copy(update={'post_id':'x'*26}))
    async with AsyncSqliteSaver.from_conn_string(path) as saver:
        graph=build_graph(lambda _:model,saver,registry(executed))
        result=await resume_turn(graph,resume(req,approval))
        assert result['response']['text']=='final answer'
        assert result['response']['approval_status']=='EXECUTED'
        assert executed==['exact approved argument']
        assert isinstance(model.calls[-1][-1],ToolMessage)
        assert model.calls[-1][-1].content=='exact approved argument'
        with pytest.raises(ApprovalPending):
            await resume_turn(graph,resume(req,approval))
        assert len(executed)==1


@pytest.mark.parametrize('decision,error,status',[('reject','TOOL_REJECTED','REJECTED'),('expire','TOOL_APPROVAL_EXPIRED','EXPIRED')])
async def test_reject_expire_without_execution(tmp_path,decision,error,status):
    executed=[]
    model=LoopModel()
    req=request(require_confirmation=['debug-echo'])
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path/'db')) as saver:
        graph=build_graph(lambda _:model,saver,registry(executed))
        approval=(await invoke_turn(graph,req))['response']['interrupt']
        result=await resume_turn(graph,resume(req,approval,decision))
        assert result['response']['approval_status']==status
        assert json.loads(model.calls[-1][-1].content)['error']==error
        assert not executed


async def test_actual_expiry_authoritative(tmp_path):
    executed=[]
    model=LoopModel()
    req=request(require_confirmation=['debug-echo'])
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path/'db')) as saver:
        graph=build_graph(lambda _:model,saver,registry(executed))
        approval=(await invoke_turn(graph,req))['response']['interrupt']
        approval['expires_at']=(datetime.now(timezone.utc)-timedelta(seconds=1)).isoformat()
        config={'configurable':{'thread_id':req.thread_id}}
        await graph.aupdate_state(config,{'pending_approval':approval},as_node='policy_check')
        assert (await graph.ainvoke(None,config,context=req))['__interrupt__']
        result=await resume_turn(graph,resume(req,approval))
        assert result['response']['approval_status']=='EXPIRED'
        assert not executed


@pytest.mark.parametrize('policy',[{'denied':['debug-echo']},{'enabled':False},{'allowed':[]}])
async def test_revoked_policy_cannot_execute(tmp_path,policy):
    executed=[]
    model=LoopModel()
    req=request(require_confirmation=['debug-echo'])
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path/'db')) as saver:
        graph=build_graph(lambda _:model,saver,registry(executed))
        approval=(await invoke_turn(graph,req))['response']['interrupt']
        result=await resume_turn(graph,resume(req,approval,**policy))
        assert result['response']['approval_status']=='FAILED'
        assert not executed
        assert json.loads(model.calls[-1][-1].content)['error']=='TOOL_NOT_ALLOWED'


async def test_execution_failure_outcome(tmp_path):
    executed=[]
    model=LoopModel()
    req=request(require_confirmation=['debug-echo'])
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path/'db')) as saver:
        graph=build_graph(lambda _:model,saver,registry(executed,True))
        approval=(await invoke_turn(graph,req))['response']['interrupt']
        result=await resume_turn(graph,resume(req,approval))
        assert result['response']['approval_status']=='FAILED'
        assert len(executed)==1
        assert json.loads(model.calls[-1][-1].content)['error']=='TOOL_EXECUTION_FAILED'


async def test_wrong_id_and_other_thread_rejected(tmp_path):
    model=LoopModel()
    req=request(require_confirmation=['debug-echo'])
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path/'db')) as saver:
        graph=build_graph(lambda _:model,saver)
        approval=(await invoke_turn(graph,req))['response']['interrupt']
        for updates in [{'approval_id':'apr_'+'0'*32},{'agent_id':'reviewer'},{'root_post_id':'z'*26}]:
            with pytest.raises(ApprovalPending):
                await resume_turn(graph,resume(req,approval).model_copy(update=updates))


def test_http_auth_validation_and_restart(tmp_path):
    model=LoopModel()
    path=str(tmp_path/'db')
    def app():
        return create_app(token='token',checkpoint_path=path,model_factory=lambda _:model)
    headers={'Authorization':'Bearer token'}
    req=request(require_confirmation=['debug-echo'])
    with TestClient(app()) as client:
        data=client.post('/v1/generate',json=req.model_dump(),headers=headers).json()
        assert data['status']=='interrupted'
    with TestClient(app()) as client:
        body=resume(req,data['interrupt'],'reject').model_dump()
        assert client.post('/v1/resume',json=body).status_code==401
        assert client.post('/v1/resume',json={**body,'decision':'bogus'},headers=headers).status_code==400
        response=client.post('/v1/resume',json=body,headers=headers)
        assert response.status_code==200
        assert response.json()['approval_status']=='REJECTED'
        assert client.post('/v1/resume',json=body,headers=headers).status_code==409


def test_denied_policy_wins():
    assert check_tool_policy(request().tools,'debug-echo')==ToolDecision.ALLOW
    assert check_tool_policy(request(require_confirmation=['debug-echo']).tools,'debug-echo')==ToolDecision.REQUIRE_CONFIRMATION
    assert check_tool_policy(request(require_confirmation=['debug-echo'],denied=['debug-echo']).tools,'debug-echo')==ToolDecision.DENY


async def test_batch_approval_and_second_interrupt_outcomes(tmp_path):
    from langchain_core.messages import AIMessage
    executed=[]
    class BatchModel(LoopModel):
        async def ainvoke(self,messages):
            self.calls.append(messages)
            results=[m for m in messages if isinstance(m,ToolMessage)]
            if len(results)==3:
                return AIMessage(content='done')
            count=2 if not results else 1
            return AIMessage(content='',tool_calls=[{'name':'debug_echo','args':{'text':f'round-{len(results)}-{i}'},'id':f'call-{i}'} for i in range(count)])
    model=BatchModel()
    req=request(require_confirmation=['debug-echo'])
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path/'db')) as saver:
        graph=build_graph(lambda _:model,saver,registry(executed))
        first=(await invoke_turn(graph,req))['response']['interrupt']
        assert len(first['calls'])==2
        assert not executed
        result=await resume_turn(graph,resume(req,first))
        assert result['response']['approval_status']=='EXECUTED'
        second=result['response']['interrupt']
        assert second['approval_id']!=first['approval_id']
        assert executed==['round-0-0','round-0-1']
        assert (await resume_turn(graph,resume(req,second)))['response']['text']=='done'
        assert executed==['round-0-0','round-0-1','round-2-0']


async def test_protected_infinite_loop_respects_budget(tmp_path):
    from langchain_core.messages import AIMessage
    executed=[]
    class Endless(LoopModel):
        async def ainvoke(self,messages):
            return AIMessage(content='',tool_calls=[{'name':'debug_echo','args':{'text':'loop'},'id':'repeat'}])
    req=request(require_confirmation=['debug-echo'])
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path/'db')) as saver:
        graph=build_graph(lambda _:Endless(),saver,registry(executed))
        response=(await invoke_turn(graph,req))['response']
        approvals=0
        while response.get('status')=='interrupted':
            approvals+=1
            response=(await resume_turn(graph,resume(req,response['interrupt'])))['response']
        assert approvals==10
        assert len(executed)==10
        assert '한도' in response['text']
