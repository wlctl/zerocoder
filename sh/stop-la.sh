#!/bin/sh
# stop-la.sh — останов продукта LogAnalyzer (la).
# Останавливает backend (он же раздаёт статический frontend), очищает .run/.

set -eu

LA_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LA_PID_DIR="${LA_PID_DIR:-"$LA_ROOT/sh/.run"}"
LA_PID_FILE="$LA_PID_DIR/la-backend.pid"

if [ -f "$LA_PID_FILE" ]; then
  pid="$(cat "$LA_PID_FILE")"
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    echo "backend остановлен (pid=$pid)"
  else
    echo "backend: процесс неактивен (pid=$pid)"
  fi
  rm -f "$LA_PID_FILE"
else
  echo "backend: PID-файл отсутствует"
fi

echo "la остановлен."