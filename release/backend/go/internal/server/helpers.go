// Package server: helpers.go — общие вспомогательные методы и функции запросов.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
)

// statsAggregates возвращает агрегаты для таблицы загрузок.
func (s *Server) statsAggregates(ctx context.Context) (map[string]int64, error) {
	var storageSize, uploadCount, fileCount, recordCount int64
	_ = s.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(size_bytes),0) FROM t_files_upload").Scan(&storageSize)
	_ = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM t_files_upload").Scan(&uploadCount)
	_ = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM t_files_analyze").Scan(&fileCount)
	_ = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM t_log_entries").Scan(&recordCount)
	return map[string]int64{
		"storage_size": storageSize,
		"upload_count": uploadCount,
		"file_count":   fileCount,
		"record_count": recordCount,
	}, nil
}

// uploadFiles возвращает analyze-файлы загрузки + first/last ts + count.
func (s *Server) uploadFiles(ctx context.Context, uploadID string) ([]map[string]any, string, string, int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, filename, path_in_archive, format, status, record_count, encoding, first_ts, last_ts, duration_sec, summary
		FROM t_files_analyze WHERE upload_id=? ORDER BY filename`, uploadID)
	if err != nil {
		return nil, "", "", 0, err
	}
	defer rows.Close()
	var list []map[string]any
	var firstTs, lastTs string
	for rows.Next() {
		var (
			id, filename, format, status                  string
			pathInArchive, encoding, first, last, sumJSON sql.NullString
			durationSec                                   sql.NullInt64
			recordCount                                   int
		)
		if err := rows.Scan(&id, &filename, &pathInArchive, &format, &status, &recordCount, &encoding, &first, &last, &durationSec, &sumJSON); err != nil {
			return nil, "", "", 0, err
		}
		m := map[string]any{
			"id":              id,
			"filename":        filename,
			"path_in_archive": nullable(pathInArchive),
			"format":          format,
			"status":          status,
			"record_count":    recordCount,
			"encoding":        nullable(encoding),
			"first_ts":        nullable(first),
			"last_ts":         nullable(last),
			"duration_sec":    nullableInt(durationSec),
		}
		if sumJSON.Valid && sumJSON.String != "" {
			var sum map[string]any
			if jsonUnmarshalSafe(sumJSON.String, &sum) {
				m["summary"] = sum
			}
		}
		list = append(list, m)
		if first.Valid && (firstTs == "" || first.String < firstTs) {
			firstTs = first.String
		}
		if last.Valid && (lastTs == "" || last.String > lastTs) {
			lastTs = last.String
		}
	}
	count := len(list)
	if count == 0 {
		list = []map[string]any{}
	}
	return list, firstTs, lastTs, count, nil
}

// scanFileRows сканирует все 16 колонок t_files_analyze из getFiles-SELECT
// (id, upload_id, filename, path_in_archive, md5, format, status, record_count,
// parsed_at, encoding, first_ts, last_ts, duration_sec, pp_status, summary, error).
// Число приёмников обязано совпадать с числом колонок SELECT — иначе rows.Scan
// падает и хендлер getFiles возвращает 500 (баг 0.5.0: сканировалось 13 из 16).
func scanFileRows(rows *sql.Rows) ([]map[string]any, error) {
	var list []map[string]any
	for rows.Next() {
		var (
			id, uploadID, filename, format, status string
			pathInArchive, md5                     sql.NullString
			parsedAt, encoding                     sql.NullString
			first, last                            sql.NullString
			ppStatus, sumJSON, errVal              sql.NullString
			durationSec                            sql.NullInt64
			recordCount                            int
		)
		if err := rows.Scan(&id, &uploadID, &filename, &pathInArchive, &md5, &format, &status, &recordCount, &parsedAt, &encoding, &first, &last, &durationSec, &ppStatus, &sumJSON, &errVal); err != nil {
			return nil, err
		}
		m := map[string]any{
			"id":              id,
			"upload_id":       uploadID,
			"filename":        filename,
			"path_in_archive": nullable(pathInArchive),
			"md5":             nullable(md5),
			"format":          format,
			"status":          status,
			"record_count":    recordCount,
			"parsed_at":       nullable(parsedAt),
			"encoding":        nullable(encoding),
			"first_ts":        nullable(first),
			"last_ts":         nullable(last),
			"duration_sec":    nullableInt(durationSec),
			"pp_status":       nullable(ppStatus),
			"error":           nullable(errVal),
		}
		if sumJSON.Valid && sumJSON.String != "" {
			var sum map[string]any
			if jsonUnmarshalSafe(sumJSON.String, &sum) {
				m["summary"] = sum
			}
		}
		list = append(list, m)
	}
	if list == nil {
		list = []map[string]any{}
	}
	return list, nil
}

// fileFull — детальный объект файла анализа.
type fileFull struct {
	ID            string         `json:"id"`
	UploadID      string         `json:"upload_id"`
	Filename      string         `json:"filename"`
	PathInArchive sql.NullString `json:"-"`
	MD5           sql.NullString `json:"md5"`
	Format        string         `json:"format"`
	Status        string         `json:"status"`
	RecordCount   int            `json:"record_count"`
	ParsedAt      sql.NullString `json:"parsed_at"`
	Encoding      sql.NullString `json:"encoding"`
	FirstTs       sql.NullString `json:"first_ts"`
	LastTs        sql.NullString `json:"last_ts"`
	DurationSec   sql.NullInt64  `json:"duration_sec"`
	PpStatus      sql.NullString `json:"pp_status"`
	PpAt          sql.NullString `json:"pp_at"`
	Summary       sql.NullString `json:"-"`
	Error         sql.NullString `json:"error"`
}

// scanFileRowFull сканирует все колонки файла анализа из *sql.Row.
func scanFileRowFull(row *sql.Row) (map[string]any, error) {
	var f fileFull
	if err := row.Scan(&f.ID, &f.UploadID, &f.Filename, &f.PathInArchive, &f.MD5, &f.Format, &f.Status, &f.RecordCount, &f.ParsedAt, &f.Encoding, &f.FirstTs, &f.LastTs, &f.DurationSec, &f.PpStatus, &f.PpAt, &f.Summary, &f.Error); err != nil {
		return nil, err
	}
	m := map[string]any{
		"id":              f.ID,
		"upload_id":       f.UploadID,
		"filename":        f.Filename,
		"path_in_archive": nullable(f.PathInArchive),
		"md5":             nullable(f.MD5),
		"format":          f.Format,
		"status":          f.Status,
		"record_count":    f.RecordCount,
		"parsed_at":       nullable(f.ParsedAt),
		"encoding":        nullable(f.Encoding),
		"first_ts":        nullable(f.FirstTs),
		"last_ts":         nullable(f.LastTs),
		"duration_sec":    nullableInt(f.DurationSec),
		"pp_status":       nullable(f.PpStatus),
		"pp_at":           nullable(f.PpAt),
		"error":           nullable(f.Error),
	}
	if f.Summary.Valid && f.Summary.String != "" {
		var sum map[string]any
		if jsonUnmarshalSafe(f.Summary.String, &sum) {
			m["summary"] = sum
		}
	}
	return m, nil
}

// nullableInt возвращает int64 или nil.
func nullableInt(n sql.NullInt64) any {
	if n.Valid {
		return n.Int64
	}
	return nil
}

// nullable возвращает строку или nil для sql.NullString.
func nullable(n sql.NullString) any {
	if n.Valid {
		return n.String
	}
	return nil
}

// atoiDefault парсит s в int; при пустой/невалидной строке возвращает def.
func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// jsonUnmarshalSafe — безопасный JSON-декодер.
func jsonUnmarshalSafe(s string, out *map[string]any) bool {
	if err := jsonUnmarshal([]byte(s), out); err == nil {
		return true
	}
	return false
}

// jsonUnmarshal — обёртка над json.Unmarshal (импортируется в handlers).
func jsonUnmarshal(b []byte, out *map[string]any) error {
	return json.Unmarshal(b, out)
}
