import asyncio
import hmac
import logging
import os
import sqlite3
from pathlib import Path
from contextlib import AsyncExitStack, asynccontextmanager
from fastapi import Depends, FastAPI, HTTPException, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver
from .graph import build_graph, invoke_turn
from .memory import ThreadLocks
from .schema import GenerateRequest, GenerateResponse

logger = logging.getLogger("agent_runtime")


def create_app(graph=None, token=None, timeout=80, checkpoint_path=None, model_factory=None):
    secret = token if token is not None else os.getenv("AGENT_RUNTIME_TOKEN", "")
    runner = graph
    locks = ThreadLocks()
    slots = asyncio.Semaphore(4)

    @asynccontextmanager
    async def lifespan(_app):
        if not secret:
            raise RuntimeError("AGENT_RUNTIME_TOKEN must be configured")
        nonlocal runner
        async with AsyncExitStack() as stack:
            if graph is None:
                try:
                    path = Path(checkpoint_path or os.getenv("CHECKPOINT_DB", "./data/checkpoints.sqlite"))
                    path.parent.mkdir(parents=True, exist_ok=True)
                    saver = await stack.enter_async_context(AsyncSqliteSaver.from_conn_string(str(path)))
                    await saver.setup()
                    runner = build_graph(checkpointer=saver, **({"model_factory": model_factory} if model_factory else {}))
                except (OSError, sqlite3.Error) as exc:
                    logger.error("checkpoint initialization failed error_type=%s", type(exc).__name__)
                    runner = None
            yield
            runner = graph

    app = FastAPI(lifespan=lifespan, docs_url=None, redoc_url=None, openapi_url=None)

    async def authenticate(request: Request):
        supplied = request.headers.get("Authorization", "")
        if not secret or not hmac.compare_digest(supplied.encode(), ("Bearer " + secret).encode()):
            raise HTTPException(401, "unauthorized")

    @app.exception_handler(RequestValidationError)
    async def invalid_request(_request, _exc):
        # FastAPI's default errors can echo submitted context; keep errors generic.
        return JSONResponse(status_code=400, content={"error": {"code": "INVALID_REQUEST"}})

    @app.get("/healthz")
    async def health():
        return {"status": "ok" if runner is not None else "degraded"}

    @app.post("/v1/generate", response_model=GenerateResponse, dependencies=[Depends(authenticate)])
    async def generate(request: Request):
        # Limit before JSON/Pydantic decoding, including chunked requests.
        body = bytearray()
        async for chunk in request.stream():
            body.extend(chunk)
            if len(body) > 512 * 1024:
                raise HTTPException(413, "request too large")
        try:
            req = GenerateRequest.model_validate_json(body)
        except ValueError:
            raise HTTPException(400, "invalid request") from None

        async def invoke():
            if runner is None:
                raise HTTPException(503, "checkpoint unavailable")
            async with locks.hold(req.thread_id):
                async with slots:
                    return await invoke_turn(runner, req)

        async def disconnected():
            while True:
                await asyncio.sleep(0.1)
                if await request.is_disconnected():
                    return

        task = asyncio.create_task(invoke())
        watcher = asyncio.create_task(disconnected())
        try:
            done, _ = await asyncio.wait({task, watcher}, timeout=timeout,
                                         return_when=asyncio.FIRST_COMPLETED)
            if watcher in done:
                raise HTTPException(499, "request cancelled")
            if task not in done:
                raise HTTPException(504, "generation timed out")
            return task.result()["response"]
        except HTTPException:
            raise
        except (sqlite3.Error, OSError) as exc:
            logger.error("checkpoint failed error_type=%s", type(exc).__name__)
            raise HTTPException(503, "checkpoint unavailable") from None
        except Exception as exc:
            # Do not log SDK exception text: it can contain credentials or payloads.
            logger.error("generation failed provider=%s error_type=%s", req.provider, type(exc).__name__)
            raise HTTPException(502, "generation failed") from None
        finally:
            task.cancel()
            watcher.cancel()
            await asyncio.gather(task, watcher, return_exceptions=True)

    return app


app = create_app()
