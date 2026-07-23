# Пользовательские истории — dev-agent / LogAnalyzer (la)

Каталог историй продукта LogAnalyzer. Каждая история — **два файла** с одинаковым base-неймом `US-NNNN-kebab-short-title`:

- `US-NNNN-kebab-short-title.md` — **текстовый (человекочитаемый)** формат, канонический источник истины.
- `US-NNNN-kebab-short-title.json` — **служебный (машинный)** формат для детерминированной обработки и трассировки.

## Текстовый формат (.md)

Markdown с YAML-фронтматтером (метаданные) и телом с acceptance criteria.

```md
---
id: US-0001
title: Краткое название
status: draft | ready | in-progress | done | rejected
priority: must | should | could | wont
domain: <домен, напр. logs | ingestion | viz | auth | config>
project: backend | frontend | backend, frontend
target: [go] | [python] | [go, python] | [angular]
spec_ref: architect/specs/<domain>.spec.md
created: YYYY-MM-DD
updated: YYYY-MM-DD
---

# US-0001 — Краткое название

## Как <роль>
## я хочу <цель>
## чтобы <ценность>

## Контекст
_Дополнения, ограничения, ссылки._

## Acceptance criteria
- [ ] Критерий 1 (проверяемый)
- [ ] Критерий 2

## Non-functional requirements
- _производительность, безопасность, ограничения_

## Зависимости
- _US-XXXX, внешние сервисы_

## Открытые вопросы
- _
```

## Служебный формат (.json)

Структурированное представление той же истории. Поля:

```json
{
  "id": "US-0001",
  "title": "Краткое название",
  "status": "draft",
  "priority": "must",
  "domain": "logs",
  "project": ["backend"],
  "target": ["go"],
  "spec_ref": "architect/specs/<domain>.spec.md",
  "created": "YYYY-MM-DD",
  "updated": "YYYY-MM-DD",
  "asRole": "...",
  "wantGoal": "...",
  "soValue": "...",
  "context": "...",
  "acceptanceCriteria": [
    { "id": "AC-1", "criterion": "...", "verified": false }
  ],
  "nonFunctionalRequirements": [],
  "dependencies": [],
  "openQuestions": [],
  "trace": {
    "spec": null,
    "code": [],
    "tests": [],
    "changelog": null,
    "release": null
  }
}
```

## Правила

- Сквозная нумерация `US-NNNN`, без повторов; `.md` и `.json` одного base-нейма всегда идут в паре.
- `project` — backend / frontend / оба (независимо подключаемые и изменяемые проекты).
- `target` — язык(и) реализации: backend — Go (первично) и/или Python (вторично, по запросу, в полном объёме функционала); frontend — Angular.
- `.md` каноничен: при конфликте с `.json` приоритет у `.md`; служебный `.json` поддерживается синхронным.
- Любое изменение истории (моё или внешнее через редактор/git) подлежит проверке и далее отражается в: архитектурных артефактах/схемах (`architect/`), коде (`release/`), тестах, документации, `changelog/CHANGELOG.md` и релизе продукта; обновляется `architect/manifest.md`.
- После готовности истории (`status: ready`) она становится основой для спецификации в `architect/specs/` и записи в `architect/manifest.md`.
- Любое изменение истории → запись в `changelog/CHANGELOG.md`.