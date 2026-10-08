"""Only registered implementations can cross the tool execution boundary."""
from dataclasses import dataclass
from enum import Enum
from langchain_core.tools import BaseTool, tool
from pydantic import BaseModel, Field
from .web_search import web_search
from .file_reader import read_file


class EchoInput(BaseModel):
    text: str = Field(max_length=4096)


@tool(args_schema=EchoInput)
def debug_echo(text: str) -> str:
    """Return the supplied text unchanged, for testing tool calling."""
    return text


@dataclass(frozen=True)
class ToolSpec:
    id: str
    function_name: str
    tool: BaseTool
    description: str

    def __post_init__(self):
        if self.function_name != self.tool.name:
            raise ValueError("tool function name mismatch")


TOOL_REGISTRY = {
    "file-reader": ToolSpec("file-reader", "read_file", read_file, read_file.description),
    "debug-echo": ToolSpec("debug-echo", "debug_echo", debug_echo, debug_echo.description),
    "web-search": ToolSpec("web-search", "web_search", web_search, web_search.description),
}


class ToolDecision(str, Enum):
    ALLOW = "allow"
    DENY = "deny"
    REQUIRE_CONFIRMATION = "require_confirmation"


def check_tool_policy(config, tool_id):
    if not config.enabled or tool_id in config.denied or tool_id not in config.allowed:
        return ToolDecision.DENY
    if tool_id in config.require_confirmation:
        return ToolDecision.REQUIRE_CONFIRMATION
    return ToolDecision.ALLOW


def resolve_tools(config, registry=None):
    registry = TOOL_REGISTRY if registry is None else registry
    return [registry[key] for key in sorted(set(config.allowed))
            if key in registry and check_tool_policy(config, key) != ToolDecision.DENY]
