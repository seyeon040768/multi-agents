import asyncio
import json
import logging
import time
from uuid import uuid4
from langchain_core.messages import AIMessage, HumanMessage, SystemMessage, ToolMessage, RemoveMessage
from langgraph.graph import END, START, MessagesState, StateGraph
from langgraph.runtime import Runtime
from .providers import create_model
from .schema import GenerateRequest, GenerateResponse
from .tools.registry import TOOL_REGISTRY, resolve_tools

logger = logging.getLogger("agent_runtime.tools")
MAX_TOOL_CALLS = 10


class State(MessagesState):
    response: dict
    tool_calls_used: int
    input_tokens: int
    output_tokens: int
    stop_tools: bool
    blocked_calls: dict


def model_context(history, current_id, system):
    """Keep complete tool exchanges together; never send orphan function responses."""
    turns = []
    for message in history:
        if isinstance(message, HumanMessage):
            turns.append([])
        if turns:
            turns[-1].append(message)
    selected = []
    remaining = 64 * 1024
    for turn in reversed(turns):
        # Failed/incomplete older runs are not provider-compatible conversation history.
        if turn[0].id != current_id and turn[-1].id != f"reply:{turn[0].id}":
            continue
        size = sum(len((m.content if isinstance(m.content, str) else json.dumps(m.content, ensure_ascii=False)).encode()) +
                   len(json.dumps(getattr(m, "tool_calls", []), ensure_ascii=False).encode()) for m in turn)
        if selected and (size > remaining or len(selected) + len(turn) > 20):
            break
        if size > remaining:
            raise ValueError("current turn too large")
        selected = turn + selected
        remaining -= size
    if not any(m.id == current_id for m in selected):
        raise ValueError("current message missing")
    return [SystemMessage(content=system), *selected]


def build_graph(model_factory=create_model, checkpointer=None, registry=None):
    registry = TOOL_REGISTRY if registry is None else registry

    async def generate(state: State, runtime: Runtime[GenerateRequest]):
        req = runtime.context
        try:
            messages = model_context(state["messages"], req.post_id, req.messages[0].content)
        except ValueError as exc:
            if str(exc) != "current turn too large":
                raise
            text = "도구 결과가 대화 입력 한도를 초과했습니다. 요청 범위를 줄여 다시 시도해 주세요."
            inputs, outputs = state.get("input_tokens", 0), state.get("output_tokens", 0)
            return {"messages": [AIMessage(content=text, id=f"reply:{req.post_id}",
                       usage_metadata={"input_tokens": inputs, "output_tokens": outputs, "total_tokens": inputs + outputs})],
                    "response": GenerateResponse(text=text, input_tokens=inputs, output_tokens=outputs).model_dump()}
        model = model_factory(req)
        specs = resolve_tools(req.tools, registry)
        if specs and not state.get("stop_tools"):
            model = model.bind_tools([s.tool for s in specs])
        result = await model.ainvoke(messages)
        usage = result.usage_metadata or {}
        inputs = state.get("input_tokens", 0) + usage.get("input_tokens", 0)
        outputs = state.get("output_tokens", 0) + usage.get("output_tokens", 0)
        if result.tool_calls:
            if state.get("stop_tools") or len(result.tool_calls) > MAX_TOOL_CALLS:
                # Terminate a model that keeps requesting tools after the hard budget.
                result = AIMessage(content="도구 호출 한도에 도달했습니다. 요청 범위를 줄여 다시 시도해 주세요.")
            else:
                ids = [c["id"] for c in result.tool_calls]
                if any(not x for x in ids) or len(set(ids)) != len(ids):
                    raise ValueError("invalid tool calls")
                result.id = f"tool-turn:{req.post_id}:{uuid4()}"
                return {"messages": [result], "input_tokens": inputs, "output_tokens": outputs}
        content = result.content
        text = content if isinstance(content, str) else "\n".join(
            b["text"] for b in content if isinstance(b, dict) and b.get("type") == "text" and isinstance(b.get("text"), str))
        if not text.strip() or len(text.encode()) > 128 * 1024:
            raise ValueError("invalid model text")
        response = GenerateResponse(text=text, input_tokens=inputs, output_tokens=outputs)
        # Retain provider metadata (e.g. Gemini thought signatures) during tool loops.
        result.id = f"reply:{req.post_id}"
        result.content = text
        result.usage_metadata = {"input_tokens": inputs, "output_tokens": outputs, "total_tokens": inputs + outputs}
        return {"response": response.model_dump(), "messages": [result], "input_tokens": inputs, "output_tokens": outputs}

    def policy_check(state: State, runtime: Runtime[GenerateRequest]):
        allowed = {s.function_name for s in resolve_tools(runtime.context.tools, registry)}
        blocked = {c["id"]: "TOOL_NOT_ALLOWED" for c in state["messages"][-1].tool_calls if c["name"] not in allowed}
        return {"blocked_calls": blocked, "stop_tools": state.get("tool_calls_used", 0) >= MAX_TOOL_CALLS}

    async def execute(state: State, runtime: Runtime[GenerateRequest]):
        req = runtime.context
        allowed = {s.function_name: s for s in resolve_tools(req.tools, registry)}
        used = state.get("tool_calls_used", 0)
        results = []
        for call in state["messages"][-1].tool_calls:
            started = time.monotonic()
            spec = allowed.get(call["name"])
            error = None
            used += 1  # Count attempts, including denied/invalid calls, to bound the loop.
            if used > MAX_TOOL_CALLS:
                error = "TOOL_CALL_LIMIT"
            elif spec is None or call["id"] in state.get("blocked_calls", {}):
                error = "TOOL_NOT_ALLOWED"
            else:
                try:
                    content = await asyncio.wait_for(spec.tool.ainvoke(call["args"]), timeout=12)
                    if not isinstance(content, str) or len(content.encode()) > 24 * 1024:
                        raise ValueError("invalid tool result")
                except Exception:
                    error = "TOOL_EXECUTION_FAILED"
            if error:
                content = json.dumps({"error": error})
            results.append(ToolMessage(content=content, tool_call_id=call["id"], name=call["name"],
                                       id=f"tool-result:{state['messages'][-1].id}:{call['id']}", status="error" if error else "success"))
            # Log argument field names, never argument values, credentials or full outputs.
            logger.info("tool agent_id=%s thread_id=%s tool_id=%s tool_call_id=%s argument_fields=%s status=%s duration_ms=%d error=%s",
                        req.agent_id, req.thread_id, spec.id if spec else "unregistered-or-blocked",
                        call["id"][:128], sorted(str(k)[:64] for k in call["args"]),
                        "error" if error else "success", int((time.monotonic()-started)*1000), error or "none")
        return {"messages": results, "tool_calls_used": used, "stop_tools": used >= MAX_TOOL_CALLS}

    graph = StateGraph(State, context_schema=GenerateRequest)
    graph.add_node("generate", generate)
    graph.add_node("policy_check", policy_check)
    graph.add_node("tools", execute)
    graph.add_edge(START, "generate")
    graph.add_conditional_edges("generate", lambda state: "policy_check" if state["messages"][-1].tool_calls else END)
    graph.add_edge("policy_check", "tools")
    graph.add_edge("tools", "generate")
    return graph.compile(checkpointer=checkpointer)


async def invoke_turn(graph, req):
    config = {"configurable": {"thread_id": req.thread_id}, "recursion_limit": 40}
    removals = []
    if graph.checkpointer is not None:
        state = await graph.aget_state(config)
        for message in state.values.get("messages", []):
            if message.id == f"reply:{req.post_id}":
                usage = message.usage_metadata or {}
                return {"response": GenerateResponse(text=message.content, input_tokens=usage.get("input_tokens", 0),
                                                       output_tokens=usage.get("output_tokens", 0)).model_dump()}
        history = state.values.get("messages", [])
        for index, message in enumerate(history):
            if message.id == req.post_id:
                # Retry unfinished turn from its user input; do not carry orphan calls.
                for following in history[index + 1:]:
                    if isinstance(following, HumanMessage):
                        break
                    removals.append(RemoveMessage(id=following.id))
                break
    return await graph.ainvoke({"messages": [*removals, HumanMessage(content=req.messages[-1].content, id=req.post_id)],
                               "tool_calls_used": 0, "input_tokens": 0, "output_tokens": 0, "stop_tools": False},
                              config=config, context=req, **({"durability": "sync"} if graph.checkpointer is not None else {}))
