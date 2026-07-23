# Changelog — dev-agent / LogAnalyzer (la)

Формат основан на [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/), версиирование [SemVer](https://semver.org/lang/ru/).
Каждое изменение функциональных и нефункциональных требований, архитектуры, кода и бинарного образа фиксируется здесь.

## [Unreleased]

### Added — спецификация US-0002
- Написана `architect/specs/ingestion.spec.md` (YAML + Mermaid) + диаграмма `architect/diagrams/ingestion.upload.mmd`.
- Таблицы миграции 0002: `t_files_upload` (вход, md5 UNIQUE — дедуп) 1→N `t_files_analyze` (распакованные, path_in_archive, format, + сводка постобработки: encoding/first_ts/last_ts/duration_sec/pp_status/pp_at/summary JSON/record_count/parsed_at) → `t_log_entries` (записи: ts/ts_raw/tz_offset/tz_inferred/level/component/message/raw_line TEXT/attrs JSON; индексы).
- Конфиг-поля: `MAX_FILE_SIZE=10GB`, `MAX_FILE_COUNT=10` (zip=1), `LA_DEFAULT_TZ=UTC`, `LA_PARSERS_DIR=./parsers`.
- Плагины парсеров (Go `.so` buildmode=plugin / Python `.py`) + общий интерфейс (`internal/parser`, `parsers/base.py`); detection по содержимому; built-in `text` fallback.
- Паттерны форматов выведены из `sample-logs/`: `oracle` (alert.log ISO8601+offset), `weblogic` (`####<...>`, UTC по epoch-millis), `wls_stdout` (`<ts> <LEVEL> <Comp> <Msg>`), `java` (log4j + stack trace), `access` (apache common, level из HTTP-статуса), `odl` (диагностический `[ISO8601] [comp] [level] ...`), `text` (fallback).
- REST API: `/api/uploads`, `/api/uploads/{id}`, `/api/files`, `/api/files/{id}/entries` (фильтры level/from/to/q), `DELETE` с каскадом, `/api/parsers`.
- Словарь нормализации критичности; `postgres`-парсер отложен (нет образца).

### Added — НФР (многоформатный/мультилокальный разбор дат)
- datetime определяется и разбирается в разных форматах и локалях: единый каталог layout-ов + локализованные имена месяцев (`Jan..Dec`, расширяемо). TZ-аббревиатуры (MSK/EST) неоднозначны → `LA_DEFAULT_TZ` (`tz_inferred=1`); явный offset → UTC (`tz_inferred=0`); ни один layout не подошёл → `ts=NULL`, `ts_raw` сохраняется. Общий модуль: Go `internal/parser/datetime.go`, Python `parsers/datetime.py`. Зафиксировано в `architect/specs/ingestion.spec.md` (`datetime`) и `user-stories/US-0002` (NFR).

### Added — НФР (плагинная архитектура парсеров)
- Приложение определяет тип лога и применяет разные парсеры; каждый парсер — отдельный подключаемый модуль: **Go — `.so`** (`go build -buildmode=plugin`), **Python — `.py`**, в подкаталоге `parsers/` (путь — env `LA_PARSERS_DIR`).
- Добавление/модификация парсера: собирается/кладётся только модуль парсера в `parsers/`, затем **рестарт процесса** (допустим) — парсер подхватывается при старте сканированием `parsers/`. При этом **не требуется пересборка основного бинарника** и **не требуется повторное развёртывание всего продукта** (новый ZIP/дистрибутив не нужен); модификация затрагивает только парсер.
- Общий стабильный интерфейс парсера — в отдельном пакете (Go: `internal/parser`; Python: `parsers/base.py`); изменение интерфейса — отдельная cross-cutting задача.
- Зафиксированы **ограничения Go-плагинов**: только Linux; плагин и хост — один тулчейн и одинаковые версии общих зависимостей (парсеры в том же модуле); выгрузка `.so` в работающем процессе невозможна — смена/добавление парсера через рестарт (допустим по NFR), без пересборки основного бинарника и повторного развёртывания продукта.
- Зафиксировано в `CLAUDE.md` (раздел «Требования и ограничения»), `user-stories/US-0002` (NFR) и `architect/manifest.md` (раздел «Парсеры»). Дизайн интерфейса парсера и менеджера загрузки — в `architect/specs/ingestion.spec.md`.

### Added — НФР (этап постобработки)
- После загрузки и парсинга каждый файл получает сводку (этап постобработки, параллельно парсерам). К каждому типу файла — свой постобработчик: **базовый** (built-in, всегда есть) — общие поля (число записей, дата загрузки, размер, кодировка, first/last ts, длительность, `level_counts`); **форматные наследуют от базового** и расширяют — минимум: число сессий старт-стоп, длительности интервалов старт-стоп, число сообщений в категориях (error/warning/critical/...).
- Постобработчики — подключаемые модули: **Go — `.so`** (`go build -buildmode=plugin`), **Python — `.py`**, в подкаталоге `postprocessors/` (путь — env `LA_POSTPROCESSORS_DIR`). Добавление/модификация: только модуль + **рестарт процесса** (допустим) — **без пересборки основного бинарника и без повторного развёртывания продукта**. Если плагина для формата нет — built-in базовый.
- Общий интерфейс: Go — `internal/postprocess`; Python — `postprocessors/base.py`. Сводка хранится в `t_files_analyze` (колонки `encoding`/`first_ts`/`last_ts`/`duration_sec` + `summary` JSON); кодировка определяется chardet по первым ~64KB. Доступ: `GET /api/files/{id}`, `POST /api/files/{id}/postprocess` (пере-запуск).
- Правила старт/стоп сессий per формат (oracle/weblogic/java/odl/access/text) — в `architect/specs/ingestion.spec.md` → `postprocessors.rules_by_format`.
- Зафиксировано в `CLAUDE.md`, `user-stories/US-0002` (NFR + AC-11..13), `architect/manifest.md` (раздел «Постобработчики») и `architect/specs/ingestion.spec.md` (`postprocessors`, `encoding_detection`, обновлённые `flow`/диаграмма, конфиг-поле `LA_POSTPROCESSORS_DIR`, эндпоинты API).

### Added — НФР (поставка и развёртывание)
- Зафиксированы нефункциональные требования к поставке: дистрибутив для ПК пользователя — **только ZIP-архив** (без инсталлятора).
- Node.js — **только build-time** (сборка Angular → `dist/`); в поставку входит собранный JavaScript, Node.js **не поставляется и не устанавливается** на ПК пользователя.
- Содержимое ZIP: бинарник backend (Go; Python — по запросу), `dist/` frontend, `la.conf.template`, `sh/*.sh`.
- Развёртывание: после распаковки — при необходимости создаётся `la.conf` из шаблона → `sh/start-la.sh` → первый запуск создаёт дефолтную SQLite `la.db` + миграции → стартует веб-сервер со смонтированным Angular-приложением. Внешних зависимостей на ПК пользователя нет (только ОС).
- Зафиксировано в `CLAUDE.md` (раздел «Требования и ограничения») и `architect/manifest.md` (раздел «Поставка и развёртывание (NFR)»). Шаг упаковки ZIP — отдельный релизный артефакт (по соответствующей ЮС).

### Added — требования
- Зафиксирована US-0002 «Загрузка и ингестия лог-файлов разнородных форматов» (status: ready) в двойном формате: `user-stories/US-0002-upload-and-ingest-log-files.{md,json}`. Покрывает: загрузка файлов/zip через веб-интерфейс, лимиты `MAX_FILE_SIZE=10GB`/`MAX_FILE_COUNT=10`/`LA_DEFAULT_TZ=UTC`, разнородные парсеры (Oracle alert.log, Postgres, Java multiline weblogic/tomcat/glassfish, Linux + fallback `text`), MD5-дедуп входящего файла с сообщением «Файл уже был загружен ранее», список/повторный анализ/удаление, преобразование в таблицы.

### Decided — схема/хранение US-0002
- Только текстовые логи; без BLOB и без отдельного ФС-хранилища — распарсенные записи (длинный TEXT) и есть хранилище.
- Модель записей: логическая запись = одна строка таблицы; multiline stack-trace блок склеивается в одну строку (`raw_line` TEXT с переносами); `ts`/`level` из головной строки.
- Datetime: `ts` (UTC ISO8601, сортируемый), `ts_raw` (оригинал), `tz_offset`, `tz_inferred` (0/1); не разобрана → `ts=NULL`, `ts_raw` сохранён. Для логов без смещения — `LA_DEFAULT_TZ` (UTC, изменяемый).
- Таблицы (миграция 0002): `t_files_upload` (вход, md5 UNIQUE — дедуп) 1→N `t_files_analyze` (распакованные файлы, path_in_archive, format) → `t_log_entries` (записи: ts/ts_raw/tz_offset/tz_inferred/level/component/message/raw_line TEXT/attrs JSON; индексы по file_analyze_id+seq, ts, level).
- zip: считается за 1 в `MAX_FILE_COUNT`; размер входящего ≤ `MAX_FILE_SIZE`; распаковка рекурсивна по подкаталогам (только лог-файлы); архив не хранится; каждому файлу новый ID.
- Формат: автоопределение по имени/содержимому; не определился → `text` (построчно + попытка даты/уровня); форматы `oracle`/`postgres`/`java`/`linux`/`text`; конфиг-файлы тоже просматриваемы.
- Открыты (уровень спецификации): паттерны разбора per формат, состав REST API, словарь нормализации критичности.

## [0.6.0] - 2026-07-24

Пресеты просмотра, аннотации/пин-точки, поиск по регулярке/attrs, стекированный по файлам график (US-0006) + хотфикс 500 на `GET /api/files` + переименование UI «Подкраска»→«Подсветить». Бинарь `release/backend/go/build/devagent` 0.6.0 (sha256 `9ba67b34…`, 23205624 байт). ZIP `release/dist/la-0.6.0.zip` (41246420 байт, sha256 `cb97ec8f…`; пред. 0.5.0 zip сохранён). Проверено: `go build/vet/gofmt/test -race` зелёные (9 новых/расширенных тестов: `TestGetFiles`, `TestSearchRegex`, `TestSearchAttrs`, `TestCorrelateRegex`, `TestHistogramByFile`, `TestPresetsCRUD`, `TestAnnotationsCRUD`, `TestREGEXPRegistrationIdempotent`, расширенный `TestDeleteUploadCascade`); `ng build`/`ng test` (40/40) зелёные; ZIP smoke (oracle alert.log → `GET /api/files` 200 непустой [регрессия 500] → regex `ORA-[0-9]+` 91 hit → `[bad(` 400 → correlate attrs COUNT-safe → histogram-by-file агрегация sum=record_count → time-pin/entry-pin 201 + mixed 400 → preset 201 → DELETE upload 204 + footer aggregates пересчитаны → CASCADE presets/annotations `[]` → SPA-fallback `<app-root>`) — все шаги зелёные.

### Added — US-0006 (пресеты, аннотации, regex/attrs, стекированный график)
- **Backend — пресеты:** `GET/POST /api/uploads/{id}/presets`, `DELETE …/{pid}`; пресет `{name, snapshot}` где `snapshot` — JSON-снимок состояния (`searchFilters`, `timeline`, `highlights`, `selectedFileIds`, `correlateMode`, `pageSize`). Таблица `t_view_presets` (миграция 0004, FK CASCADE по `upload_id`, индекс `idx_view_presets_upload`). Снимок на момент сохранения; правки не синхронизируются.
- **Backend — аннотации/пин-точки:** `GET/POST /api/uploads/{id}/annotations`, `DELETE …/{aid}`; entry-pin (`file_analyze_id`+`entry_id`, БЕЗ FK — dangling допускается, переживает re-ингест) или time-pin (`ts`); `note`+`color` обязательны; ровно один тип пина, смешанное/половинчатое → 400. Таблица `t_annotations` (миграция 0004, FK CASCADE по `upload_id`, индексы `idx_annotations_upload`/`idx_annotations_ts`); nullable-колонки → JSON `null`.
- **Backend — regex/attrs-поиск:** `search`/`correlate` расширены `mode=text|regex` и `attrs=k1:v1,k2:v2`. `mode=regex` — серверная скалярная функция `REGEXP` (`modernc.org/sqlite RegisterScalarFunction`, package-level `sync.Once` + `sync.Map`-кеш `*regexp.Regexp`; без `sync.Once` повторная регистрация на 2+ соединениях падает «already registered»); OR по полям (all: ts_raw/level/component/message/raw_line; raw: raw_line); невалидный паттерн → предкомпиляция `regexp.Compile` в Go → 400. `attrs` → `AND json_extract(attrs,'$.k') LIKE '%v%'` (JSON1 встроен в modernc); отсутствующий ключ → NULL → исключение; пустое значение `k:` → «ключ существует». В `correlate` предикаты regex/attrs в общем `where` (COUNT-safe: `total`==`items.length`). Хелпер `attrsPredicates(attrs, alias)`; `bucketFmt` вынесен из `getHistogram`.
- **Backend — стекированный график:** `GET /api/uploads/{id}/histogram-by-file?bucket=&from=&to=&files=` — `GROUP BY bucket, file_analyze_id` → `[{bucket, file_analyze_id, count}]`; клиент агрегирует в стекированные сегменты.
- **Frontend:** режим поиска text/regex + поле attrs (чипы с бейджами `[regex]`/`[attrs:…]`); бар пресетов (`PresetBarComponent` — выбор/загрузить/удалить/сохранить); панель аннотаций (`AnnotationPanelComponent` — список + форма time-pin + кнопка 📌 entry-pin в строках таблицы); стекированный график (`StackedChartComponent` — сегменты по `FILE_PALETTE` по индексу, цикл `% len` при >10 файлов, time-pin маркеры через `bucketOf(ts,bucket)`); `FILE_PALETTE` извлечена в `components/palette.ts` (без дублирования, `correlate-table` импортирует). Viewer wiring: сигналы `presets`/`annotations`/`histByFile`/`stackedBucket`, `effect` для перегрузки графика, `savePreset`/`loadPreset`/`deletePreset`, `addAnnotation`/`removeAnnotation`/`onPinEntry`, `activeSearchMode`/`activeSearchAttrs` → `correlate-table` (`searchMode`/`searchAttrs` inputs). `ApiService`: `mode`/`attrs` в `search`/`correlate` + `histogramByFile` + presets/annotations CRUD. Модели: `PresetSnapshot`/`Preset`/`Annotation`/`HistogramByFilePoint`, `SearchRule.mode/attrs`.
- **Спека** `architect/specs/viewer.spec.md` — `spec_version 0.2.0 → 0.3.0`, `us_ref +US-0006`, секции `presets`/`annotations` в `frontend.screens.viewer`, расширение `search_filters` (mode/attrs) и `correlation` (стекированный график), эндпоинты `histogram-by-file` + presets/annotations CRUD + mode/attrs в `backend.new_endpoints`, миграция 0004 + таблицы `t_view_presets`/`t_annotations` в `backend.persistence`, notes про REGEXP `sync.Once`/кеш, `json_extract` NULL→исключение, COUNT-safe, dangling entry-pin; узлы `PR/AN/SC` в Mermaid.
- **US-0006** зафиксирована в двойном формате `user-stories/US-0006-viewer-presets-annotations-regex-stacked.{md,json}` (status: ready; deps US-0002/0003/0004/0005; AC-1..AC-10; trace → миграция 0004, `db.go`, `handlers_viewer.go`, `helpers.go`, 3 новых компонента + `palette.ts`, `viewer.component.*`, `api.service.ts`, 0.6.0).

### Fixed — 500 на `GET /api/files?upload_id=…` (хотфикс, пользовательский баг)
- `scanFileRows` (`internal/server/helpers.go`) сканировал **13 приёмников** против **16 колонок** `getFiles`-SELECT (`id, upload_id, filename, path_in_archive, md5, format, status, record_count, parsed_at, encoding, first_ts, last_ts, duration_sec, pp_status, summary, error`) → `rows.Scan` падал на несоответствии → 500 на **каждом** `GET /api/files` → пустой селектор файлов viewer'а (второй симптом пользователя «список файлов показывается пустым» — тот же корень). Выровнен под 16 колонок (`parsed_at`/`pp_status`/`error` как `sql.NullString`, собраны в выходную map). Регрессионный тест `TestGetFiles` (upload → `GET /api/files?upload_id=…` → 200, файл с `format`/`record_count`/`summary`). Симптом воспроизвёлся на `alert_orclcdb.log` пользователя — smoke 0.6.0 подтверждает 200 + непустой список.

### Fixed — footer aggregates не пересчитывались после удаления файла/upload
- `PRAGMA foreign_keys=ON` выставлялся через `db.Exec` на одном соединении пула (`database/sql` ленильно создаёт соединения) → CASCADE не срабатывал на остальных → `getStats` COUNT не менялся после delete. Перенесено в DSN: `?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)` применяется к **каждому** новому соединению пула. Smoke 0.6.0: `stats.file_count` уменьшается после `DELETE upload`.

### Changed — UI «Подкраска» → «Подсветить»
- 5 вхождений во фронте (`viewer.component.html` ×3, `viewer.component.ts` confirm, `file-window.component.ts` комментарий) + `docs/USAGE.md`. Исторические артефакты (прошлые релизы в changelog/US/manifest, спека-термин «подкраска» как концепция) НЕ переписывались.

### Verification — 0.6.0
- `go build ./...` ok; `go vet ./...` ok; `gofmt -l .` clean; `go test -race ./...` ok (9 новых/расширенных серверных тестов зелёные).
- `ng build` ok (~80 kB transfer initial; lazy chunk viewer-component с 3 новыми компонентами); `ng test --watch=false --browsers=ChromeHeadless` **40/40 success** (новые: `stacked-chart` 5, `annotation-panel` 4, `preset-bar` 5, расширены `api.service.spec` +8; `correlate-table` palette-извлечение поведение сохранило).
- ZIP smoke e2e (oracle `alert_orclcdb.log`, 6217 записей): upload → uuid; `GET /api/files` 200 непустой (регрессия 500); `search?mode=regex&q=ORA-[0-9]+` → 91 hit, `q=[bad(` → 400; `search?mode=text&q=ORA` → 100; `correlate?attrs=level:ERROR` → 200 `{items,total}` COUNT-safe; `histogram-by-file?bucket=hour` → 826 точек, sum_counts=6217 (=record_count, агрегация корректна); POST time-pin/entry-pin annotation → 201, mixed → 400, GET list → 2; POST preset → 201, GET list → 1; DELETE upload → 204, `stats.file_count` уменьшился, GET presets/annotations → `[]` (CASCADE); `/viewer/:id` → 200 `<app-root>` (SPA-fallback).

### Notes — 0.6.0
- REGEXP — серверная скалярная функция; `sync.Once` критичен (без него 2-й `db.Open` в тестах/процессе падает «already registered»); кеш `*regexp.Regexp` процесс-глобальный (`sync.Map`).
- `json_extract` (JSON1) встроен в `modernc.org/sqlite` — без внешних расширений; отсутствующий ключ → NULL → `NULL LIKE '%x%'` = NULL (falsy) → строка исключена (требуемое поведение «ключ существует со значением»).
- Аннотации entry-pin БЕЗ FK → dangling допускается (переживает re-ингест файла; фронт рендерит бейдж «вне страницы/удалена»); CASCADE только по `upload_id`.
- Пресет — снимок на момент сохранения; загрузка заменяет состояние (confirm если highlights/фильтры непусты); правки после сохранения не синхронизируются.
- Стекированный график — клиент-сайд агрегация плоских `{bucket,file_analyze_id,count}`; цвет стабилен per file-id по индексу в `fileIds` (цикл `% FILE_PALETTE.length` при >10 файлов).
- `deleteViewState` НЕ затрагивает пресеты и аннотации (они персистентные заметки/явно сохранённые).
- env-override `LISTEN_PORT` (как и #4) — по-прежнему отложено пользователем; smoke использует дефолт 8888.

## [0.5.0] - 2026-07-24

Корреляция событий по времени между файлами (US-0005) — объединённый по `ts` кросс-файл поток событий на общем таймлайне. Бинарь `release/backend/go/build/devagent` 0.5.0 (sha256 `6f77ec9b…`, 23141624 байт). ZIP `release/dist/la-0.5.0.zip` (41214557 байт, sha256 `8e98ebb9…`(пред. 0.4.0 zip сохранён). Проверено: `go build/vet/gofmt/test -race` зелёные; `ng build`/`ng test` (17/17) зелёные; ZIP smoke 6/6 (2-файловый zip → `/correlate` порядок `a.log,b.log,a.log` + `filename`; `from/to`-окно → 1 запись `b.log`; `q=mid` → 1; SPA-fallback `/viewer/:id` → `<app-root>`).

### Added — US-0005 (корреляция по времени)
- **Backend:** `GET /api/uploads/{id}/correlate?files=&from=&to=&q=&level=&limit=&offset=` — объединённый поток записей из выбранных файлов (по умолчанию все файлы загрузки), `JOIN t_log_entries × t_files_analyze` за `filename`, `ORDER BY e.ts IS NULL, e.ts, e.seq` кросс-файл (записи без `ts` — в конце); фильтры `files` (subset), `from`/`to` (ts-окно), `q` (LIKE по полям, как в `search`: `fields=all|raw`), `level`; пагинация `{items, total, limit, offset}` (limit ≤ 1000). Ограничен рамками одной загрузки (`f.upload_id = ?`); пустая выборка → `{items:[], total:0, …}`. Маршрут в `server.go` (`internal/server/handlers_viewer.go:getCorrelate`). Тест `TestCorrelate` (zip с weblogic+java, чередование по ts, `filename`, `from/to`, subset, пустая выборка) — `t.TempDir()`.
- **Frontend:** режим «Корреляция (общий таймлайн)» в просмотрщике — переключатель заменяет пофайловые таблицы единой объединённой таблицей (`ts | файл (цвет) | level | component | message | raw`); записи из `/correlate` по выбранным в селекторе файлам + таймлайн-окно + активный поиск + подкраска; над таблицей — общий график `/histogram?files=…`; пагинация. Новый компонент `components/correlate-table/` (`CorrelateTableComponent`, spec 3 теста). Цвет файла — стабильная палитра по индексу в `selectedFileIds`. `ApiService.correlate` + модели `CorrelatedEntry`/`CorrelatedPage`.
- Переиспользованы: `timeline` (границы), `histogram` (мульти-файловый `files[]`), `highlights` (подкраска), селектор файлов (US-0003).
- **Спека** `architect/specs/viewer.spec.md` — `spec_version 0.1.0 → 0.2.0`, `us_ref +US-0005`, `summary` дополнен, новая секция `frontend.screens.viewer.correlation`, эндпоинт `correlate` в `backend.new_endpoints`, note про `JOIN`/`ORDER BY ts IS NULL`, узел `X[Режим Корреляция]` в Mermaid.
- **US-0005** зафиксирована в двойном формате `user-stories/US-0005-cross-file-time-correlation.{md,json}` (status: ready; deps US-0002/0003/0004; AC-1..AC-7; trace → `handlers_viewer.go`, `viewer.component.*`, `correlate-table.component.*`, 0.5.0).

### Notes — ограничения/открытые вопросы (US-0005)
- Записи без `ts` не теряются (в конце потока), но на общий график не попадают (`histogram` фильтрует `ts IS NOT NULL`).
- `q` — LIKE (FTS5/регулярки — отдельная ЮС); стекированный по файлам график — на потом (MVP: суммарный `histogram?files=…`).
- `la.conf` — без новых обязательных полей; лимиты — дефолты в коде. Go-плагины (парсеры/постобработчики) не затронуты; Python-релиз — тот же эндпоинт/контракт по запросу.

## [0.4.0] - 2026-07-24

Завершение UX-заглушек viewer (US-0003 ФР-5/ФР-9) — review-сценарий #1 («получить MVP и корректировать дальше»; #2 frontend multi-file UI и #4 env-override отложены пользователем). Бинарь `release/backend/go/build/devagent` 0.4.0 (sha256 `5da2727f…`, 23126712 байт). ZIP `release/dist/la-0.4.0.zip` (41 206 151 байт, sha256 `4e4d7643…`; устаревший 0.3.0 zip удалён). Проверено: `ng build`/`ng test` (14/14) зелёные; ZIP smoke 8/8.

### Fixed — FileTable не реагировал на фильтры (MVP-баг)
- `FileTableComponent` грузил записи только в `ngOnInit` и не реагировал на изменение `@Input from/to/searchQ` → таймлайн и поиск фактически не фильтровали пофайловые таблицы. Добавлен `ngOnChanges`: при смене `file`/`from`/`to`/`searchQ` — сброс `offset=0` и перезагрузка entries+histogram. Начальная загрузка тоже идёт через `ngOnChanges` (вызывается для начальных @Input до `ngOnInit`), отдельный `ngOnInit` убран. `highlights` намеренно исключён (подкраска не требует перезагрузки записей).

### Added — timeline drag-слайдер (ФР-5)
- `ViewerComponent`: dual-thumb `<input type="range">` поверх `[min_ts…max_ts]` (epoch-ms): вычисляемые `minMs/maxMs/fromMs/toMs/rangeSpan/fillLeft/fillWidth`; live-подпись диапазона во время перетаскивания (`dragFromMs/dragToMs`), коммит `from/to` ISO на `change` (persist + refresh через `ngOnChanges` FileTable). Кнопка «сброс». Прежние `datetime-local` убраны под `<details>` как точный ввод. Стили dual-thumb в `viewer.component.scss`.

### Added — «новое окно» реальный рендер (ФР-9)
- Новый роут `window/:fileId` → `FileWindowComponent` (`release/frontend/src/app/pages/file-window/`): грузит `GET /api/files/{id}` (→ upload_id + filename) и `GET /api/uploads/{id}/highlights`, рендерит `<app-file-table>` (полноценный Angular-рендер вместо статического плейсхолдера). Закрытие окна → `window.close()`.
- `ViewerComponent.openInWindow`: `window.open('/window/'+fileId)` (SPA-роут, backend SPA-fallback отдаёт index.html); хэндлы окон в `Map` («управление от основной страницы»), `beforeunload`→вернуть на основную; в селекторе бейдж «окно ×» — кнопка возврата. Файл в `detached` не показывается на основной (через `selectedFiles` computed), при закрытии окна возвращается.
- Спецификация `architect/specs/viewer.spec.md` (ФР: «границы двигаются; фильтрует по ts», «новое окно с управлением») уже соответствовала — код догнал спеку; `spec_version` viewer без изменений (0.1.0).

### Changed
- `cmd/devagent/main.go`: версия `0.3.0` → `0.4.0`.
- `devagent.provenance.json`: 0.3.0 → 0.4.0 (новый sha256/zip, `zip_smoke` 8/8, фичи timeline-slider/file-window/table-reacts-to-filters).
- `architect/manifest.md`: ingestion/viewer → 0.4.0 active + note.

### Verification
- `ng build` — зелёный (lazy-чанк `file-window-component`; ~80 kB transfer initial). `ng test --watch=false --browsers=ChromeHeadless` — **14/14 SUCCESS** (добавлен `file-window.component.spec.ts`: 3 теста — загрузка файла+highlights+рендер app-file-table, ошибка без fileId, closeWindow не падает).
- ZIP smoke 0.4.0 (распаковка в чистый /tmp): `./devagent` 0.4.0 → 10 `.so` `plugin.Open` без ошибок → upload weblogic (92 записи) → `/timeline` (min/max) → `/entries?from=&to=` (1-сек окно: **92→5**, фильтр работает) → `/entries?q=Server` (88) → `GET /window/:fileId` → 200 + `<app-root>` (SPA-fallback) → `GET /viewer/:id` → 200 + `<app-root>` → delete. **8/8 passed.**

### Notes
- Слайдер коммитит на `change` (отпускание), не на каждый `input` → нет шторма запросов/персиста; live-подпись обновляется на `input`.
- `persistTimeline` по-прежнему создаёт новый timeline-фильтр на каждый коммит (дубли могут накапливаться до «Очистить»); мелкая доработка — на потом.
- #2 (frontend multi-file UI) и #4 (env-override конфига) — отложены пользователем явно.

## [0.3.0] - 2026-07-24

Релиз дистрибутива и плагинной поставки. Реализован review-пункт #7 пользователя: собраны `.so`-плагины парсеров/постобработчиков и **ZIP-дистрибутив** с MD-документацией; review-пункт #2 (многопоточная загрузка) реализован на backend. Бинарь `release/backend/go/build/devagent` 0.3.0 (sha256 `5a14a2a2…`, 23126712 байт). ZIP `release/dist/la-0.3.0.zip` (41 200 062 байт, sha256 `fc7fc4c2…`). Проверено: `go build/vet/gofmt/test -race` зелёные; end-to-end smoke распакованного ZIP — 15/15.

### Added — плагинная поставка (.so)
- Исходники плагинов: `release/backend/go/parsers/<fmt>/main.go` (oracle/weblogic/wls_stdout/java/access/odl) и `release/backend/go/postprocessors/<fmt>/main.go` (oracle/weblogic/wls_stdout/odl) — `package main` с `//go:build plugin`, `func New() parser.Parser` / `func New() postprocess.Postprocessor` (делегируют в экспортированные конструкторы `internal/parser`/`internal/postprocess`). `text` (парсер) и `base` (постобработчик) остаются built-in в хосте (fallback).
- Сборка: `make plugins` → `go build -buildmode=plugin -tags plugin -o parsers/<fmt>.so ./parsers/<fmt>` (аналогично postprocessors). Go plugin: Linux-only, один тулчейн `/usr/local/go/bin/go`, общие зависимости из того же модуля → `plugin.Open` без ошибок. Makefile: цели `parsers`/`postprocessors`/`plugins`/`dist`.
- Runtime: `plugin.Open` → `Lookup("New")`; плагин с тем же `Name()` заменяет built-in (override). Добавление/модификация парсера/постобработчика — собрать/положить только `.so` + рестарт процесса; **без пересборки основного бинарника и без повторного развёртывания продукта** (NFR соблюдён).

### Added — ZIP-дистрибутив и документация
- Скрипт `sh/make-dist.sh` (POSIX sh): сборка бинарника + плагинов, стейджинг `build/dist/la-<version>/`, упаковка `release/dist/la-<version>.zip`. Версия читается из `const version` в `main.go`.
- Состав ZIP `la-0.3.0/`: `devagent` (бинарник), `la.conf.template`, `frontend/browser/` (собранный Angular), `parsers/*.so` (6), `postprocessors/*.so` (4), `sh/{start,stop,status}-la.sh` (dist-адаптированные: env задаёт dist-относительные пути — `LA_FRONTEND_DIST`/`LA_PARSERS_DIR`/`LA_POSTPROCESSORS_DIR`/`SOURCE_DB_URL`), `docs/{README,INSTALL,USAGE}.md`.
- Документация `release/docs/`: `README.md` (продукт/состав), `INSTALL.md` (распаковка → `la.conf` → `sh/start-la.sh` → авто-создание SQLite → веб), `USAGE.md` (загрузка/просмотр/плагины). Копируется в ZIP.
- NFR поставки: только ZIP, без инсталлятора; Node.js только build-time (Angular `dist/`), в runtime не поставляется и не устанавливается; один процесс backend (API + статика); внешних зависимостей на ПК пользователя нет (только ОС).

### Changed
- `cmd/devagent/main.go`: версия `0.2.0` → `0.3.0`.
- `internal/server/handlers_ingest.go`: `POST /api/uploads` принимает **несколько файлов одновременно** (multipart с несколькими file-полями; каждый файл обрабатывается независимо, дедуп/ошибка одного не прерывает остальные). Ответ 201 `{results:[{upload_id, files:[{file_analyze_id, format, summary, …}], status, duplicate?, existing_upload_id?, …}]}`; дедуп — `status:"duplicate"` + `duplicate:true` + `existing_upload_id` (не 409). (review-пункт #2)
- `devagent.provenance.json`: 0.2.0 → 0.3.0 (новый sha256/размер, блоки `plugins_built`, `distribution`, `zip_smoke_e2e`).

### Verification
- `go build`/`go vet`/`gofmt -l .` — чисто; `go test -race ./...` — зелёные (config/db/ingest/parser/postprocess/server); покрытие без изменений.
- ZIP smoke (распаковка в чистый /tmp): `./devagent` 0.3.0 → авто-создание `la.db`+`la.conf` → 6 parser `.so` + 4 postprocess `.so` `plugin.Open` без ошибок (`parser/postprocess plugin loaded` в логе) → `POST /api/uploads` weblogic AdminServer.log → 201, `format=weblogic`, `record_count=92`, `summary` (encoding UTF-8, level_counts, 1 session — форматный постобработчик-плагин) → `/entries` (items/total) → повторная загрузка → `status=duplicate`+`existing_upload_id` → мульти-загрузка (2 файла одним запросом, 2 results) → `GET /` (index.html) + SPA-fallback `/viewer/:id` → `DELETE /api/uploads/{id}` → 204. **15/15 passed.**

### Notes
- Frontend пока отправляет по одному файлу за запрос; backend уже поддерживает несколько (UI-часть review-пункта #2 — отдельная правка фронта).
- env-override полей конфига (LISTEN_PORT/SOURCE_DB_URL и др.) — TODO (review-пункт #4, «не критично»); dist-скрипты используют env для путей, но `la.conf`-поля port/db-url env пока не перекрывают.

## [0.2.0] - 2026-07-23

Реализованы backend (Go) US-0002 (ингестия) + US-0004 (viewer-поддержка) и frontend (Angular) US-0003. Бинарь `release/backend/go/build/devagent` 0.2.0; статика `release/frontend/dist/la-frontend/browser/`. Проверено: `go build/vet/gofmt/test -race/cover` зелёные, `ng build`/`ng test` (10/10) зелёные, end-to-end smoke зелёный.

### Added — домен viewer (US-0003 frontend + US-0004 backend)
- Зафиксированы ФР фронтенда (US-0003, двойной формат `user-stories/US-0003-log-viewer-frontend.{md,json}`, status: ready, target: angular) и бэкенд-расширение (US-0004, `user-stories/US-0004-viewer-backend-support.{md,json}`, status: ready, target: go).
- Спецификация `architect/specs/viewer.spec.md` (YAML + Mermaid) — домен `viewer`, проект frontend+backend.
- ФР фронтенда: таблица загрузок (сортируемая/фильтруемая, удаление с каскадом, агрегаты под таблицей); drill-in в просмотр (шапка деталей архива/файла); селектор файлов с чекбоксами (все выбраны по умолчанию); мульти-фильтр поиска по всем полям (+опц. raw) по всем файлам селектора, независимое удаление фильтров; таймлайн-селектор min/max даты; подкраска строк (текст + опц. кластерные лексемы + виджет цвета); персистентность фильтров/подкраски между рестартами (в таблицах) + кнопка очистки; пофайловые таблицы (по 10 строк / динамически, имя+краткая сводка мелким шрифтом, закрытие снимает чекбокс); опц. открытие файла в новом окне с управлением от основной страницы; график событий с группировкой месяц/день/час/минута (всплески).
- Бэкенд-расширение (US-0004): `GET /api/stats`, расширение `GET /api/uploads`/`GET /api/uploads/{id}` (агрегаты, summary, first_ts/last_ts); `GET /api/uploads/{id}/search` (мульти-файловый поиск по полям all|raw), `/timeline` (границы ts), `/lexemes` (топ-N токенов, MVP частотность), `/histogram?bucket=...` (GROUP BY bucket(ts)); миграция 0003 — `t_view_filters`/`t_view_highlights` (per upload, ON DELETE CASCADE, survives restarts); `GET/POST/DELETE /api/uploads/{id}/filters[/{fid}]`, `…/highlights[/{hid}]`, `DELETE /api/uploads/{id}/view-state` (кнопка очистки); `DELETE /api/uploads/{id}` каскадно чистит `t_view_*`.
- Состояние просмотра — на сервере (не localStorage как единственный источник); пофайловые таблицы — пагинация (limit/offset); поиск — `LIKE` (FTS5 — отдельная ЮС).

### Added — реализация backend US-0002 (ингестия, Go)
- `internal/config`: поля `MAX_FILE_SIZE` (парсинг 10GB/512MB → байты, humanize), `MAX_FILE_COUNT=10`, `LA_DEFAULT_TZ=UTC`, `LA_PARSERS_DIR=./parsers`, `LA_POSTPROCESSORS_DIR=./postprocessors`; `la.conf.template` обновлён.
- `internal/db`: `PRAGMA foreign_keys=ON` (CASCADE); миграции 0002 (`t_files_upload` md5 UNIQUE, `t_files_analyze` со сводкой постобработки, `t_log_entries` + индексы) и 0003 (`t_view_filters`, `t_view_highlights`, FK CASCADE); идемпотентный раннер.
- `internal/parser`: интерфейс `Parser`/`Record`/`Manager`; 7 built-in парсеров (oracle/weblogic/wls_stdout/java/access/odl/text) по реальным образцам `sample-logs/`; multiline-блок = одна запись (`raw_line` TEXT); detection по sample первых ~20 строк; загрузка `.so`-плагинов через `plugin.Open`→`Lookup("New")` (опциональна, recover, не фатальна). `datetime.go` — каталог layout-ов + локализованные месяцы, TZ inferred→`LA_DEFAULT_TZ`. `encoding.go` + `decoders.go` — chardet + декодеры Windows-1251/1252/ISO-8859.
- `internal/postprocess`: `Session`/`Summary`/`Postprocessor`/`Base`/`Manager`; base (общая сводка) + форматные наследники (oracle/weblogic/wls_stdout/odl) с правилами start/stop; fallback base; загрузка `.so`-плагинов.
- `internal/ingest`: multipart приём, потоковый MD5, лимиты, дедуп 409 «Файл уже был загружен ранее», рекурсивная распаковка zip (`archive/zip`), detect→parse (потоково)→batch INSERT транзакциями→UPDATE record_count/status→postprocess→UPDATE encoding/first_ts/last_ts/duration_sec/summary/status=pp.
- `internal/server`: роуты ingestion (`/api/uploads`, `/api/files`, `/api/files/{id}/entries`, `/api/parsers`, DELETE с каскадом) + viewer; статика SPA + fallback (`release/frontend/dist/la-frontend/browser/`, env `LA_FRONTEND_DIST`).
- `cmd/devagent/main.go`: версия `0.2.0`; config→db→migrations→parser mgr→postprocess mgr→server→graceful shutdown.

### Added — реализация frontend US-0003 (Angular)
- Angular 20.3 (standalone, routing, SCSS, без SSR) в `release/frontend/`; `ng build` → `dist/la-frontend/browser/` (~78 kB transfer).
- Экраны: таблица загрузок (сортировка/фильтр/delete/агрегаты из `/api/stats`), drill-in `/viewer/:id` (шапка деталей), селектор файлов с чекбоксами, мульти-фильтр поиска (`/search`, fields=all|raw), подкраска (`/lexemes` + color, `/highlights`), персистентность (`/filters`, `/highlights`, кнопка очистки `/view-state`), пофайловые пагинируемые таблицы (`/entries` limit/offset, имя+сводка мелким шрифтом, закрытие↔чекбокс), SVG-график событий с группировкой M/D/H/min (`/histogram`).
- `ApiService` + `models.ts` (контракт); 8 unit-тестов + 2 app-теста (`ng test --browsers=ChromeHeadless` — 10/10).
- MVP-заглушки: таймлайн — datetime-инпуты (drag-слайдер — TODO); «новое окно» — плейсхолдер с управлением (полный рендер в окне — TODO).

### Changed
- Бинарник `release/backend/go/build/devagent` 0.1.0 → 0.2.0; `devagent.provenance.json` обновлён (sha256 `b59b3ae2…`, 23120280 байт, фичи, покрытие).
- `go.mod`: добавлены `github.com/saintfish/chardet`, `golang.org/x/text` (через `go get`).
- `architect/manifest.md`: `ingestion` и `viewer` → `active` (0.2.0) с кодом/бинарником.

### Verification
- `go build`/`go vet`/`gofmt -l .` — чисто; `go test -race ./...` — зелёные; покрытие: config 87.5%, db 72.7%, ingest 74.8%, parser 25.1%, postprocess 53.3%, server 59.6%.
- `ng build` — зелёный; `ng test --watch=false --browsers=ChromeHeadless` — 10/10 SUCCESS.
- Smoke e2e (один процесс backend раздаёт API + статику): `POST /api/uploads` (weblogic AdminServer.log) → 201, format=weblogic, record_count=92, summary (encoding UTF-8, first/last ts, level_counts, sessions); `/api/stats`, `/timeline`, `/histogram?bucket=hour`, `/lexemes`, `/api/files/{id}/entries` (items/total/limit/offset, `raw_line`), `/search?q=Server`, filters/highlights CRUD, дедуп → 409 «Файл уже был загружен ранее»; статика `/` + SPA-fallback `/viewer/:id` → index.html.

### Notes
- MVP: входящий файл читается в `limitedBuffer` (для zip, нужен `io.ReaderAt`+size); true-streaming без буфера в RAM — TODO (NFR соблюдён частично, лимит `MAX_FILE_SIZE` защищает).
- `POST /api/uploads` обрабатывает один файл за запрос (фронт шлёт по одному); множественные отдельные файлы за запрос — TODO/уточнение (конфликт с md5-дедупом per-file).
- `postgres`-парсер отложен (out_of_scope: образца нет); плагины `.so` не собирались (built-in + загрузчик работают; add `.so` + restart по NFR).
- env-override отдельных полей конфига (LISTEN_PORT/SOURCE_DB_URL) не wired — конфиг берёт дефолты из `la.conf` (путь — env `LA_CONF`); при необходимости — отдельная правка.

## [0.1.0] - 2026-07-23

Первый функциональный релиз: определён продукт LogAnalyzer (la), реализован первый поток backend — конфигурация и bootstrap БД (US-0001).

### Added — продукт и процесс
- Зафиксирован продукт **LogAnalyzer (la)** — анализ и визуализация лог-файлов заказчиков для инженеров тех.поддержки и аналитиков.
- Состав продукта: **два независимо подключаемых и изменяемых проекта** — `backend` (`release/backend/`, Go первично / Python-FastAPI вторично) и `frontend` (`release/frontend/`, Angular).
- Пользовательские истории — **двойной формат**: текстовый `.md` (канонический) + служебный `.json` (машинный, трассировка). Процесс обработки внешних правок ЮС: дифф → проверка → синхронизация `.json` → распространение в архитектуру/схемы, код, тесты, документацию, changelog, релиз. Описано в `CLAUDE.md` и `user-stories/README.md`.
- Управление процессом продукта: скрипты `sh/start-la.sh`, `sh/stop-la.sh`, `sh/status-la.sh` (POSIX sh, однопроцессная модель backend, PID `sh/.run/la-backend.pid`, `LA_BACKEND=go|python`).
- Требование: по запросу доступен релиз на Python **в полном объёме функционала** (наряду с первичным Go).

### Added — US-0001 (конфигурация и bootstrap БД)
- Пользовательская история US-0001 в двойном формате: `user-stories/US-0001-config-and-db-bootstrap.{md,json}`.
- Спецификация `architect/specs/config.spec.md` (YAML + Mermaid) и диаграмма `architect/diagrams/config.startup.mmd`.
- Пакет `internal/config`: загрузка `la.conf` (формат KEY=VALUE), дефолты (`SOURCE_DB_URL=sqlite:la.db`, `LISTEN_ADDRESS=localhost`, `LISTEN_PORT=8888`), создание из встроимого шаблона `la.conf.template` (`//go:embed`), переопределение пути env `LA_CONF`.
- Пакет `internal/db`: открытие SQLite по `SOURCE_DB_URL` (драйвер `modernc.org/sqlite`, чистый Go, без CGo), авто-создание файла, прагмы WAL/busy_timeout; разбор scheme `sqlite:` (и зарезервировано `postgres://` — пока ошибка).
- Пакет `internal/db` миграции: таблица `schema_migrations`, идемпотентный раннер; миграция `0001` создаёт baseline `la_meta(key, value)`.
- Пакет `internal/server`: HTTP на `LISTEN_ADDRESS:LISTEN_PORT`, эндпоинт `GET /healthz` → `{"status":"ok"}` (ping БД), graceful shutdown.
- `cmd/devagent/main.go` (версия `0.1.0`): связка config → db.Open → migrations → HTTP → shutdown по `SIGINT/SIGTERM`.
- Тесты: `config_test.go`, `migrations_test.go`, `server_test.go` (изоляция через `t.TempDir()`, HTTP-проверка через `net/http` клиент — без curl/wget).
- Собран бинарник `release/backend/go/build/devagent` `0.1.0` + `devagent.provenance.json` (spec, sha256, размер, go1.26.2).
- `.gitignore` (корень): `la.conf`, `la.db`, `*.db*`, `sh/.run/`, сборки Go/Python/Angular.

### Changed
- Источник данных: для MVP — **SQLite по умолчанию** (`SOURCE_DB_URL=sqlite:la.db`, авто-создание); **Postgres — позже** отдельной ЮС. Зафиксировано в `CLAUDE.md`, `architect/specs/config.spec.md`.
- Runtime-модель: **Node не используется в runtime** — frontend (Angular) компилируется build-time в `dist/`, раздаётся backend (Go: `embed.FS`/`http.FileServer`; Python: `StaticFiles`). В runtime один процесс backend.
- Структура `release/` приведена к `release/backend/{go,python}` + `release/frontend/` (вместо `release/go`/`release/python`); обновлены `CLAUDE.md`, `architect/manifest.md`, нейминг.
- `architect/manifest.md`: запись для `config` (0.1.0 active) вместо плейсхолдера; таблица с колонками Go/Python/Frontend и `project`; раздел «Управление процессом».
- `settings.local.json`: frontend build/test (node, npm run/test, npx, ng build/test/lint) — allow; `ng serve`/`npm install`/`ng add`/`ng new`/`pip`/`nuitka`/`docker`/`psql` — ask; `rm -rf`/`git push`/`curl`/`wget` — deny.

### Verification
- `go build`, `go vet`, `gofmt`, `go test -race`, `go test -cover` — зелёные (покрытие: config 84%, db 73%, server 85%).
- Smoke: `sh/start-la.sh` → создаёт `la.conf` из шаблона и `la.db`, `GET /healthz` → `200 {"status":"ok"}`, таблицы `la_meta`+`schema_migrations`, миграция `0001` записана → `sh/stop-la.sh` (graceful). Осиротевших процессов нет.

### Notes
- Git будет подключён позже; пока работа идёт в каталоге.
- Module path Go: `github.com/irav/dev-agent` (меняемый; уточнить при подключении git/публикации).
- За рамками: Postgres-драйвер, таблица `logs` и приём/парсинг логов, полный REST API, Angular-frontend.

## [0.0.1] - 2026-07-21

### Added
- Проектный скаффолд `dev-agent`: каталоги `user-stories/`, `architect/` (`specs/`, `diagrams/`), `changelog/`, `release/{go,python}` (`cmd/`, `internal/`, `build/`, `src/`, `tests/`).
- `CLAUDE.md`: роль агента, workflow (user-stories → architect → code → build), инструменты, требования/ограничения, правила безопасности, нейминг, структура, сохранение контекста.
- `settings.local.json` — базовая структура разрешений.
- `architect/manifest.md` — реестр трассировки спецификация → код → бинарник.
- Тулчейн: `/usr/local/go/bin/go` (go1.26.2 linux/amd64); спецификация — `.md` с YAML-блоком + Mermaid; мультиязычный таргет.
- Минимальная точка входа `cmd/devagent/main.go` (плейсхолдер `0.0.0-placeholder`), `Makefile`, скелетный бинарник + provenance. Проверено зелёным: `go build/vet/test`, `gofmt`.