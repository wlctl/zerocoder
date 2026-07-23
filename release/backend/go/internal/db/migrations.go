package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Migration — одна миграция схемы: version (упорядоченный идентификатор) и DDL.
type Migration struct {
	Version     string
	Description string
	Up          []string // DDL-выражения (CREATE TABLE IF NOT EXISTS ...)
}

// migrations — упорядоченный список миграций. Новые ЮС добавляют миграции сюда.
var migrations = []Migration{
	{
		Version:     "0001",
		Description: "baseline la_meta",
		Up: []string{
			`CREATE TABLE IF NOT EXISTS la_meta (
				key   TEXT PRIMARY KEY,
				value TEXT NOT NULL
			)`,
		},
	},
	{
		// US-0002: загрузка и ингестия лог-файлов.
		// Источник: architect/specs/ingestion.spec.md (db.migration 0002).
		Version:     "0002",
		Description: "ingestion tables: t_files_upload, t_files_analyze, t_log_entries",
		Up: []string{
			`CREATE TABLE IF NOT EXISTS t_files_upload (
				id          TEXT PRIMARY KEY,
				filename    TEXT NOT NULL,
				md5         TEXT NOT NULL UNIQUE,
				size_bytes  INTEGER NOT NULL,
				kind        TEXT NOT NULL,
				status      TEXT NOT NULL,
				uploaded_at TEXT NOT NULL,
				error       TEXT
			)`,
			`CREATE TABLE IF NOT EXISTS t_files_analyze (
				id              TEXT PRIMARY KEY,
				upload_id       TEXT NOT NULL REFERENCES t_files_upload(id) ON DELETE CASCADE,
				filename        TEXT NOT NULL,
				path_in_archive TEXT,
				md5             TEXT,
				format          TEXT NOT NULL,
				status          TEXT NOT NULL,
				record_count    INTEGER NOT NULL DEFAULT 0,
				parsed_at       TEXT,
				encoding        TEXT,
				first_ts        TEXT,
				last_ts         TEXT,
				duration_sec    INTEGER,
				pp_status       TEXT,
				pp_at           TEXT,
				summary         TEXT,
				error           TEXT
			)`,
			`CREATE INDEX IF NOT EXISTS idx_files_analyze_upload ON t_files_analyze(upload_id)`,
			`CREATE TABLE IF NOT EXISTS t_log_entries (
				id              INTEGER PRIMARY KEY AUTOINCREMENT,
				file_analyze_id TEXT NOT NULL REFERENCES t_files_analyze(id) ON DELETE CASCADE,
				seq             INTEGER NOT NULL,
				format          TEXT NOT NULL,
				ts              TEXT,
				ts_raw          TEXT,
				tz_offset       TEXT,
				tz_inferred     INTEGER NOT NULL DEFAULT 0,
				level           TEXT,
				component       TEXT,
				message         TEXT,
				raw_line        TEXT NOT NULL,
				attrs           TEXT
			)`,
			`CREATE INDEX IF NOT EXISTS idx_log_entries_file ON t_log_entries(file_analyze_id, seq)`,
			`CREATE INDEX IF NOT EXISTS idx_log_entries_ts ON t_log_entries(ts)`,
			`CREATE INDEX IF NOT EXISTS idx_log_entries_level ON t_log_entries(level)`,
		},
	},
	{
		// US-0004: персистентность состояния просмотра (фильтры/подкраска).
		// Источник: architect/specs/viewer.spec.md (backend.persistence migration 0003).
		Version:     "0003",
		Description: "viewer persistence tables: t_view_filters, t_view_highlights",
		Up: []string{
			`CREATE TABLE IF NOT EXISTS t_view_filters (
				id         TEXT PRIMARY KEY,
				upload_id  TEXT NOT NULL REFERENCES t_files_upload(id) ON DELETE CASCADE,
				kind       TEXT NOT NULL,
				rule       TEXT NOT NULL,
				created_at TEXT NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_view_filters_upload ON t_view_filters(upload_id)`,
			`CREATE TABLE IF NOT EXISTS t_view_highlights (
				id         TEXT PRIMARY KEY,
				upload_id  TEXT NOT NULL REFERENCES t_files_upload(id) ON DELETE CASCADE,
				text       TEXT NOT NULL,
				color      TEXT NOT NULL,
				lexeme     INTEGER NOT NULL DEFAULT 0,
				created_at TEXT NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_view_highlights_upload ON t_view_highlights(upload_id)`,
		},
	},
	{
		// US-0006: пресеты просмотра, аннотации-пины, regex/attrs-поиск, стекированный график.
		// Источник: architect/specs/viewer.spec.md (backend.persistence migration 0004).
		// t_annotations.file_analyze_id/entry_id — БЕЗ FK: t_log_entries/t_files_analyze
		// каскадно удаляются с upload; per-row FK заставил бы аннотации исчезать при
		// повторном ингесте файла. Допускаем dangling (frontend: бейдж «вне страницы»).
		// ts nullable → time-pin; file_analyze_id+entry_id nullable → entry-pin.
		Version:     "0004",
		Description: "viewer persistence tables: t_view_presets, t_annotations",
		Up: []string{
			`CREATE TABLE IF NOT EXISTS t_view_presets (
				id         TEXT PRIMARY KEY,
				upload_id  TEXT NOT NULL REFERENCES t_files_upload(id) ON DELETE CASCADE,
				name       TEXT NOT NULL,
				snapshot   TEXT NOT NULL,
				created_at TEXT NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_view_presets_upload ON t_view_presets(upload_id)`,
			`CREATE TABLE IF NOT EXISTS t_annotations (
				id               TEXT PRIMARY KEY,
				upload_id        TEXT NOT NULL REFERENCES t_files_upload(id) ON DELETE CASCADE,
				file_analyze_id  TEXT,
				entry_id         INTEGER,
				ts               TEXT,
				note             TEXT NOT NULL,
				color            TEXT NOT NULL,
				created_at       TEXT NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_annotations_upload ON t_annotations(upload_id)`,
			`CREATE INDEX IF NOT EXISTS idx_annotations_ts ON t_annotations(ts)`,
		},
	},
}

// Run применяет миграции к БД. Создаёт таблицу schema_migrations при отсутствии,
// затем выполняет миграции, ещё не записанные в ней. Идемпотентен.
func Run(ctx context.Context, sdb *sql.DB) error {
	if _, err := sdb.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, m := range migrations {
		applied, err := isApplied(ctx, sdb, m.Version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyMigration(ctx, sdb, m); err != nil {
			return fmt.Errorf("migration %s: %w", m.Version, err)
		}
	}
	return nil
}

// isApplied проверяет, записана ли миграция в schema_migrations.
func isApplied(ctx context.Context, sdb *sql.DB, version string) (bool, error) {
	var v string
	err := sdb.QueryRowContext(ctx,
		"SELECT version FROM schema_migrations WHERE version = ?", version).Scan(&v)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, fmt.Errorf("check migration %s: %w", version, err)
}

// applyMigration выполняет DDL миграции в транзакции и фиксирует её в schema_migrations.
func applyMigration(ctx context.Context, sdb *sql.DB, m Migration) error {
	tx, err := sdb.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, stmt := range m.Up {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
		m.Version, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("record migration %s: %w", m.Version, err)
	}
	return tx.Commit()
}
