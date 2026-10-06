FROM python:3.12-slim
WORKDIR /app
COPY requirements.lock .
RUN pip install --no-cache-dir -r requirements.lock
COPY agent_runtime ./agent_runtime
RUN mkdir -p /app/data && chown 65534:65534 /app/data
ENV CHECKPOINT_DB=/app/data/checkpoints.sqlite
USER 65534:65534
EXPOSE 8000
CMD ["uvicorn", "agent_runtime.app:app", "--host", "0.0.0.0", "--port", "8000"]
