import json
import os
import httpx
from langchain_core.tools import tool
from pydantic import BaseModel, Field


class SearchInput(BaseModel):
    query: str = Field(min_length=1, max_length=512)


@tool(args_schema=SearchInput)
async def web_search(query: str) -> str:
    """Search the web for current information. Return source titles, URLs and snippets."""
    key = os.getenv("BRAVE_SEARCH_API_KEY")
    if not key:
        raise RuntimeError("search unavailable")
    async with httpx.AsyncClient(timeout=10, follow_redirects=False) as client:
        async with client.stream("GET", "https://api.search.brave.com/res/v1/web/search",
                                 params={"q": query, "count": 5},
                                 headers={"X-Subscription-Token": key, "Accept": "application/json"}) as response:
            response.raise_for_status()
            body = bytearray()
            async for chunk in response.aiter_bytes():
                body.extend(chunk)
                if len(body) > 1024 * 1024:
                    raise ValueError("search response too large")
    data = json.loads(body)
    results = [{"title": str(item.get("title", ""))[:512],
                "url": str(item.get("url", ""))[:2048],
                "snippet": str(item.get("description", ""))[:2048]}
               for item in data.get("web", {}).get("results", [])[:5]]
    return json.dumps({"results": results}, ensure_ascii=False)
