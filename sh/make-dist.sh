#!/bin/sh
# make-dist.sh — сборка ZIP-дистрибутива LogAnalyzer (la).
#
# Дистрибутив (NFR): только ZIP, без инсталлятора; в runtime один процесс backend
# раздаёт API и статический frontend; Node.js в runtime не нужен (Angular собран
# build-time, в поставку входит собранный JavaScript).
#
# Состав ZIP (la-<version>/):
#   devagent                  — бинарник backend (Go)
#   la.conf.template          — шаблон конфигурации (для справки)
#   frontend/browser/...      — собранный Angular (статика)
#   parsers/*.so              — плагины-парсеры
#   postprocessors/*.so       — плагины-постобработчики
#   sh/start-la.sh stop-la.sh status-la.sh  — управление процессом (dist-относительные пути)
#   docs/README.md INSTALL.md USAGE.md      — документация
#
# Запуск: sh/make-dist.sh [version]
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO="${GO:-/usr/local/go/bin/go}"
BE="$ROOT/release/backend/go"
FE="$ROOT/release/frontend"

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  VERSION="$(grep -m1 'const version =' "$BE/cmd/devagent/main.go" | sed -E 's/.*"([^"]+)".*/\1/')"
fi
[ -n "$VERSION" ] || { echo "не удалось определить версию"; exit 1; }

DISTNAME="la-$VERSION"
STAGE="$BE/build/dist/$DISTNAME"
OUTDIR="$ROOT/release/dist"
ZIP="$OUTDIR/$DISTNAME.zip"

echo "==> сборка backend + плагинов"
( cd "$BE" && "$GO" build -o "$BE/build/devagent" ./cmd/devagent )
( cd "$BE" && make plugins >/dev/null )

echo "==> подготовка stage: $STAGE"
rm -rf "$STAGE"
mkdir -p "$STAGE/frontend" "$STAGE/parsers" "$STAGE/postprocessors" "$STAGE/sh" "$STAGE/docs"

# Бинарник.
cp "$BE/build/devagent" "$STAGE/devagent"

# Плагины.
cp "$BE"/parsers/*.so "$STAGE/parsers/" 2>/dev/null || true
cp "$BE"/postprocessors/*.so "$STAGE/postprocessors/" 2>/dev/null || true

# Фронтенд: собранная статика (Angular dist browser/).
if [ -d "$FE/dist/la-frontend/browser" ]; then
  cp -R "$FE/dist/la-frontend/browser" "$STAGE/frontend/browser"
else
  echo "WARN: Angular dist не собран (ng build); UI недоступен, API работает."
fi

# Шаблон конфигурации (для справки; la.conf создаётся из встроенного в бинарник).
cp "$BE/internal/config/la.conf.template" "$STAGE/la.conf.template"

# Документация.
cp "$ROOT"/release/docs/*.md "$STAGE/docs/"

# Dist-адаптированные скрипты управления (env задаёт dist-относительные пути;
# env > la.conf, поэтому правка la.conf для базового запуска не нужна).
cat > "$STAGE/sh/start-la.sh" <<'EOF'
#!/bin/sh
# start-la.sh (dist) — старт LogAnalyzer. Один процесс backend раздаёт API и
# статический frontend. Node.js в runtime не нужен.
set -eu
LA_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LA_PID_DIR="${LA_PID_DIR:-"$LA_ROOT/sh/.run"}"
LA_PID_FILE="$LA_PID_DIR/la-backend.pid"
mkdir -p "$LA_PID_DIR"

is_running() { [ -n "$1" ] && kill -0 "$1" 2>/dev/null; }
if [ -f "$LA_PID_FILE" ] && is_running "$(cat "$LA_PID_FILE")"; then
  echo "la уже запущен (pid=$(cat "$LA_PID_FILE")). Сначала sh/stop-la.sh."
  exit 0
fi

# dist-относительные пути (env перекрывают la.conf).
export LA_FRONTEND_DIST="$LA_ROOT/frontend/browser"
export LA_PARSERS_DIR="$LA_ROOT/parsers"
export LA_POSTPROCESSORS_DIR="$LA_ROOT/postprocessors"
export SOURCE_DB_URL="sqlite:$LA_ROOT/la.db"

cd "$LA_ROOT"
if [ -x "$LA_ROOT/devagent" ]; then
  ./devagent &
  echo $! > "$LA_PID_FILE"
  echo "backend запущен (pid=$!), cwd=$LA_ROOT"
  echo "URL: http://localhost:8888  (порт — LISTEN_PORT в la.conf или env)"
else
  echo "ERROR: бинарник не найден/не исполняемый: $LA_ROOT/devagent"
  exit 1
fi
echo "la стартовал. Статус: sh/status-la.sh"
EOF
chmod +x "$STAGE/sh/start-la.sh"

cat > "$STAGE/sh/stop-la.sh" <<'EOF'
#!/bin/sh
# stop-la.sh (dist) — останов LogAnalyzer.
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
EOF
chmod +x "$STAGE/sh/stop-la.sh"

cat > "$STAGE/sh/status-la.sh" <<'EOF'
#!/bin/sh
# status-la.sh (dist) — статус LogAnalyzer.
set -eu
LA_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LA_PID_FILE="${LA_PID_DIR:-"$LA_ROOT/sh/.run"}/la-backend.pid"
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
EOF
chmod +x "$STAGE/sh/status-la.sh"

echo "==> упаковка ZIP: $ZIP"
mkdir -p "$OUTDIR"
rm -f "$ZIP"
( cd "$BE/build/dist" && zip -qr "$ZIP" "$DISTNAME" )

echo "==> готово: $ZIP"
unzip -l "$ZIP" | tail -n +4 | head -40
echo "..."
echo "размер: $(du -h "$ZIP" | cut -f1)"