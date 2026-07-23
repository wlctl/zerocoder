---
id: US-0001
title: Конфигурация и bootstrap БД при старте backend
status: ready
priority: must
domain: config
project: [backend]
target: [go]
spec_ref: architect/specs/config.spec.md
created: 2026-07-23
updated: 2026-07-23
---

# US-0001 — Конфигурация и bootstrap БД при старте backend

## Как инженер тех.поддержки/аналитик (через оператора продукта)
## я хочу запустить backend LogAnalyzer одной командой без ручной настройки БД и конфига
## чтобы продукт поднимался с нуля с разумными дефолтами и был готов принимать логи.

## Контекст

MVP-поток backend. Для простоты используется **SQLite** (Postgres — позже, отдельной ЮС).
При старте процесс читает `la.conf`. При отсутствии `la.conf` — создаёт его из шаблона
`la.conf.template`. При отсутствии SQLite-файла и объектов схемы — создаёт автоматически.
Параметры: `SOURCE_DB_URL` (по умолчанию `sqlite:la.db`), `LISTEN_ADDRESS` (по умолчанию
`localhost`), `LISTEN_PORT` (по умолчанию `8888`). Node в runtime не используется.

## Acceptance criteria

- [ ] AC-1: При старте процесс читает `la.conf` (путь по умолчанию `./la.conf`, переопределяется env `LA_CONF`).
- [ ] AC-2: Если `la.conf` отсутствует — он создаётся из встроенного шаблона `la.conf.template` с дефолтами `SOURCE_DB_URL=sqlite:la.db`, `LISTEN_ADDRESS=localhost`, `LISTEN_PORT=8888`.
- [ ] AC-3: Если поле в `la.conf` пустое/отсутствует — применяется значение по умолчанию.
- [ ] AC-4: Backend подключается к БД по `SOURCE_DB_URL`; scheme `sqlite:` открывает SQLite-файл (создаёт при отсутствии).
- [ ] AC-5: При отсутствии объектов схемы они создаются автоматически (миграции); повторный запуск не пересоздаёт существующее и не падает (идемпотентность).
- [ ] AC-6: Backend слушает HTTP на `LISTEN_ADDRESS:LISTEN_PORT` и отвечает `GET /healthz` → `{"status":"ok"}`.
- [ ] AC-7: Корректный останов по `SIGINT`/`SIGTERM` (graceful shutdown).
- [ ] AC-8: Код проверен `go build`, `go vet`, `gofmt`, `go test -race`, `go test -cover` — зелёные.

## Non-functional requirements

- SQLite-драйвер — чистый Go (`modernc.org/sqlite`), без CGo (сборка штатным `go build`, без gcc).
- Формат `la.conf` — простой `KEY=VALUE` (строки, `#` — комментарии); парсится stdlib.
- Секреты не в репозитории; `la.conf` и `la.db` — runtime/сгенерированные, в `.gitignore`.
- Postgres не реализуется в рамках этой ЮС (зарезервировать разбор scheme `postgres://`).
- Изолированные тесты через `t.TempDir()`; HTTP-проверка `/healthz` через `net/http` клиент (без curl/wget).

## Зависимости

- Нет (фундаментальная ЮС для backend).

## Открытые вопросы

- Структура таблицы `logs` — определяется в ЮС приёма/парсинга логов (миграция 0002).
- Формат/драйвер Postgres — отдельная ЮС позже.