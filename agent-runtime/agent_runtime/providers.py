"""Provider credentials and endpoints are runtime configuration, never request data."""
import os
from .schema import GenerateRequest


def create_model(req: GenerateRequest):
    common = {"model": req.model, "temperature": req.temperature}
    if req.provider == "openai":
        from langchain_openai import ChatOpenAI
        if not os.getenv("OPENAI_API_KEY"):
            raise ValueError("provider not configured")
        kwargs = {**common, "api_key": os.environ["OPENAI_API_KEY"],
                  "max_tokens": req.max_tokens, "timeout": 75, "max_retries": 0}
        # The existing catalog uses gpt-5.4: sampling needs reasoning effort none.
        if req.model.startswith("gpt-5.4"):
            kwargs.update(use_responses_api=True, reasoning={"effort": "none"}, store=False)
        if req.top_p is not None:
            kwargs["top_p"] = req.top_p
        return ChatOpenAI(**kwargs)
    if req.provider == "google":
        from langchain_google_genai import ChatGoogleGenerativeAI
        key = os.getenv("GOOGLE_API_KEY") or os.getenv("GEMINI_API_KEY")
        if not key:
            raise ValueError("provider not configured")
        kwargs = {**common, "api_key": key, "max_output_tokens": req.max_tokens,
                  "timeout": 75, "max_retries": 0, "vertexai": False}
        if req.top_p is not None:
            kwargs["top_p"] = req.top_p
        return ChatGoogleGenerativeAI(**kwargs)
    if req.provider == "anthropic":
        from langchain_anthropic import ChatAnthropic
        if not os.getenv("ANTHROPIC_API_KEY"):
            raise ValueError("provider not configured")
        kwargs = {**common, "api_key": os.environ["ANTHROPIC_API_KEY"],
                  "max_tokens": req.max_tokens, "timeout": 75, "max_retries": 0}
        # Claude permits temperature OR top_p; avoid sending both.
        if req.top_p is not None:
            kwargs.pop("temperature")
            kwargs["top_p"] = req.top_p
        return ChatAnthropic(**kwargs)
    if req.provider == "ollama":
        from langchain_ollama import ChatOllama
        url = os.getenv("OLLAMA_BASE_URL")
        if not url:
            raise ValueError("provider not configured")
        kwargs = {**common, "base_url": url, "num_predict": req.max_tokens,
                  "client_kwargs": {"timeout": 75}}
        if req.top_p is not None:
            kwargs["top_p"] = req.top_p
        return ChatOllama(**kwargs)
    raise ValueError("unsupported provider")
