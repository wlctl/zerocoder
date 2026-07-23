package db

import (
	"context"
	"path/filepath"
	"testing"
)

func openTempDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open("sqlite:" + filepath.Join(t.TempDir(), "la.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestOpenAutoCreatesSQLiteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.db")
	d, err := Open("sqlite:" + path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	if err := d.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestOpenRejectsPostgres(t *testing.T) {
	if _, err := Open("postgres://user@host/db"); err == nil {
		t.Fatal("ожидалась ошибка для postgres scheme")
	}
}

func TestOpenRejectsUnknownScheme(t *testing.T) {
	if _, err := Open("mysql://whatever"); err == nil {
		t.Fatal("ожидалась ошибка для неизвестного scheme")
	}
}

func TestRunCreatesSchemaMigrationsAndLaMeta(t *testing.T) {
	d := openTempDB(t)
	ctx := context.Background()
	if err := Run(ctx, d.DB); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var n int
	if err := d.QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('schema_migrations','la_meta')").Scan(&n); err != nil {
		t.Fatalf("query tables: %v", err)
	}
	if n != 2 {
		t.Errorf("создано таблиц = %d, want 2 (schema_migrations, la_meta)", n)
	}

	// миграция 0001 записана
	var ver string
	if err := d.QueryRowContext(ctx,
		"SELECT version FROM schema_migrations WHERE version='0001'").Scan(&ver); err != nil {
		t.Errorf("миграция 0001 не записана: %v", err)
	}
}

func TestRunIsIdempotent(t *testing.T) {
	d := openTempDB(t)
	ctx := context.Background()
	if err := Run(ctx, d.DB); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if err := Run(ctx, d.DB); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	// ровно одна запись о миграции
	var n int
	if err := d.QueryRowContext(ctx,
		"SELECT count(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 4 {
		t.Errorf("записей миграций = %d, want 4 (идемпотентность)", n)
	}
}

func TestLaMetaInsertWorks(t *testing.T) {
	d := openTempDB(t)
	ctx := context.Background()
	if err := Run(ctx, d.DB); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := d.ExecContext(ctx,
		"INSERT INTO la_meta (key, value) VALUES ('schema_version', '0.1.0')"); err != nil {
		t.Fatalf("insert la_meta: %v", err)
	}
	var v string
	if err := d.QueryRowContext(ctx,
		"SELECT value FROM la_meta WHERE key='schema_version'").Scan(&v); err != nil {
		t.Fatalf("select la_meta: %v", err)
	}
	if v != "0.1.0" {
		t.Errorf("la_meta value = %q", v)
	}
}

func TestMigrations0002Through0004Applied(t *testing.T) {
	d := openTempDB(t)
	ctx := context.Background()
	if err := Run(ctx, d.DB); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var n int
	if err := d.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN
		('t_files_upload','t_files_analyze','t_log_entries','t_view_filters','t_view_highlights','t_view_presets','t_annotations')`).Scan(&n); err != nil {
		t.Fatalf("query tables: %v", err)
	}
	if n != 7 {
		t.Errorf("создано таблиц = %d, want 7", n)
	}
	for _, v := range []string{"0001", "0002", "0003", "0004"} {
		var ver string
		if err := d.QueryRowContext(ctx, "SELECT version FROM schema_migrations WHERE version=?", v).Scan(&ver); err != nil {
			t.Errorf("миграция %s не записана: %v", v, err)
		}
	}
}

// TestCascadeDeleteUpload проверяет FK ON DELETE CASCADE: удаление upload
// удаляет t_files_analyze, t_log_entries, t_view_filters, t_view_highlights.
func TestCascadeDeleteUpload(t *testing.T) {
	d := openTempDB(t)
	ctx := context.Background()
	if err := Run(ctx, d.DB); err != nil {
		t.Fatalf("Run: %v", err)
	}
	_, err := d.ExecContext(ctx, `INSERT INTO t_files_upload (id, filename, md5, size_bytes, kind, status, uploaded_at)
		VALUES ('u1', 'f.log', 'md5u1', 100, 'file', 'uploaded', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert upload: %v", err)
	}
	_, err = d.ExecContext(ctx, `INSERT INTO t_files_analyze (id, upload_id, filename, format, status, record_count)
		VALUES ('a1', 'u1', 'f.log', 'text', 'pp', 1)`)
	if err != nil {
		t.Fatalf("insert analyze: %v", err)
	}
	_, err = d.ExecContext(ctx, `INSERT INTO t_log_entries (file_analyze_id, seq, format, raw_line)
		VALUES ('a1', 1, 'text', 'hello')`)
	if err != nil {
		t.Fatalf("insert entry: %v", err)
	}
	_, err = d.ExecContext(ctx, `INSERT INTO t_view_filters (id, upload_id, kind, rule, created_at)
		VALUES ('vf1', 'u1', 'search', '{}', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert filter: %v", err)
	}
	_, err = d.ExecContext(ctx, `INSERT INTO t_view_highlights (id, upload_id, text, color, lexeme, created_at)
		VALUES ('vh1', 'u1', 'err', '#f00', 0, '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert highlight: %v", err)
	}

	if _, err := d.ExecContext(ctx, "DELETE FROM t_files_upload WHERE id='u1'"); err != nil {
		t.Fatalf("delete upload: %v", err)
	}
	for _, q := range []string{
		"SELECT count(*) FROM t_files_analyze WHERE upload_id='u1'",
		"SELECT count(*) FROM t_log_entries WHERE file_analyze_id='a1'",
		"SELECT count(*) FROM t_view_filters WHERE upload_id='u1'",
		"SELECT count(*) FROM t_view_highlights WHERE upload_id='u1'",
	} {
		var n int
		if err := d.QueryRowContext(ctx, q).Scan(&n); err != nil {
			t.Fatalf("query %q: %v", q, err)
		}
		if n != 0 {
			t.Errorf("CASCADE не сработал: %q → %d (want 0)", q, n)
		}
	}
}
