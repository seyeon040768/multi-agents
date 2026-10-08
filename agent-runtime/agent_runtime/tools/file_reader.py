"""Read only authenticated plugin attachments; never accept paths or remote URLs."""
import json
import os
from contextvars import ContextVar
from dataclasses import dataclass
from urllib.parse import urlsplit

import httpx
from langchain_core.tools import tool
from pydantic import BaseModel, ConfigDict, Field

MAX_RESULT_BYTES = 20 * 1024
SAFE_ERRORS = {"FILE_PERMISSION_DENIED", "FILE_NOT_ATTACHED", "FILE_NOT_FOUND",
               "FILE_TOO_LARGE", "FILE_TYPE_NOT_SUPPORTED", "FILE_READ_FAILED"}


class FileReaderError(Exception):
    def __init__(self, code):
        self.code = code
        super().__init__(code)


class FileInput(BaseModel):
    model_config = ConfigDict(extra="forbid")
    file_id: str = Field(pattern=r"^[a-z0-9]{26}$")


@dataclass
class FileContext:
    request: object
    max_content_bytes: int
    approval_id: str | None = None


file_context = ContextVar("file_reader_context", default=None)


@tool(args_schema=FileInput)
async def read_file(file_id: str) -> str:
    """Read text from a Mattermost file attached to the current request only.

    Use an available attachment file_id, never a filesystem path or URL.
    The result may be truncated; do not claim to have read the omitted content.
    """
    ctx = file_context.get()
    if ctx is None or not ctx.request.permissions.files.read:
        raise FileReaderError("FILE_PERMISSION_DENIED")
    req = ctx.request
    if not any(a.file_id == file_id for a in req.attachments):
        raise FileReaderError("FILE_NOT_ATTACHED")
    # The address and credential are deployment configuration, never model inputs
    # or checkpoint state. No redirects, environment proxies or arbitrary URLs.
    base = os.getenv("AGENT_BRIDGE_URL", "").rstrip("/")
    secret = os.getenv("AGENT_RUNTIME_TOKEN", "")
    url = urlsplit(base)
    if url.scheme not in {"http", "https"} or not url.hostname or url.username or url.password or url.query or url.fragment or not secret:
        raise FileReaderError("FILE_READ_FAILED")
    body = {"agent_id": req.agent_id, "requester_user_id": req.requester_user_id,
            "channel_id": req.channel_id, "post_id": req.post_id, "file_id": file_id,
            "max_content_bytes": min(MAX_RESULT_BYTES, ctx.max_content_bytes)}
    if ctx.approval_id:
        body["approval_id"] = ctx.approval_id
    try:
        async with httpx.AsyncClient(timeout=10, follow_redirects=False, trust_env=False) as client:
            async with client.stream("POST", base + "/api/internal/files/read", json=body,
                                     headers={"Authorization": "Bearer " + secret}) as response:
                raw = bytearray()
                async for chunk in response.aiter_bytes():
                    raw.extend(chunk)
                    if len(raw) > 160 * 1024:
                        raise FileReaderError("FILE_READ_FAILED")
                data = json.loads(raw)
                if response.status_code != 200:
                    code = data.get("error", {}).get("code")
                    raise FileReaderError(code if code in SAFE_ERRORS else "FILE_READ_FAILED")
    except FileReaderError:
        raise
    except Exception:
        raise FileReaderError("FILE_READ_FAILED") from None
    if (not isinstance(data, dict) or data.get("file_id") != file_id or
            not isinstance(data.get("content"), str) or not isinstance(data.get("truncated"), bool) or
            not isinstance(data.get("name"), str) or not isinstance(data.get("mime_type"), str) or
            not isinstance(data.get("size"), int) or isinstance(data.get("size"), bool) or
            not 0 <= data["size"] <= 2 * 1024 * 1024 or
            len(data["content"].encode()) > body["max_content_bytes"]):
        raise FileReaderError("FILE_READ_FAILED")
    # Whitelist response fields; error bodies, credentials and arbitrary fields
    # returned by a misconfigured upstream never reach the model.
    result = {k: data[k] for k in ("file_id", "name", "mime_type", "size", "truncated", "content")}
    return json.dumps(result, ensure_ascii=False)
