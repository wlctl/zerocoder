---
id: US-0006
title: Пресеты просмотра, аннотации/пин-точки, поиск по регулярке/attrs, стекированный по файлам график
status: ready
priority: should
domain: viewer
project: [backend, frontend]
target: [go, angular]
spec_ref: architect/specs/viewer.spec.md
created: 2026-07-24
updated: 2026-07-24
---

# US-0006 — Пресеты, аннотации, regex/attrs-поиск, стекированный график

## Как аналитик тех.поддержки
## я хочу сохранять удачные комбинации фильтров/подкраски в пресеты, оставлять аннотации-пин-точки на записях и моментах времени, искать по регулярным выражениям и атрибутам записей, и видеть стекированный по файлам график событий
## чтобы быстро воспроизводить типовые разборы логов, помечать важные места для коллег/будущих заходов и гибко находить события по структуре записей.

## Контекст

Расширение домена `viewer` поверх US-0003/US-0004/US-0005. Текущий просмотрщик имеет мульти-фильтр поиска (LIKE), таймлайн, подкраску, пофайловые таблицы и режим корреляции с суммарным графиком. На практике аналитик повторяет одни и те же наборы фильтров для похожих инцидентов, хочет «запомнить» удачный вид, отмечать конкретные записи/моменты (для передачи разбора), искать по паттернам (`ORA-\d+`, `^<.*>$`) и по атрибутам записей (`attrs` JSON: user/session/host), а на корреляции видеть вклад каждого файла в общий всплеск (стекированный график), а не только сумму. Источник — SQLite (MVP), JSON1 + скалярная REGEXP через `modernc.org/sqlite`. Go первичен; Python — полный функционал по запросу. Контракт — в `architect/specs/viewer.spec.md` (0.3.0): `presets`, `annotations`, расширение `search`/`correlate` (`mode`/`attrs`), `histogram-by-file`, миграция 0004.

## Что добавляется

- **Пресеты:** `GET/POST /api/uploads/{id}/presets`, `DELETE …/{pid}`; пресет — `{name, snapshot}` где `snapshot` — JSON-снимок состояния просмотра (`searchFilters`, `timeline`, `highlights`, `selectedFileIds`, `correlateMode`, `pageSize`). Таблица `t_view_presets` (FK CASCADE по `upload_id`). Снимок на момент сохранения; правки позже не синхронизируются; загрузка заменяет состояние (confirm если highlights/фильтры непусты).
- **Аннотации/пин-точки:** `GET/POST /api/uploads/{id}/annotations`, `DELETE …/{aid}`; два типа пина: **entry-pin** (`file_analyze_id` + `entry_id`, БЕЗ FK — dangling допускается, переживает re-ингест файла) и **time-pin** (`ts`); `note`+`color` обязательны; ровно один тип пина, смешанное/половинчатое → 400. Таблица `t_annotations` (FK CASCADE по `upload_id`, индексы по `upload_id` и `ts`). Entry-pin инициируется кнопкой 📌 в строке таблицы; dangling → бейдж «вне страницы/удалена»; time-pin → вертикальный маркер на стекированном графике.
- **Поиск по регулярке/attrs:** `search`/`correlate` расширяются параметрами `mode=text|regex` и `attrs=k1:v1,k2:v2`. `mode=regex` — серверная скалярная функция `REGEXP` (modernc `RegisterScalarFunction`, package-level `sync.Once` + `sync.Map`-кеш `*regexp.Regexp`), OR по полям (all: ts_raw/level/component/message/raw_line; raw: raw_line); невалидный паттерн → `regexp.Compile` в Go → 400 (понятное сообщение). `attrs` → `AND json_extract(attrs, '$.k') LIKE '%v%'` (JSON1 встроен); отсутствующий ключ → NULL → исключение; пустое значение `k:` → «ключ существует». В `correlate` предикаты regex/attrs идут в общий `where` (COUNT-safe: `total` и `items` согласованы).
- **Стекированный по файлам график:** `GET /api/uploads/{id}/histogram-by-file?bucket=&from=&to=&files=` — `GROUP BY bucket, file_analyze_id` → `[{bucket, file_analyze_id, count}]`; фронт агрегирует в стекированные сегменты (цвет из стабильной палитры `FILE_PALETTE` по индексу в `fileIds`, цикл `% len` при >10 файлов). В режиме корреляции — над таблицей; per-file таблицы (не-correlate) — `PerFileChartComponent` без изменений.
- **Хотфикс 500 (попутно):** `scanFileRows` сканировал 13 приёмников против 16 колонок `getFiles`-SELECT → 500 на каждом `GET /api/files?upload_id=…` (пустой селектор файлов). Выровнен под 16 колонок. Регрессионный тест `TestGetFiles`.
- **Переименование UI:** «Подкраска» → «Подсветить» (5 вхождений во фронте + USAGE.md).

## Acceptance criteria

_Все критерии приёмки проверены (release 0.6.0): см. `changelog/CHANGELOG.md#0.6.0` и `release/backend/go/build/devagent.provenance.json`._

- [x] AC-1: `search?mode=regex&q=…` возвращает записи, совпадающие с регуляркой по полям; невалидный паттерн (`[bad(`) → 400 с понятным сообщением (не 500, не SQLite-ошибка).
- [x] AC-2: `search?attrs=user:alice` → только записи с `json_extract(attrs,'$.user') LIKE '%alice%'`; отсутствующий ключ → 0 строк; несколько пар → AND; `correlate?attrs=…` — то же, и `total` согласован с числом `items` (COUNT-safe).
- [x] AC-3: `GET /api/uploads/{id}/histogram-by-file?bucket=hour&files=f1,f2` → `[{bucket, file_analyze_id, count}]` с per-file сегментами; `from`/`to`-окно работает.
- [x] AC-4: Пресеты CRUD: `POST` создаёт (`{name, snapshot}` → 201, `{id, name, snapshot, created_at}`), `GET` список, `DELETE` → 204; snapshot round-trip как JSON.
- [x] AC-5: Аннотации CRUD: entry-pin (`file_analyze_id`+`entry_id`, `ts=null`) и time-pin (`ts`, null file/entry) создаются → 201; nullable-поля → JSON `null`; `note`/`color` обязательны; смешанный/половинчатый пин → 400; `DELETE` → 204; `GET` список.
- [x] AC-6: CASCADE: удаление загрузки → `GET presets`/`annotations` → `[]` (FK по `upload_id`); DSN `foreign_keys=1` на каждом соединении пула (footer aggregates пересчитываются после delete файла/upload).
- [x] AC-7: REGEXP-регистрация идемпотентна (`sync.Once`): `db.Open` дважды в одном процессе — без ошибки «already registered», REGEXP работает на обоих соединениях.
- [x] AC-8: Frontend: режим поиска text/regex + поле attrs; чипы с бейджами `[regex]`/`[attrs:…]`; бар пресетов (выбор/загрузить/удалить/сохранить); панель аннотаций (список + форма time-pin + 📌 entry-pin в строках); стекированный график в режиме корреляции с time-pin маркерами; `FILE_PALETTE` извлечена в `palette.ts` (без дублирования).
- [x] AC-9: `go build/vet/gofmt/test -race` зелёные (новые тесты: `TestGetFiles`, `TestSearchRegex`, `TestSearchAttrs`, `TestCorrelateRegex`, `TestHistogramByFile`, `TestPresetsCRUD`, `TestAnnotationsCRUD`, `TestREGEXPRegistrationIdempotent`, расширенный `TestDeleteUploadCascade`); `ng build`/`ng test` (40/40) зелёные.
- [x] AC-10: `la.conf` — без новых обязательных полей; `LA_CONF`/env переопределения не затрагиваются; Go-плагины (парсеры/постобработчики) — не затрагиваются.

## Non-functional requirements

- REGEXP — серверная скалярная функция; кеш `*regexp.Regexp` процесс-глобальный (`sync.Map`); `sync.Once` для регистрации (повторная регистрация в modernc возвращает ошибку).
- `json_extract` (JSON1) встроен в `modernc.org/sqlite` — без внешних расширений.
- COUNT-safe: regex/attrs-предикаты в общем `where` для `correlate` (и COUNT, и records).
- Аннотации entry-pin БЕЗ FK → dangling допускается (переживает re-ингест); CASCADE только по `upload_id`.
- Пресет — снимок на момент сохранения; правки не синхронизируются; хранится как один JSON-blob.
- Стекированный график — клиент-сайд агрегация плоских точек; цвет стабилен per file-id по индексу (цикл при >10 файлов).
- Секреты не в репозитории; `la.conf`/`la.db` — runtime/сгенерированные.
- Python-релиз — те же эндпоинты/таблицы/контракт (полный функционал по запросу).
- Поставка — только ZIP; Node только build-time.

## Зависимости

- US-0002 (`t_files_analyze`/`t_log_entries` + `attrs` JSON), US-0003 (селектор, подкраска, график, «новое окно»), US-0004 (`timeline`, `histogram?files=`, `search`-фильтры, `filters`/`highlights` CRUD), US-0005 (`correlate`, режим корреляции, палитра файлов) — расширяются/переиспользуются.

## Открытые вопросы (уровень спецификации)

- Версионирование/дифф пресетов, импорт-экспорт — на потом (MVP: один snapshot на пресет).
- Аннотации: привязка к диапазону ts (не точка), теги/группировка — на потом (MVP: точечные пины).
- Полнотекстовый FTS5 (поверх LIKE/REGEXP) — отдельная ЮС.
- Цветовая палитра файлов — детерминированная по индексу (расширяемо).