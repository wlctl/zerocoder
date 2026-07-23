#!/bin/sh
# status-la.sh — статус продукта LogAnalyzer (la).
# Проверяет живость backend (он раздаёт API + статический frontend).

set -eu

LA_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LA_PID_DIR="${LA_PID_DIR:-"$LA_ROOT/sh/.run"}"
LA_PID_FILE="$LA_PID_DIR/la-backend.pid"

if [ -f "$LA_PID_FILE" ]; then
  pid="$(cat "$LA_PID_FILE")"
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    echo "backend: RUNNING (pid=$pid)"
    echo "ИТОГ: la RUNNING"
    exit 0
  else
    echo "backend: STOPPED (устаревший PID-файл, pid=$pid)"
  fi
else
  echo "backend: STOPPED (PID-файл отсутствует)"
fi

echo "ИТОГ: la STOPPED"
exit 1