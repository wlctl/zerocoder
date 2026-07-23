# LogAnalyzer (la)

**LogAnalyzer (la)** — настольное приложение для инженеров технической поддержки и аналитиков по загрузке, разбору и визуализации лог-файлов заказчиков. Поставка — один ZIP-архив без инсталлятора; в runtime работает один процесс backend, раздающий REST API и статический frontend. Внешних зависимостей на ПК пользователя нет (только ОС Linux).

> Текущая версия: **0.6.0**. См. [changelog/CHANGELOG.md](changelog/CHANGELOG.md).

---

## Какую задачу решает

Логи заказчиков приходят в разнородных форматах (Oracle alert.log, WebLogic `.log`/`.out`, Java/log4j с многострочными stack-trace, apache access, Oracle Diagnostic Log, обычный текст), в разных кодировках (включая Windows-1251/1252, ISO-8859), с разными форматами дат и таймзон. Тех.поддержке нужно быстро:

- загрузить пачку файлов или zip-архив;
- автоматически разобрать их в структурированные записи с корректным `timestamp`, `level`, `component`, `message` (многострочные stack-trace — одной записью);
- получить сводку по каждому файлу (число записей, кодировка, first/last ts, длительность, распределение по уровням критичности, сессии старт-стоп);
- искать, фильтровать по времени, подсвечивать строки, видеть всплески событий на графике;
- **коррелировать события из разных файлов на общем таймлайне** (кто что делал одновременно);
- сохранять пресеты просмотра, аннотации/пин-точки, искать по регуляркам и атрибутам.

LogAnalyzer делает это в одном локальном процессе, без сервера БД и без Node.js в runtime.

## Аудитория

- **Инженеры технической поддержки** — разбор логов заказчиков, поиск ошибок, воспроизведение инцидентов по времени.
- **Аналитики** — частотный анализ, всплески событий, корреляция между компонентами/файлами, аннотирование находок.
- **Разработчики backend-систем (Oracle/WebLogic/Java)** — быстрая локализация проблем по stack-trace и сессиям старт-стоп.

## Функционал

### Загрузка и ингестия
- Загрузка **нескольких файлов или zip-архива одновременно** (multipart, drag&drop).
- Лимиты `MAX_FILE_SIZE` (по умолчанию 10GB) и `MAX_FILE_COUNT` (10; zip считается за 1).
- **Дедупликация по MD5** входящего файла — повторная загрузка возвращает `duplicate` с указанием существующего `upload_id` (не прерывает остальные файлы).
- Рекурсивная распаковка zip по подкаталогам (только лог-файлы); архив не хранится.
- **Автоопределение формата** по содержимому; не определился → `text` (построчно + попытка даты/уровня). Конфиг-файлы также просматриваемы.
- Многострочные stack-trace склеиваются в **одну запись** (`raw_line` TEXT с переносами); `ts`/`level` — из головной строки.
- **Даты в разных форматах и локалях**: каталог layout-ов + локализованные имена месяцев; явный offset → UTC, TZ-аббревиатуры (MSK/EST) неоднозначны → `LA_DEFAULT_TZ` (UTC, изменяемый); не разобрана → `ts=NULL`, оригинал сохраняется в `ts_raw`.
- Определение кодировки (chardet по первым ~64KB) с декодерами Windows-1251/1252/ISO-8859.

### Постобработка (сводка по файлу)
- **Базовый постобработчик** (built-in, всегда есть): число записей, дата загрузки, размер, кодировка, first/last ts, длительность, `level_counts`.
- **Форматные постобработчики** наследуют от базового и расширяют: число сессий старт-стоп, длительности интервалов старт-стоп, число сообщений в категориях (error/warning/critical/…).
- Сводка хранится в `t_files_analyze` (`encoding`/`first_ts`/`last_ts`/`duration_sec` + `summary` JSON).

### Просмотр и анализ (frontend)
- **Таблица загрузок** (сортируемая/фильтруемая) с удалением и каскадной очисткой результатов; агрегаты под таблицей (размер хранилища, число загрузок/файлов/зисей).
- **Drill-in** в просмотр: шапка деталей архива/файла.
- **Селектор файлов** с чекбоксами (все выбраны по умолчанию).
- **Мульти-поиск** по всем полям (+опц. исходное `raw`-поле) по всем файлам селектора; фильтров несколько, каждый удаляется независимо.
- **Таймлайн** min/max с перемещаемыми границами (dual-thumb слайдер, epoch-ms).
- **Подсветка строк** (текст + опц. кластерные лексемы + виджет цвета).
- **Пофайловые таблицы** по 10 строк (динамически), имя+краткая сводка над каждой; закрытие снимает чекбокс.
- **График событий** с группировкой месяц/день/час/минута (всплески).
- **Новое окно** для файла с управлением от основной страницы (открытое в окне не показывается на основной).
- **Корреляция по времени** (US-0005): объединённый кросс-файл поток записей на общем таймлайне (`ts | файл (цвет) | level | component | message | raw`), фильтры, общий график.
- **Пресеты просмотра** (US-0006): снимок состояния (фильтры/таймлайн/подсветка/выбор файлов/режим корреляции) — сохранить/загрузить/удалить.
- **Аннотации/пин-точки** (US-0006): time-pin (по `ts`) или entry-pin (по записи) + `note` + `color`; entry-pin переживает re-ингест (без FK, dangling допускается).
- **Поиск по регулярке и атрибутам** (US-0006): `mode=text|regex` (серверная `REGEXP`), `attrs=k1:v1,k2:v2` (по JSON `attrs` записей).
- **Стекированный по файлам график** (US-0006): сегменты по стабильной палитре per file-id + маркеры time-pin.
- **Персистентность** фильтров/подсветки/пресетов/аннотаций между рестартами (в БД, per upload) + кнопка очистки.

### Парсеры и постобработщики — плагинная архитектура
- Каждый парсер/постобработчик — отдельный подключаемый модуль: Go `.so` (`go build -buildmode=plugin`), Python `.py` (для Python-релиза).
- Модули лежат в `parsers/` и `postprocessors/` (пути — env `LA_PARSERS_DIR` / `LA_POSTPROCESSORS_DIR`).
- **Добавление/модификация**: собрать/положить только модуль + рестарт процесса — **без пересборки основного бинарника и без повторного развёртывания продукта** (новый ZIP не нужен).
- Встроенный `text`-парсер и базовый постобработчик всегда есть в хосте (fallback); плагин с тем же именем формата заменяет built-in.
- Общий стабильный интерфейс: Go — `internal/parser`, `internal/postprocess`; Python — `parsers/base.py`, `postprocessors/base.py`.

### Поддерживаемые форматы

| Формат | Источник | Парсер | Постобработчик |
|---|---|---|---|
| `oracle` | Oracle alert.log (ISO8601+offset) | ✓ плагин | ✓ (сессии старт-стоп) |
| `weblogic` | WebLogic `.log` (`####<…>`, UTC по epoch-millis) | ✓ плагин | ✓ (сессии старт-стоп) |
| `wls_stdout` | WLS `.out` / nodemanager | ✓ плагин | ✓ (сессии старт-стоп) |
| `java` | log4j-style (multiline stack) | ✓ плагин | base (fallback) |
| `access` | apache common log | ✓ плагин | base (fallback) |
| `odl` | Oracle Diagnostic Log | ✓ плагин | ✓ |
| `text` | fallback (построчно + дата/уровень) | built-in | base (built-in) |

## Архитектура

Два независимо подключаемых и изменяемых проекта:

- **backend** (`release/backend/`) — поставщик API. **Go первичен** (поставляется в ZIP), **Python/FastAPI вторичен** (собирается по запросу, в полном объёме функционала). Источник данных — **SQLite** (MVP, `modernc.org/sqlite`, pure-Go без CGo; авто-создание файла и схемы при старте); Postgres — позже, отдельной ЮС.
- **frontend** (`release/frontend/`) — **Angular 20** (standalone, routing, SCSS). Компилируется build-time (Angular CLI/Node) в `dist/`; в runtime раздаётся backend как статика (Go: `embed.FS`/`http.FileServer`). **Node.js в runtime не используется.**

В runtime — один процесс backend (REST API `/api/*` + статический frontend). Управление процессом — скрипты `sh/`.

```
dev-agent/
├── user-stories/      # US-NNNN, двойной формат: .md (канонический) + .json (служебный)
├── architect/         # спецификации (YAML+Mermaid), диаграммы, manifest трассировки
├── changelog/         # CHANGELOG.md (Keep a Changelog, SemVer)
├── sh/                # start-la.sh / stop-la.sh / status-la.sh / make-dist.sh
├── sample-logs/       # образцы логов (база для паттернов парсеров)
└── release/
    ├── backend/go/    # первичный backend (cmd/devagent, internal/{config,db,ingest,parser,postprocess,server}, parsers/, postprocessors/)
    ├── backend/python/# вторичный backend (FastAPI, по запросу)
    ├── frontend/      # Angular-приложение
    └── docs/          # README/INSTALL/USAGE (входят в ZIP)
```

Процесс разработки ведётся по потоку **user-stories → architect → code → build**: каждая пользовательская история хранится в двух форматах, спецификации (`architect/specs/`) — источник истины для архитектуры, трассировка `спецификация → код → бинарник` — в `architect/manifest.md`. Подробнее — в [CLAUDE.md](CLAUDE.md).

## Быстрый старт (из дистрибутива)

1. Распакуйте ZIP `la-<version>.zip` в любой каталог.
2. `sh/start-la.sh` — при первом старте создаётся `la.conf` (из шаблона) и SQLite-БД `la.db`, применяются миграции, загружаются плагины, стартует веб-сервер.
3. Откройте `http://localhost:8888`.

```sh
sh/start-la.sh     # старт
sh/status-la.sh    # статус (RUNNING/STOPPED)
sh/stop-la.sh      # останов
curl http://localhost:8888/healthz   # -> {"status":"ok"}
```

Конфигурация (`la.conf`, приоритет env > conf):

| Поле | По умолчанию | Назначение |
|---|---|---|
| `SOURCE_DB_URL` | `sqlite:la.db` | БД (SQLite для MVP) |
| `LISTEN_ADDRESS` | `localhost` | адрес HTTP |
| `LISTEN_PORT` | `8888` | порт HTTP |
| `MAX_FILE_SIZE` | `10GB` | лимит входящего файла |
| `MAX_FILE_COUNT` | `10` | число файлов за запрос (zip = 1) |
| `LA_DEFAULT_TZ` | `UTC` | таймзона для логов без смещения |
| `LA_PARSERS_DIR` | `./parsers` | каталог плагинов-парсеров |
| `LA_POSTPROCESSORS_DIR` | `./postprocessors` | каталог плагинов-постобработчиков |
| `LA_FRONTEND_DIST` | `release/frontend/dist/la-frontend/browser` | каталог собранного Angular |

Подробности — в [release/docs/INSTALL.md](release/docs/INSTALL.md) и [release/docs/USAGE.md](release/docs/USAGE.md).

## Сборка из исходников

### Backend (Go)
```sh
cd release/backend/go
/usr/local/go/bin/go build ./...          # проверка
/usr/local/go/bin/go test -race ./...     # тесты
make plugins                              # собрать .so парсеры/постобработчики
make dist                                 # собрать бинарник + ZIP-дистрибутив
```
Бинарник → `release/backend/go/build/devagent` (+ `devagent.provenance.json` с трассировкой). ZIP → `release/dist/la-<version>.zip` (сборщик `sh/make-dist.sh`).

### Frontend (Angular)
```sh
cd release/frontend
npm install          # build-time зависимость (с подтверждением)
ng build             # -> dist/la-frontend/browser/
ng test --watch=false --browsers=ChromeHeadless
```

### Требования к окружению
- **Сборка:** Go 1.26+, Node.js + Angular CLI (только build-time frontend).
- **Runtime (ПК пользователя):** только Linux (Go-плагины `.so` — Linux-only); без внешних зависимостей, без Node.js, без сервера БД.

## REST API (кратко)

- `POST /api/uploads` (multipart, multi-file) → `{results:[{upload_id, files, status, duplicate?, …}]}`
- `GET /api/uploads`, `GET /api/uploads/{id}`, `DELETE /api/uploads/{id}` (каскад)
- `GET /api/files?upload_id=`, `GET /api/files/{id}`, `GET /api/files/{id}/entries`, `POST /api/files/{id}/postprocess`, `DELETE /api/files/{id}`
- `GET /api/stats`, `GET /api/parsers`
- Viewer: `search` (`mode=text|regex`, `attrs=`), `correlate` (`files=/from=/to=/q=/level=`), `timeline`, `lexemes`, `histogram`, `histogram-by-file`, `presets` (CRUD), `annotations` (CRUD), `filters`/`highlights` (CRUD), `DELETE /api/uploads/{id}/view-state`
- `GET /healthz`

## Документация

- [CLAUDE.md](CLAUDE.md) — роль агента, workflow, требования и ограничения.
- [architect/manifest.md](architect/manifest.md) — трассировка спецификация → код → бинарник.
- [changelog/CHANGELOG.md](changelog/CHANGELOG.md) — история релизов (0.1.0 → 0.6.0).
- [release/docs/](release/docs/) — пользовательская документация (входит в ZIP).
- [user-stories/](user-stories/) — пользовательские истории US-0001…US-0006.

## Пользовательские истории (реализованные)

| US | Тема | Версия |
|---|---|---|
| US-0001 | Конфигурация и bootstrap БД | 0.1.0 |
| US-0002 | Загрузка и ингестия лог-файлов разнородных форматов | 0.2.0 |
| US-0003 | Frontend просмотрщика логов (Angular) | 0.2.0 |
| US-0004 | Backend-поддержка viewer | 0.2.0 |
| US-0005 | Корреляция событий по времени между файлами | 0.5.0 |
| US-0006 | Пресеты, аннотации, regex/attrs-поиск, стекированный график | 0.6.0 |

## Лицензия

Исходники проекта dev-agent / LogAnalyzer публикуются в репозитории для разработки. Конкретная лицензия на использование — по запросу.