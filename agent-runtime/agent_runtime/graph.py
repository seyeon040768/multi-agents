from langchain_core.messages import AIMessage, HumanMessage, SystemMessage
from langgraph.graph import END, START, MessagesState, StateGraph
from langgraph.runtime import Runtime
from .providers import create_model
from .schema import GenerateRequest, GenerateResponse


class State(MessagesState):
    # Plain JSON values keep checkpoint serialization independent of API DTOs.
    response: dict


def model_context(history, current_id, system):
    """Bound model input without deleting checkpoint history; future summary hook."""
    messages = []
    remaining = 64 * 1024
    for message in reversed(history):
        size = len(message.content.encode())
        if size > remaining or len(messages) >= 20:
            break
        messages.append(message)
        remaining -= size
    messages.reverse()
    # Avoid beginning the selected window with an assistant response.
    while messages and isinstance(messages[0], AIMessage):
        messages.pop(0)
    if not messages or messages[-1].id != current_id:
        raise ValueError("current message missing")
    return [SystemMessage(content=system), *messages]


def build_graph(model_factory=create_model, checkpointer=None):
    async def generate(state: State, runtime: Runtime[GenerateRequest]):
        req = runtime.context
        messages = model_context(state["messages"], req.post_id, req.messages[0].content)
        result = await model_factory(req).ainvoke(messages)
        if result.tool_calls:
            raise ValueError("tool calls are unavailable")
        content = result.content
        if isinstance(content, str):
            text = content
        else:
            text = "\n".join(block["text"] for block in content
                             if isinstance(block, dict) and block.get("type") == "text"
                             and isinstance(block.get("text"), str))
        if not text.strip() or len(text.encode()) > 128 * 1024:
            raise ValueError("invalid model text")
        usage = result.usage_metadata or {}
        response = GenerateResponse(text=text, input_tokens=usage.get("input_tokens", 0),
                                    output_tokens=usage.get("output_tokens", 0))
        # Stable IDs replace repeated input instead of appending duplicate messages.
        answer = AIMessage(content=text, id=f"reply:{req.post_id}", usage_metadata=result.usage_metadata)
        return {"response": response.model_dump(), "messages": [answer]}

    graph = StateGraph(State, context_schema=GenerateRequest)
    graph.add_node("generate", generate)
    graph.add_edge(START, "generate")
    graph.add_edge("generate", END)
    return graph.compile(checkpointer=checkpointer)


async def invoke_turn(graph, req):
    config = {"configurable": {"thread_id": req.thread_id}}
    if graph.checkpointer is not None:
        state = await graph.aget_state(config)
        for message in state.values.get("messages", []):
            if message.id == f"reply:{req.post_id}":
                usage = message.usage_metadata or {}
                return {"response": GenerateResponse(text=message.content,
                    input_tokens=usage.get("input_tokens", 0),
                    output_tokens=usage.get("output_tokens", 0)).model_dump()}
    return await graph.ainvoke({"messages": [HumanMessage(content=req.messages[-1].content, id=req.post_id)]},
                              config=config, context=req, **({"durability": "sync"} if graph.checkpointer is not None else {}))
