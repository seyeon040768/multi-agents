from langchain_core.messages import HumanMessage, AIMessage, ToolMessage


def get_complete_turns(messages, current_id=None):
    """Preserve human + all AI/tool exchanges as an indivisible turn.

    Incomplete historical executions are excluded; the active turn is retained.
    """
    turns = []
    for message in messages:
        if isinstance(message, HumanMessage):
            turns.append([])
        if turns:
            turns[-1].append(message)
    result = []
    for turn in turns:
        pending = set()
        valid = True
        for message in turn:
            if isinstance(message, AIMessage):
                if pending:
                    valid = False
                pending.update(call['id'] for call in message.tool_calls)
            elif isinstance(message, ToolMessage):
                if message.tool_call_id not in pending:
                    valid = False
                pending.discard(message.tool_call_id)
        if turn[0].id == current_id:
            if not valid or pending:
                raise ValueError("incomplete current tool exchange")
            result.append(turn)
        elif valid and not pending and isinstance(turn[-1], AIMessage) and turn[-1].id == f"reply:{turn[0].id}":
            result.append(turn)
    return result


def unsummarized_turns(messages, cursor, current_id):
    if cursor:
        index = next((i for i, m in enumerate(messages) if m.id == cursor), None)
        if index is None:
            raise ValueError("summary cursor missing")
        messages = messages[index + 1:]
    return get_complete_turns(messages, current_id)
