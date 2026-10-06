"""Only registered implementations can cross the tool execution boundary."""
from dataclasses import dataclass
from langchain_core.tools import BaseTool, tool
from pydantic import BaseModel, Field
from .web_search import web_search


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
    "debug-echo": ToolSpec("debug-echo", "debug_echo", debug_echo, debug_echo.description),
    "web-search": ToolSpec("web-search", "web_search", web_search, web_search.description),
}


def resolve_tools(config, registry=None):
    registry = TOOL_REGISTRY if registry is None else registry
    if not config.enabled:
        return []
    executable = set(config.allowed) - set(config.denied) - set(config.require_confirmation)
    return [registry[key] for key in sorted(executable) if key in registry]
