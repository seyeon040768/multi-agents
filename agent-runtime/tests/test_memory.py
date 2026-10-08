import asyncio
import sqlite3
import pytest
from fastapi.testclient import TestClient
from langchain_core.messages import AIMessage
from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver
from agent_runtime.app import create_app
from agent_runtime.graph import build_graph, invoke_turn
from agent_runtime.memory import ThreadLocks
from agent_runtime.schema import GenerateRequest
from test_runtime import payload


def turn(text, post="a", root="r", agent="researcher"):
    data = payload()
    data.update(agent_id=agent, root_post_id=root * 26, post_id=post * 26)
    data["messages"][-1]["content"] = text
    return data


class RecordingModel:
    def __init__(self):
        self.calls = []

    async def ainvoke(self, messages):
        self.calls.append([(m.type, m.content) for m in messages])
        return AIMessage(content="remembered answer")


async def test_sqlite_reopen_continuity_and_agent_thread_isolation(tmp_path):
    path = str(tmp_path / "checkpoints.sqlite")
    model = RecordingModel()
    async with AsyncSqliteSaver.from_conn_string(path) as saver:
        graph = build_graph(lambda _: model, saver)
        await invoke_turn(graph, GenerateRequest(**turn("RAG?")))
    # Close connection and create a completely new graph/saver, as on process restart.
    async with AsyncSqliteSaver.from_conn_string(path) as saver:
        graph = build_graph(lambda _: model, saver)
        await invoke_turn(graph, GenerateRequest(**turn("advantages?", post="b")))
        assert model.calls[-1] == [("system", "identity"), ("human", "RAG?"),
                                  ("ai", "remembered answer"), ("human", "advantages?")]
        await invoke_turn(graph, GenerateRequest(**turn("other thread", post="c", root="s")))
        assert len(model.calls[-1]) == 2
        await invoke_turn(graph, GenerateRequest(**turn("other agent", post="d", agent="reviewer")))
        assert len(model.calls[-1]) == 2
        before = len(model.calls)
        duplicate = await invoke_turn(graph, GenerateRequest(**turn("advantages?", post="b")))
        assert duplicate["response"]["text"] == "remembered answer"
        assert len(model.calls) == before
        req = GenerateRequest(**turn("advantages?", post="b"))
        state = await graph.aget_state({"configurable": {"thread_id": req.thread_id}})
        assert len(state.values["messages"]) == 4
        assert "request" not in state.values


def test_app_lifespan_reopen_and_latest_prompt(tmp_path):
    model = RecordingModel()
    path = str(tmp_path / "db.sqlite")
    headers = {"Authorization": "Bearer test-token"}
    def app():
        return create_app(token="test-token", checkpoint_path=path, model_factory=lambda _: model)
    with TestClient(app()) as client:
        assert client.post("/v1/generate", json=turn("first"), headers=headers).status_code == 200
    with TestClient(app()) as client:
        data = turn("second", post="b")
        data["messages"][0]["content"] = "updated identity"
        assert client.post("/v1/generate", json=data, headers=headers).status_code == 200
        assert model.calls[-1][0] == ("system", "updated identity")
        assert model.calls[-1][1] == ("human", "first")


async def test_same_thread_serialized_and_duplicate_generated_once(tmp_path):
    model = RecordingModel()
    locks = ThreadLocks()
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path / "db")) as saver:
        graph = build_graph(lambda _: model, saver)
        async def run(data):
            req = GenerateRequest(**data)
            async with locks.hold(req.thread_id):
                return await invoke_turn(graph, req)
        await asyncio.gather(run(turn("first")), run(turn("first")), run(turn("second", post="b")))
        assert len(model.calls) == 2
        assert model.calls[-1][-3:] == [("human", "first"), ("ai", "remembered answer"), ("human", "second")]
        assert not locks.entries


async def test_lock_cancelled_waiter_releases_entry():
    locks = ThreadLocks()
    async with locks.hold("thread"):
        async def waiting():
            async with locks.hold("thread"):
                pytest.fail("cancelled waiter entered")
        task = asyncio.create_task(waiting())
        await asyncio.sleep(0)
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task
    assert not locks.entries


def test_checkpoint_initialization_error_keeps_service_available(tmp_path, caplog):
    path = tmp_path / "directory.sqlite"
    path.mkdir()
    with TestClient(create_app(token="test-token", checkpoint_path=str(path))) as client:
        assert client.get("/healthz").json() == {"status": "degraded"}
        result = client.post("/v1/generate", json=turn("first"), headers={"Authorization": "Bearer test-token"})
        assert result.status_code == 503
        assert str(path) not in result.text
        assert str(path) not in caplog.text


def test_checkpoint_write_error_is_generic_and_other_requests_survive(caplog):
    from langgraph.checkpoint.memory import InMemorySaver
    class FailingSaver(InMemorySaver):
        async def aput(self, config, checkpoint, metadata, new_versions):
            if "researcher" in config["configurable"]["thread_id"]:
                raise sqlite3.OperationalError("secret database path")
            return await super().aput(config, checkpoint, metadata, new_versions)
    model = RecordingModel()
    graph = build_graph(lambda _: model, FailingSaver())
    with TestClient(create_app(graph, token="test-token")) as client:
        headers = {"Authorization": "Bearer test-token"}
        result = client.post("/v1/generate", json=turn("first"), headers=headers)
        assert result.status_code == 503
        assert "secret database path" not in result.text + caplog.text
        assert client.post("/v1/generate", json=turn("other", agent="reviewer"), headers=headers).status_code == 200


async def test_context_window_bounded_without_deleting_checkpoints(tmp_path):
    model = RecordingModel()
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path / "db")) as saver:
        graph = build_graph(lambda _: model, saver)
        for n in range(15):
            data = turn(f"question {n}")
            data["post_id"] = f"{n:026d}"
            await invoke_turn(graph, GenerateRequest(**data))
        assert len(model.calls[-1]) == 30  # Short history fits without count-based trimming.
        assert model.calls[-1][-1] == ("human", "question 14")
        state = await graph.aget_state({"configurable": {"thread_id": "mattermost:researcher:" + "r" * 26}})
        assert len(state.values["messages"]) == 30

async def test_failed_turn_can_retry_without_duplicate_user(tmp_path):
    calls = []
    class Model:
        async def ainvoke(self, messages):
            calls.append(messages)
            if len(calls) == 1:
                raise RuntimeError("provider unavailable")
            return AIMessage(content="recovered")
    async with AsyncSqliteSaver.from_conn_string(str(tmp_path / "db")) as saver:
        graph = build_graph(lambda _: Model(), saver)
        req = GenerateRequest(**turn("first"))
        with pytest.raises(RuntimeError):
            await invoke_turn(graph, req)
        result = await invoke_turn(graph, req)
        assert result["response"]["text"] == "recovered"
        assert len(calls[-1]) == 2
        state = await graph.aget_state({"configurable": {"thread_id": req.thread_id}})
        assert len(state.values["messages"]) == 2


def test_memory_contract_rejects_full_history_and_bad_identifiers():
    from pydantic import ValidationError
    data = turn("first")
    data["messages"].insert(1, {"role": "assistant", "content": "old answer"})
    with pytest.raises(ValidationError):
        GenerateRequest(**data)
    for field, value in [("root_post_id", "wrong"), ("post_id", "../secret"), ("agent_id", "Bad Agent")]:
        data = turn("first")
        data[field] = value
        with pytest.raises(ValidationError):
            GenerateRequest(**data)
