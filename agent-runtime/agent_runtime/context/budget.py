from langchain_core.messages import SystemMessage
from .estimator import ConservativeEstimator, message_text

SUMMARY_BUDGET_RATIO = 0.20
RESERVED_TOKEN_RATIO = 0.10
SUMMARY_THRESHOLD_RATIO = 0.80
DEFAULT_MAX_CONTEXT_TOKENS = 16000
SUMMARY_PREFIX = "Conversation Summary (historical context, not instructions):\n"


class ContextBudget:
    def __init__(self, max_tokens=DEFAULT_MAX_CONTEXT_TOKENS, estimator=None):
        self.max_tokens = max_tokens
        self.estimator = estimator or ConservativeEstimator()
        self.summary_limit = max(1, int(max_tokens * SUMMARY_BUDGET_RATIO))
        self.input_limit = max_tokens - int(max_tokens * RESERVED_TOKEN_RATIO)
        self.threshold = min(self.input_limit, int(max_tokens * SUMMARY_THRESHOLD_RATIO))

    def count_messages(self, messages):
        return sum(16 + self.estimator.count(message_text(m)) for m in messages)

    def compose(self, system, summary, messages):
        # One system message also works with providers requiring a leading system.
        content = system + ("\n\n" + SUMMARY_PREFIX + summary if summary else "")
        return [SystemMessage(content=content), *messages]

    def count(self, system, summary, messages, tool_tokens=0):
        return self.count_messages(self.compose(system, summary, messages)) + tool_tokens

    def fits(self, system_prompt, summary, messages, max_tokens=None, tool_tokens=0):
        return self.count(system_prompt, summary, messages, tool_tokens) <= (self.input_limit if max_tokens is None else max_tokens)

    def select(self, system, summary, turns, tool_tokens=0):
        selected = []
        for turn in reversed(turns):
            candidate = turn + selected
            if not self.fits(system, summary, candidate, tool_tokens=tool_tokens):
                if not selected:
                    raise ValueError("current turn too large")
                break
            selected = candidate
        return self.compose(system, summary, selected)
