from typing import TypedDict
from langchain_core.messages import AIMessage, HumanMessage, SystemMessage
from langgraph.graph import END, START, StateGraph
from .providers import create_model
from .schema import GenerateRequest, GenerateResponse


class State(TypedDict):
    request: GenerateRequest
    response: GenerateResponse


def build_graph(model_factory=create_model):
    async def generate(state: State):
        req = state["request"]
        roles = {"system": SystemMessage, "user": HumanMessage, "assistant": AIMessage}
        messages = [roles[m.role](content=m.content) for m in req.messages]
        result = await model_factory(req).ainvoke(messages)
        # No tools are bound and no tool node exists. Unexpected tool calls fail closed.
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
        return {"response": GenerateResponse(text=text,
                                             input_tokens=usage.get("input_tokens", 0),
                                             output_tokens=usage.get("output_tokens", 0))}

    graph = StateGraph(State)
    graph.add_node("generate", generate)
    graph.add_edge(START, "generate")
    graph.add_edge("generate", END)
    return graph.compile()
