FROM python:3.12-slim AS base
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 PIP_NO_CACHE_DIR=1
WORKDIR /app
COPY requirements.lock ./requirements.lock
RUN --mount=type=bind,source=builddeps,target=/wheels \
    if find /wheels -name '*.whl' | grep -q .; then pip install --no-index --find-links=/wheels -r requirements.lock; \
    else pip install --no-cache-dir --timeout 20 --retries 2 -r requirements.lock; fi \
    && groupadd --gid 10001 ptmove \
    && useradd --uid 10001 --gid 10001 --no-create-home ptmove \
    && mkdir /data /state && chown 10001:10001 /data /state
COPY ptmove ./ptmove

FROM base AS test
RUN --mount=type=bind,source=builddeps,target=/wheels \
    if find /wheels -name 'pytest*.whl' | grep -q .; then pip install --no-index --find-links=/wheels pytest==9.1.1; \
    else pip install --no-cache-dir --timeout 20 --retries 2 pytest==9.1.1; fi
COPY tests ./tests
COPY pyproject.toml ./pyproject.toml
CMD ["python", "-m", "pytest", "-q", "-p", "no:cacheprovider"]

FROM base AS runtime
USER 10001:10001
ENV PTMOVE_DATA_DIR=/data PTMOVE_WORKER_STATE_DIR=/state
EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8000/health',timeout=3)"
CMD ["uvicorn", "ptmove.api:app", "--host", "0.0.0.0", "--port", "8000", "--no-access-log"]
