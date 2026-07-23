# Архитектурный манифест — трассировка (provenance)

Этот файл — единый реестр соответствия **спецификация → код → бинарный образ**.
Архитектурные артефакты (моя спецификация в `architect/specs/` и пользовательский формат) должны совпадать и отражать код и собранный бинарник. Любое изменение в коде/бинарнике обязано сопровождаться обновлением соответствующей строки здесь и записи в `changelog/CHANGELOG.md`.

Продукт **LogAnalyzer (la)** состоит из двух независимо подключаемых и изменяемых проектов: **backend** (`release/backend/`, Go первично / Python вторично) и **frontend** (`release/frontend/`, Angular). Frontend компилируется build-time (Angular CLI/Node) в `dist/` и в runtime раздаётся backend как статика (Go: `embed.FS`/`http.FileServer`; Python: `StaticFiles`) — Node в runtime не используется. В runtime работает один процесс backend. Управление процессом — скрипты `sh/`.

## Схема трассировки

| Спецификация | Домен/компонент | Проект | Файлы кода (Go) | Файлы кода (Python) | Файлы кода (Frontend) | Бинарник/артефакт | Версия | Статус |
|---|---|---|---|---|---|---|---|---|
| `architect/specs/<domain>.spec.md` | _описание_ | backend \| frontend | `release/backend/go/...` | `release/backend/python/...` | `release/frontend/...` | `release/backend/go/build/<name>` \| `release/backend/python/build/<name>` \| `release/frontend/dist/` | _SemVer_ | draft \| active \| deprecated |

## Записи

| Спецификация | Домен/компонент | Проект | Файлы кода (Go) | Файлы кода (Python) | Файлы кода (Frontend) | Бинарник/артефакт | Версия | Статус |
|---|---|---|---|---|---|---|---|---|
| `architect/specs/config.spec.md` | `config` (конфиг + bootstrap БД) | backend | `release/backend/go/cmd/devagent/main.go`, `internal/config/*`, `internal/db/*`, `internal/server/*` | — | — | `release/backend/go/build/devagent` | 0.1.0 | active |
| `architect/specs/ingestion.spec.md` | `ingestion` (загрузка/парсинг/постобработка) | backend | `release/backend/go/internal/ingest/*`, `internal/parser/*`, `internal/postprocess/*`, `internal/server/handlers_ingest.go`, `internal/db/migrations.go`, `parsers/<fmt>/main.go` (.so), `postprocessors/<fmt>/main.go` (.so) | — | — | `release/backend/go/build/devagent` + `parsers/*.so` + `postprocessors/*.so` | 0.5.0 | active |
| `architect/specs/viewer.spec.md` (spec_version 0.3.0) | `viewer` (просмотр/анализ + корреляция US-0005 + пресеты/аннотации/regex-attrs/стекированный график US-0006) | frontend, backend | `release/backend/go/internal/server/handlers_viewer.go` (`getCorrelate`/regex-attrs/`getHistogramByFile`/presets/annotations CRUD + viewer-эндпоинты), `internal/server/helpers.go` (`scanFileRows` 16-col), `internal/db/db.go` (REGEXP `sync.Once` + DSN pragmas), `internal/db/migrations.go` (0004), `internal/server/server.go` (routes) | — | `release/frontend/src/app/*` (incl. `pages/viewer/`, `pages/file-window/`, `components/{correlate-table,stacked-chart,annotation-panel,preset-bar}/`, `components/palette.ts`) | `release/backend/go/build/devagent` + `release/frontend/dist/la-frontend/browser/` | 0.6.0 | active |

> Трассировка US-0001 → `architect/specs/config.spec.md` (spec_version 0.1.0) → код Go → бинарник `0.1.0`. Provenance: `release/backend/go/build/devagent.provenance.json`. Проверено: `go build/vet/test -race/cover` зелёные, smoke `sh/start-la.sh`→`/healthz`→`sh/stop-la.sh`.

> Трассировка US-0002 → `architect/specs/ingestion.spec.md` (spec_version 0.1.0) → код Go (`internal/ingest`, `internal/parser`, `internal/postprocess`, миграции 0002) → бинарник `0.2.0`. US-0003 → `architect/specs/viewer.spec.md` (frontend секция) → Angular `release/frontend/src/app/*` → `dist/la-frontend/browser/` (`ng build`/`ng test` зелёные). US-0004 → `architect/specs/viewer.spec.md` (backend секция) → `internal/server/handlers_viewer.go`, миграция 0003 (`t_view_filters`/`t_view_highlights`) → бинарник `0.2.0`. Provenance: `release/backend/go/build/devagent.provenance.json` (0.2.0). Проверено: `go build/vet/gofmt/test -race/cover` зелёные; `ng build`/`ng test` (10/10) зелёные; end-to-end smoke (upload→parse→postprocess→summary→stats/timeline/histogram/lexemes/entries/search/filters/highlights/dedup409; static+SPA-fallback) зелёный.

> **0.3.0** — релиз плагинной поставки и ZIP-дистрибутива (review-пункт #7). Плагины `.so` (6 парсеров + 4 постобработчика) собраны из `release/backend/go/{parsers,postprocessors}/<fmt>/main.go` (`go build -buildmode=plugin -tags plugin`, тот же модуль/тулчейн) и входят в ZIP `release/dist/la-0.3.0.zip` вместе с бинарником 0.3.0, `dist/` frontend, `la.conf.template`, dist-скриптами `sh/` и `docs/`. Сборщик — `sh/make-dist.sh`. Backend `POST /api/uploads` поддерживает несколько файлов одновременно (review-пункт #2). Provenance: `devagent.provenance.json` (0.3.0, sha256 `5a14a2a2…`, zip sha256 `fc7fc4c2…`). Проверено: `go build/vet/gofmt/test -race` зелёные; ZIP smoke (распаковка → start → 10 `.so` `plugin.Open` без ошибок → upload weblogic 92 записи/summary/сессия → dedup → multi-file 2-в-1 → static+SPA-fallback → delete 204) — 15/15.

> **0.4.0** — завершение UX-заглушек viewer (US-0003 ФР-5/ФР-9; review-сценарий #1). Frontend: `FileTableComponent` теперь реагирует на смену `from/to/searchQ` через `ngOnChanges` (фикс MVP-бага — таймлайн/поиск не фильтровали таблицы); timeline dual-thumb drag-слайдер поверх `[min_ts…max_ts]` (epoch-ms, live-подпись, коммит на `change`); «новое окно» — реальный рендер через новый роут `window/:fileId` → `FileWindowComponent` (`getFile`+`highlights`+`app-file-table`) с управлением из основной страницы. Спека `viewer.spec.md` уже соответствовала (код догнал). Бинарь 0.4.0 (sha256 `5da2727f…`), ZIP `release/dist/la-0.4.0.zip` (sha256 `4e4d7643…`; устаревший 0.3.0 zip удалён). Проверено: `ng build`/`ng test` (14/14) зелёные; ZIP smoke 8/8 (`/entries?from=&to=` 1-сек окно 92→5, `/window/:fileId`→`<app-root>`). #2 (frontend multi-file UI) и #4 (env-override) — отложены пользователем.

> **0.5.0** — корреляция событий по времени между файлами (US-0005). Backend: `GET /api/uploads/{id}/correlate` — объединённый кросс-файл поток записей, `JOIN t_log_entries × t_files_analyze` за `filename`, `ORDER BY ts IS NULL, ts, seq` (записи без `ts` — в конце); фильтры `files`/`from`/`to`/`q`/`level`; пагинация `{items,total,limit,offset}` (limit ≤ 1000); рамки одной загрузки. Frontend: режим «Корреляция (общий таймлайн)» — переключатель в `ViewerComponent` заменяет пофайловые таблицы на `CorrelateTableComponent` (`components/correlate-table/`) — объединённая таблица `ts|файл(цвет)|level|component|message|raw` по выбранным файлам + таймлайн-окно + поиск + подкраска + общий график `/histogram?files=…`; пагинация; цвет файла — стабильная палитра по индексу. `ApiService.correlate` + модели `CorrelatedEntry`/`CorrelatedPage`. Спека `viewer.spec.md` обновлена до `spec_version 0.2.0` (`us_ref +US-0005`, секция `frontend.screens.viewer.correlation`, эндпоинт `correlate` в `backend.new_endpoints`, note про JOIN/`ORDER BY ts IS NULL`, узел `X[Режим Корреляция]` в Mermaid). Бинарь 0.5.0 (sha256 `6f77ec9b…`), ZIP `release/dist/la-0.5.0.zip` (sha256 `8e98ebb9…`; пред. 0.4.0 zip сохранён). Provenance: `devagent.provenance.json` (0.5.0). Проверено: `go build/vet/gofmt/test -race` зелёные (`TestCorrelate`: 2-файловый zip weblogic+java, чередование по ts, `filename`, `from/to`, subset, пустая выборка); `ng build`/`ng test` (17/17) зелёные; ZIP smoke 6/6 (порядок `[a.log,b.log,a.log]`, фильтры `from/to` и `q`, SPA-fallback).

> **0.6.0** — пресеты просмотра, аннотации/пин-точки, regex/attrs-поиск, стекированный по файлам график (US-0006) + хотфикс 500 + переименование UI. Backend: миграция 0004 (`t_view_presets`/`t_annotations`, FK CASCADE по `upload_id`; entry-pin `file_analyze_id`/`entry_id` БЕЗ FK — dangling допускается); `REGEXP` скалярная функция (`modernc RegisterScalarFunction`, package-level `sync.Once` + `sync.Map`-кеш) + `mode=regex`/`attrs` в `search`/`correlate` (предикаты в общем `where` — COUNT-safe; невалидный паттерн → 400); `GET /api/uploads/{id}/histogram-by-file` (`GROUP BY bucket, file_analyze_id`); presets/annotations CRUD с валидацией пинов; DSN `?_pragma=foreign_keys(1)…` на каждом соединении пула (фикс footer aggregates); `scanFileRows` выровнен под 16 колонок `getFiles`-SELECT (фикс 500 на `GET /api/files`). Frontend: `StackedChartComponent` (`components/stacked-chart/`, сегменты по `FILE_PALETTE` + time-pin маркеры через `bucketOf`), `AnnotationPanelComponent`, `PresetBarComponent`, `palette.ts` (извлечённая `FILE_PALETTE`); viewer wiring (сигналы `presets`/`annotations`/`histByFile` + `effect`, `savePreset`/`loadPreset`, `onPinEntry` 📌, `searchMode`/`searchAttrs`); кнопка 📌 в строках `file-table`/`correlate-table`. Спека `viewer.spec.md` → `spec_version 0.3.0` (`us_ref +US-0006`, секции `presets`/`annotations`, mode/attrs, `histogram-by-file`, миграция 0004 + таблицы, notes REGEXP/`json_extract`/COUNT-safe/dangling, узлы `PR/AN/SC` в Mermaid). US-0006 — двойной формат `user-stories/US-0006-*.{md,json}`. Бинарь 0.6.0 (sha256 `9ba67b34…`, 23205624 байт), ZIP `release/dist/la-0.6.0.zip` (sha256 `cb97ec8f…`, 41246420 байт; пред. 0.5.0 zip сохранён). Provenance: `devagent.provenance.json` (0.6.0). Проверено: `go build/vet/gofmt/test -race` зелёные (9 новых/расширенных тестов); `ng build`/`ng test` (40/40) зелёные; ZIP smoke (oracle alert.log 6217 зап. → `GET /api/files` 200 непустой [регрессия 500] → regex `ORA-[0-9]+` 91 hit → `[bad(` 400 → correlate attrs COUNT-safe → histogram-by-file sum=record_count → time-pin/entry-pin 201 + mixed 400 → preset 201 → DELETE upload 204 + footer aggregates ↓ → CASCADE `[]` → SPA-fallback `<app-root>`) — зелёные.

## Управление процессом

Старт/останов/статус продукта — скрипты `sh/start-la.sh`, `sh/stop-la.sh`, `sh/status-la.sh` (PID-файл `sh/.run/la-backend.pid`, запуск backend; backend раздаёт API и статический frontend `dist/`). Node в runtime не используется. Скрипты развиваются вместе с пользовательскими историями, определяющими runtime (порты, env, команды запуска Go/Python backend).

## Поставка и развёртывание (NFR)

- Дистрибутив для ПК пользователя — **только ZIP-архив**; инсталлятора нет.
- Node.js — **только build-time** (сборка Angular → `dist/`); в поставке собранный JS, Node.js не поставляется и не устанавливается на ПК пользователя.
- Содержимое ZIP: бинарник backend (Go; Python — по запросу), `dist/` frontend, `la.conf.template`, `sh/*.sh`.
- После распаковки: при необходимости создаётся `la.conf` из шаблона → `sh/start-la.sh` → первый запуск создаёт SQLite `la.db` + миграции → стартует веб-сервер со смонтированным Angular-приложением. Внешних зависимостей на ПК пользователя нет (только ОС).
- (Артефакт сборки/упаковки ZIP — `sh/make-dist.sh` → `release/dist/la-<version>.zip`; реализован в 0.3.0.)

## Парсеры (плагинная архитектура)

- Парсеры логов — подключаемые модули в подкаталоге `parsers/` (env `LA_PARSERS_DIR`): Go — `.so` (`go build -buildmode=plugin`), Python — `.py`.
- Добавление/модификация парсера: собирается/кладётся только модуль парсера в `parsers/`, затем рестарт процесса (допустим) — подхватывается при старте сканированием `parsers/`; **без пересборки основного бинарника и без повторного развёртывания всего продукта**.
- Общий стабильный интерфейс: Go — `release/backend/go/internal/parser`; Python — `release/backend/python/parsers/base.py`.
- Ограничения Go-плагинов: только Linux; один тулчейн + одинаковые версии общих зависимостей (парсеры в том же модуле); выгрузка `.so` в работающем процессе невозможна — смена/добавление парсера через рестарт (допустим), без пересборки основного бинарника и повторного развёртывания продукта.
- Манифест парсеров (тип лога → модуль) и трассировка — в `architect/specs/ingestion.spec.md` (по реализации).

## Постобработчики (плагинная архитектура)

- Постобработчики — этап после парсинга (параллельно парсерам): подключаемые модули в подкаталоге `postprocessors/` (env `LA_POSTPROCESSORS_DIR`): Go — `.so` (`go build -buildmode=plugin`), Python — `.py`.
- Базовый постобработчик (built-in, всегда есть): общие поля — число записей, дата загрузки, размер, кодировка (chardet по ~64KB), first/last ts, длительность, `level_counts`.
- Форматные постобработчики наследуют от базового и расширяют (минимум): число сессий старт-стоп, длительности интервалов старт-стоп, число сообщений в категориях (error/warning/critical/...). Правила старт/стоп per формат — в `architect/specs/ingestion.spec.md` → `postprocessors.rules_by_format`.
- Если плагина для формата нет — применяется built-in базовый постобработчик.
- Добавление/модификация постобработчика: только модуль в `postprocessors/` + рестарт процесса (допустим) — подхватывается при старте; **без пересборки основного бинарника и без повторного развёртывания продукта**.
- Общий стабильный интерфейс: Go — `release/backend/go/internal/postprocess`; Python — `release/backend/python/postprocessors/base.py`.
- Сводка хранится в `t_files_analyze` (колонки `encoding`/`first_ts`/`last_ts`/`duration_sec` + `summary` JSON); доступ — `GET /api/files/{id}`, `POST /api/files/{id}/postprocess`.
- Ограничения Go-плагинов — те же, что у парсеров.
- Трассировка (формат → модуль постобработчика) — в `architect/specs/ingestion.spec.md` → `postprocessors` (по реализации).

## Правила ведения

1. Каждой спецификации присваивается домен и версия (`spec_version` в YAML-блоке файла спецификации).
2. Каждое сгенерированное дерево кода содержит заголовок-комментарий со ссылкой на спецификацию и её версию.
3. При сборке бинарника в `build/` кладётся `build/<name>.provenance.json`: `{spec, spec_version, commit_or_rev, go_version, built_at, checksum}`.
4. При рассинхроне (код/архитектура/бинарник не совпадают) — статус `stale`; работа приостанавливается до устранения.