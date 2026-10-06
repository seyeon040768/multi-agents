from typing import Literal
from pydantic import BaseModel, ConfigDict, Field, model_validator


class Message(BaseModel):
    model_config = ConfigDict(extra="forbid")
    role: Literal["system", "user", "assistant"]
    content: str = Field(min_length=1, max_length=65536)


class GenerateRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")
    provider: Literal["openai", "google", "anthropic", "ollama"]
    model: str = Field(min_length=1, max_length=128)
    messages: list[Message] = Field(min_length=2, max_length=21)
    temperature: float = Field(ge=0, le=2)
    max_tokens: int = Field(ge=1, le=32768)
    top_p: float | None = Field(default=None, ge=0, le=1)

    @model_validator(mode="after")
    def validate_messages(self):
        if self.messages[0].role != "system" or self.messages[-1].role != "user":
            raise ValueError("system prompt and final user message required")
        if any(m.role == "system" for m in self.messages[1:]):
            raise ValueError("only the first message may be system")
        if sum(len(m.content.encode()) for m in self.messages) > 81920:
            raise ValueError("context too large")
        return self


class GenerateResponse(BaseModel):
    text: str
    input_tokens: int = 0
    output_tokens: int = 0
