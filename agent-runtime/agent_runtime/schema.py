from typing import Literal
from pydantic import BaseModel, ConfigDict, Field, model_validator, field_validator


class Message(BaseModel):
    model_config = ConfigDict(extra="forbid")
    role: Literal["system", "user", "assistant"]
    content: str = Field(min_length=1, max_length=65536)


class ToolConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")
    enabled: bool = False
    allowed: list[str] = Field(default_factory=list, max_length=64)
    denied: list[str] = Field(default_factory=list, max_length=64)
    require_confirmation: list[str] = Field(default_factory=list, max_length=64)

    @field_validator("allowed", "denied", "require_confirmation", mode="before")
    @classmethod
    def empty_go_slices(cls, value):
        return [] if value is None else value


class GenerateRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")
    agent_id: str = Field(pattern=r"^[a-z][a-z0-9._-]{2,31}$")
    root_post_id: str = Field(pattern=r"^[a-z0-9]{26}$")
    post_id: str = Field(pattern=r"^[a-z0-9]{26}$")
    provider: Literal["openai", "google", "anthropic", "ollama"]
    model: str = Field(min_length=1, max_length=128)
    messages: list[Message] = Field(min_length=2, max_length=2)
    temperature: float = Field(ge=0, le=2)
    max_tokens: int = Field(ge=1, le=32768)
    max_context_tokens: int | None = Field(default=None, ge=512, le=2000000)
    tools: ToolConfig = Field(default_factory=ToolConfig)
    top_p: float | None = Field(default=None, ge=0, le=1)

    @property
    def thread_id(self):
        return f"mattermost:{self.agent_id}:{self.root_post_id}"

    @model_validator(mode="after")
    def validate_messages(self):
        if self.messages[0].role != "system" or self.messages[-1].role != "user":
            raise ValueError("system prompt and final user message required")
        if any(m.role == "system" for m in self.messages[1:]):
            raise ValueError("only the first message may be system")
        if len(self.messages[0].content.encode()) > 16 * 1024 or len(self.messages[-1].content.encode()) > 64 * 1024:
            raise ValueError("context too large")
        return self


class GenerateResponse(BaseModel):
    status: Literal["completed"] = "completed"
    text: str
    input_tokens: int = 0
    output_tokens: int = 0


class ResumeRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")
    agent_id: str = Field(pattern=r"^[a-z][a-z0-9._-]{2,31}$")
    root_post_id: str = Field(pattern=r"^[a-z0-9]{26}$")
    approval_id: str = Field(pattern=r"^apr_[a-f0-9]{32}$")
    decision: Literal["approve", "reject", "expire"]
    tools: ToolConfig

    @property
    def thread_id(self):
        return f"mattermost:{self.agent_id}:{self.root_post_id}"
