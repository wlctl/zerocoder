#!/bin/sh
# start-la.sh — старт продукта LogAnalyzer (la).
#
# Runtime: ОДИН процесс backend раздаёт и API, и статический frontend (Angular
# сборка dist/). Node в runtime НЕ используется — Angular компилируется
# build-time (Angular CLI/Node) в release/frontend/dist/, затем dist/ монтируется
# backend (Go: embed.FS/http.FileServer; Python/FastAPI: StaticFiles).
#
# Какой backend запускать (Go или Python) — задаётся LA_BACKEND и конфигурацией
# (детали — в user-stories). Скрипт — каркас с PID-управлением; раздел LAUNCH
# дополняется по мере реализации US.

set -eu

LA_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LA_PID_DIR="${LA_PID_DIR:-"$LA_ROOT/sh/.run"}"
LA_PID_FILE="$LA_PID_DIR/la-backend.pid"

LA_BACKEND="${LA_BACKEND:-go}"                       # go | python
LA_BACKEND_BIN="${LA_BACKEND_BIN:-"$LA_ROOT/release/backend/go/build/devagent"}"
LA_PYTHON_APP="${LA_PYTHON_APP:-"$LA_ROOT/release/backend/python/src"}"
LA_FRONTEND_DIST="${LA_FRONTEND_DIST:-"$LA_ROOT/release/frontend/dist"}"

mkdir -p "$LA_PID_DIR"

is_running() {
  pid="$1"
  [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null
}

if [ -f "$LA_PID_FILE" ] && is_running "$(cat "$LA_PID_FILE")"; then
  echo "la уже запущен (backend pid=$(cat "$LA_PID_FILE")). Сначала sh/stop-la.sh."
  exit 0
fi

# frontend проверяется как статика (собрана build-time, без Node в runtime)
if [ -d "$LA_FRONTEND_DIST" ]; then
  echo "frontend: найдена сборка $LA_FRONTEND_DIST (раздаётся backend как static)"
else
  echo "WARN: frontend dist не найден: $LA_FRONTEND_DIST (соберите Angular build-time). UI недоступен, API работает."
fi

# --- LAUNCH BACKEND -----------------------------------------------------------
# Запуск из LA_ROOT, чтобы la.conf и la.db жили в корне проекта.
# (Путь к конфигу дополнительно переопределяется env LA_CONF.)
cd "$LA_ROOT"
case "$LA_BACKEND" in
  go)
    if [ -x "$LA_BACKEND_BIN" ]; then
      "$LA_BACKEND_BIN" &
      echo $! > "$LA_PID_FILE"
      echo "backend (go) запущен (pid=$!), cwd=$LA_ROOT"
    else
      echo "ERROR: backend-бинарник не найден/не исполняемый: $LA_BACKEND_BIN"
      exit 1
    fi
    ;;
  python)
    echo "TODO(US): запуск python backend (uvicorn/fastapi + StaticFiles) не реализован"
    echo "       задайте LA_BACKEND=go или реализуйте по US"
    exit 1
    ;;
  *)
    echo "ERROR: неизвестный LA_BACKEND='$LA_BACKEND' (ожидался go|python)"
    exit 1
    ;;
esac

echo "la стартовал. Статус: sh/status-la.sh"