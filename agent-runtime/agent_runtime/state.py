from langgraph.graph import MessagesState


class AgentState(MessagesState):
    response: dict
    tool_calls_used: int
    input_tokens: int
    output_tokens: int
    stop_tools: bool
    blocked_calls: dict
    pending_approval: dict
    approval_result: dict
    approval_request: dict

    summary: str | None
    summarized_until: str | None
