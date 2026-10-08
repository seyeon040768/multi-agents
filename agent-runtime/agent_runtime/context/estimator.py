import json
from typing import Protocol


class TokenEstimator(Protocol):
    def count(self, text: str) -> int: ...


class ConservativeEstimator:
    """One token per UTF-8 byte, plus message framing; replaceable per provider.

    Deliberately overestimates typical text. This is an estimate, not a provider
    tokenizer guarantee (provider-specific hidden framing can differ).
    """
    def count(self, text: str) -> int:
        return len(text.encode("utf-8"))


def message_text(message):
    return json.dumps({"role": message.type, "content": message.content,
                       "tool_calls": getattr(message, "tool_calls", []),
                       "tool_call_id": getattr(message, "tool_call_id", None),
                       "metadata": message.additional_kwargs}, ensure_ascii=False)
