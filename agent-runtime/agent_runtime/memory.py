import asyncio
from contextlib import asynccontextmanager


class ThreadLocks:
    """Serialize state read + execution per conversation in this single worker."""
    def __init__(self):
        self.entries = {}

    @asynccontextmanager
    async def hold(self, thread_id):
        lock, users = self.entries.get(thread_id, (asyncio.Lock(), 0))
        self.entries[thread_id] = (lock, users + 1)
        try:
            async with lock:
                yield
        finally:
            _, users = self.entries[thread_id]
            if users == 1:
                del self.entries[thread_id]
            else:
                self.entries[thread_id] = (lock, users - 1)
