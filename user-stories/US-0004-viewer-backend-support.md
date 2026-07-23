---
id: US-0004
title: Просмотрщик логов — backend-поддержка (расширение API + персистентность)
status: ready
priority: must
domain: viewer
project: [backend]
target: [go]
spec_ref: architect/specs/viewer.spec.md
created: 2026-07-23
updated: 2026-07-23
---

# US-0004 — Просмотрщик логов — backend-поддержка

## Как бэкенд-разработчик
## я хочу реализовать серверные эндпоинты и таблицы персистентности состояния просмотра, требуемые фронтенд-просмотрщиком (US-0003)
## чтобы фронтенд мог искать по нескольким файлам, показывать таймлайн/гистограммы и сохранять фильтры/подкраску между рестартами.

## Контекст

Backend-расширение домена `viewer`, вызванное ФР фронтенда (US-0003). Переиспользует и расширяет эндпоинты ингестии (US-0002). Источник данных — SQLite (MVP). Go первичен; Python — полный функционал по запросу. Контракт — в `architect/specs/viewer.spec.md` (`backend`).

## Что добавляется (производно из ФР US-0003)

- **Агрегаты таблицы загрузок:** `GET /api/stats` — размер хранилища, число загрузок/файлов/записей; `GET /api/uploads` — расширить sort/filter и meta-агрегаты; `GET /api/uploads/{id}` — summary (один лог → `t_files_analyze.summary`; архив → число файлов) + `first_ts`/`last_ts` (границы таймлайна).
- **Мульти-файловый поиск:** `GET /api/uploads/{id}/search?q=&files=&fields=all|raw&limit&offset` — поиск по тексту по всем полям (+ опц. исходное `raw_line`) по выбранным файлам; возвращает записи с `file_analyze_id`. Поиск — `LIKE` (FTS5 — отдельная ЮС).
- **Границы таймлайна:** `GET /api/uploads/{id}/timeline` — min/max `ts` по выборке файлов.
- **Кластерные лексемы:** `GET /api/uploads/{id}/lexemes?files=&limit=` — топ-N токенов по выборке (MVP: простая частотность, без внешних ML-зависимостей) — для предложений подкраски.
- **Гистограмма событий:** `GET /api/uploads/{id}/histogram?bucket=month|day|hour|minute&from=&to=&files=` — `GROUP BY bucket(ts)` по `t_log_entries`; видеть всплески.
- **Персистентность фильтров/подкраски (миграция 0003):** таблицы `t_view_filters` (kind=search|timeline, rule JSON) и `t_view_highlights` (text, color, lexeme) — per upload, `ON DELETE CASCADE` от `t_files_upload`; survives restarts.
  - `GET/POST/DELETE /api/uploads/{id}/filters[/{fid}]`, `…/highlights[/{hid}]`, `DELETE /api/uploads/{id}/view-state` (кнопка очистки).
- Переиспользуются без изменения: `DELETE /api/uploads/{id}` (каскад дополнить до `t_view_*`), `GET /api/files`, `GET /api/files/{id}`, `GET /api/files/{id}/entries?limit&offset&level&from&to&q`, `GET /api/parsers`.

## Acceptance criteria

- [ ] AC-1: `GET /api/stats` и расширение `GET /api/uploads`/`GET /api/uploads/{id}` возвращают агрегаты (storage_size, upload/file/record_count), summary (один лог | архив-файл-count), first_ts/last_ts.
- [ ] AC-2: `GET /api/uploads/{id}/search` — мульти-файловый поиск по полям (all | raw), пагинация, возвращает записи с `file_analyze_id`; лимит по числу файлов/записей.
- [ ] AC-3: `GET /api/uploads/{id}/timeline` — min/max `ts` по выборке файлов (NULL-безопасно, если дат нет).
- [ ] AC-4: `GET /api/uploads/{id}/lexemes` — топ-N токенов по выборке (MVP частотность).
- [ ] AC-5: `GET /api/uploads/{id}/histogram?bucket=...` — счёт по бакетам month|day|hour|minute; пустой выборка → пустой ряд.
- [ ] AC-6: Миграция 0003 — `t_view_filters`, `t_view_highlights` (per upload, CASCADE); идемпотентна.
- [ ] AC-7: `GET/POST/DELETE` filters/highlights + `DELETE /api/uploads/{id}/view-state`; `DELETE /api/uploads/{id}` каскадно чистит `t_view_*`.
- [ ] AC-8: `go build/vet/gofmt/test -race/cover` — зелёные; тесты на агрегаты/поиск/гистограмму/персистентность (изоляция `t.TempDir()`).
- [ ] AC-9: `la.conf`/`la.conf.template` — без новых обязательных полей (параметры поиска/гистограммы — query, дефолты в коде); при необходимости — `LA_VIEW_MAX_FILES`/`LA_VIEW_MAX_RECORDS` (опц.).

## Non-functional requirements

- Мульти-файловый поиск/гистограмма — серверная, потоковая агрегация; лимиты по числу файлов/записей (защита от тяжёлых выборок).
- Поиск — `LIKE` (FTS5 — отдельная ЮС); индексы `t_log_entries(ts)`, `(level)` используются.
- Персистентность per upload; удаление загрузки каскадно чистит view-state (FK CASCADE).
- Секреты не в репозитории.
- Python-релиз — те же эндпоинты/таблицы (полный функционал по запросу).
- Ограничения Go-плагинов (парсеры/постобработчики) — не затрагиваются.

## Зависимости

- US-0002 (таблицы ингестии `t_files_upload`/`t_files_analyze`/`t_log_entries`, эндпоинты, сводки постобработки) — расширяются и переиспользуются.

## Открытые вопросы (уровень спецификации)

- Алгоритм кластерных лексем — закрыт на уровне MVP (топ-N токенов по частоте, нормализация lower-case, стоп-слова минимальные); уточняется по реализации.
- Лимиты мульти-файлового поиска/гистограммы — дефолты в коде, опц. env.
- FTS5 — отдельная ЮС.