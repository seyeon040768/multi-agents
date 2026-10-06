import asyncio
import pytest
from fastapi.testclient import TestClient
from langchain_core.messages import AIMessage
from agent_runtime.app import create_app
from agent_runtime.graph import build_graph, invoke_turn
from agent_runtime.providers import create_model
from agent_runtime.schema import GenerateRequest


def payload(provider="google", model="gemini-3-flash-preview"):
    return {"agent_id": "researcher", "root_post_id": "r" * 26, "post_id": "p" * 26, "provider": provider, "model": model, "temperature": 0.2, "max_tokens": 8000,
            "top_p": None, "messages": [{"role": "system", "content": "identity"},
                                       {"role": "user", "content": "question"}]}


class FakeModel:
    async def ainvoke(self, messages):
        assert messages[0].type == "system"
        assert messages[-1].content == "question"
        return AIMessage(content="안녕하세요", usage_metadata={"input_tokens": 3, "output_tokens": 2, "total_tokens": 5})


@pytest.mark.parametrize("provider,model", [("openai", "gpt-5.4"), ("google", "gemini-3-flash-preview"),
                                           ("anthropic", "claude-sonnet-4-6"), ("ollama", "qwen3:8b")])
async def test_graph_routes_without_llm(provider, model):
    def factory(req):
        assert req.provider == provider
        assert req.model == model
        return FakeModel()
    result = await invoke_turn(build_graph(factory), GenerateRequest(**payload(provider, model)))
    assert result["response"]["text"] == "안녕하세요"
    assert result["response"]["input_tokens"] == 3


def test_auth_schema_and_error_redaction():
    with TestClient(create_app(build_graph(lambda _: FakeModel()), token="test-token")) as client:
        assert client.get("/healthz").status_code == 200
        assert client.post("/v1/generate", json=payload()).status_code == 401
        headers = {"Authorization": "Bearer test-token"}
        result = client.post("/v1/generate", json=payload(), headers=headers)
        assert result.status_code == 200, result.text
        assert result.json()["text"] == "안녕하세요"
        bad = payload()
        bad["api_key"] = "secret"
        result = client.post("/v1/generate", json=bad, headers=headers)
        assert result.status_code == 400
        assert "secret" not in result.text
        assert client.post("/v1/generate", content="x" * (512 * 1024 + 1), headers=headers).status_code == 413


def test_missing_auth_fails_startup():
    with pytest.raises(RuntimeError):
        with TestClient(create_app(token="")):
            pass


def test_failure_is_generic(caplog):
    class Failing:
        async def ainvoke(self, _):
            raise RuntimeError("secret-api-key")
    with TestClient(create_app(build_graph(lambda _: Failing()), token="test-token")) as client:
        result = client.post("/v1/generate", json=payload(), headers={"Authorization": "Bearer test-token"})
        assert result.status_code == 502
        assert "secret-api-key" not in result.text
        assert "secret-api-key" not in caplog.text


def test_timeout_cancels_model():
    cancelled = []
    class Slow:
        async def ainvoke(self, _):
            try:
                await asyncio.sleep(10)
            finally:
                cancelled.append(True)
    with TestClient(create_app(build_graph(lambda _: Slow()), token="test-token", timeout=0.02)) as client:
        result = client.post("/v1/generate", json=payload(), headers={"Authorization": "Bearer test-token"})
        assert result.status_code == 504
        assert cancelled == [True]


async def test_unexpected_tool_call_has_no_execution():
    class Tool:
        async def ainvoke(self, _):
            return AIMessage(content="", tool_calls=[{"name": "shell", "args": {}, "id": "call"}])
    with pytest.raises(ValueError, match="tool calls"):
        await invoke_turn(build_graph(lambda _: Tool()), GenerateRequest(**payload()))


@pytest.mark.parametrize("provider,model,key", [("openai", "gpt-5.4", "OPENAI_API_KEY"),
                                                ("google", "gemini-3-flash-preview", "GOOGLE_API_KEY"),
                                                ("anthropic", "claude-sonnet-4-6", "ANTHROPIC_API_KEY"),
                                                ("ollama", "qwen3:8b", "OLLAMA_BASE_URL")])
def test_real_provider_adapter_construction(monkeypatch, provider, model, key):
    monkeypatch.setenv(key, "http://127.0.0.1:11434" if provider == "ollama" else "test-key")
    req = GenerateRequest(**payload(provider, model))
    adapter = create_model(req)
    assert hasattr(adapter, "ainvoke")
    if provider != "ollama":
        assert adapter.max_retries == 0
    req.top_p = 0.8
    assert hasattr(create_model(req), "ainvoke")
