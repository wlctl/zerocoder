// Package server: handlers_viewer.go — обработчики viewer-эндпоинтов (US-0004).
//
// Формы ответов согласованы с frontend-контрактом.
package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ---- GET /api/stats ---------------------------------------------------------

func (s *Server) getStats(w http.ResponseWriter, r *http.Request) {
	meta, _ := s.statsAggregates(r.Context())
	writeJSON(w, http.StatusOK, meta)
}

// ---- GET /api/uploads/{id}/search?q=&files=&fields=all|raw&mode=text|regex&attrs=&limit&offset ----
// Возвращает массив LogEntry[] (+ file_analyze_id).
// mode=regex — серверный REGEXP (sqlite.RegisterScalarFunction, sync.Once в db.go);
// attrs=k:v,... — фильтр по json_extract(attrs, '$.k') LIKE '%v%' (NULL → строка исключена).

func (s *Server) getSearch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	search := q.Get("q")
	fields := q.Get("fields")
	if fields == "" {
		fields = "all"
	}
	mode := q.Get("mode")
	if mode == "" {
		mode = "text"
	}
	attrs := q.Get("attrs")
	limit := atoiDefault(q.Get("limit"), 100)
	if limit > 1000 {
		limit = 1000
	}
	offset := atoiDefault(q.Get("offset"), 0)
	files := parseFileList(q.Get("files"))

	var where string
	args := []any{}
	if len(files) > 0 {
		placeholders := make([]string, len(files))
		for i, f := range files {
			placeholders[i] = "?"
			args = append(args, f)
		}
		where = "file_analyze_id IN (" + strings.Join(placeholders, ",") + ")"
	} else {
		where = "file_analyze_id IN (SELECT id FROM t_files_analyze WHERE upload_id=?)"
		args = append(args, id)
	}
	if search != "" {
		if mode == "regex" {
			if _, err := regexp.Compile(search); err != nil {
				writeError(w, http.StatusBadRequest, "неверный regex: "+err.Error())
				return
			}
			if fields == "raw" {
				where += " AND REGEXP(?, raw_line)"
				args = append(args, search)
			} else {
				where += " AND (REGEXP(?, ts_raw) OR REGEXP(?, level) OR REGEXP(?, component) OR REGEXP(?, message) OR REGEXP(?, raw_line))"
				args = append(args, search, search, search, search, search)
			}
		} else {
			like := "%" + search + "%"
			if fields == "raw" {
				where += " AND raw_line LIKE ?"
				args = append(args, like)
			} else {
				where += " AND (ts_raw LIKE ? OR level LIKE ? OR component LIKE ? OR message LIKE ? OR raw_line LIKE ?)"
				args = append(args, like, like, like, like, like)
			}
		}
	}
	if attrs != "" {
		preds, aargs, err := attrsPredicates(attrs, "")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		where += preds
		args = append(args, aargs...)
	}
	query := `SELECT id, file_analyze_id, seq, format, ts, ts_raw, tz_offset, tz_inferred, level, component, message, raw_line, attrs
		FROM t_log_entries WHERE ` + where + " ORDER BY file_analyze_id, seq LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		m, err := scanLogEntry(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, m)
	}
	writeJSON(w, http.StatusOK, items)
}

// ---- GET /api/uploads/{id}/correlate?files=&from=&to=&q=&mode=&attrs=&level=&limit=&offset ----
// Корреляция по времени (US-0005): объединённый поток записей из выбранных файлов
// загрузки, отсортированный по ts кросс-файл (NULL ts — в конце), с file_analyze_id и
// filename (JOIN t_files_analyze). Фильтры: files (subset), from/to (ts-окно), q (LIKE
// или REGEXP при mode=regex), attrs (json_extract), level. Ответ: {items, total, limit, offset}.
// regex/attrs-предикаты добавляются в ОБЩИЙ where (COUNT + записи), чтобы total был COUNT-safe.

func (s *Server) getCorrelate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	search := q.Get("q")
	fields := q.Get("fields")
	if fields == "" {
		fields = "all"
	}
	mode := q.Get("mode")
	if mode == "" {
		mode = "text"
	}
	attrs := q.Get("attrs")
	level := q.Get("level")
	limit := atoiDefault(q.Get("limit"), 100)
	if limit > 1000 {
		limit = 1000
	}
	offset := atoiDefault(q.Get("offset"), 0)
	files := parseFileList(q.Get("files"))

	// Базовый WHERE: рамки одной загрузки (+ subset файлов).
	where := "f.upload_id = ?"
	args := []any{id}
	if len(files) > 0 {
		placeholders := make([]string, len(files))
		for i, f := range files {
			placeholders[i] = "?"
			args = append(args, f)
		}
		where += " AND e.file_analyze_id IN (" + strings.Join(placeholders, ",") + ")"
	}
	if from := q.Get("from"); from != "" {
		where += " AND e.ts >= ?"
		args = append(args, from)
	}
	if to := q.Get("to"); to != "" {
		where += " AND e.ts <= ?"
		args = append(args, to)
	}
	if level != "" {
		where += " AND e.level = ?"
		args = append(args, level)
	}
	if search != "" {
		if mode == "regex" {
			if _, err := regexp.Compile(search); err != nil {
				writeError(w, http.StatusBadRequest, "неверный regex: "+err.Error())
				return
			}
			if fields == "raw" {
				where += " AND REGEXP(?, e.raw_line)"
				args = append(args, search)
			} else {
				where += " AND (REGEXP(?, e.ts_raw) OR REGEXP(?, e.level) OR REGEXP(?, e.component) OR REGEXP(?, e.message) OR REGEXP(?, e.raw_line))"
				args = append(args, search, search, search, search, search)
			}
		} else {
			like := "%" + search + "%"
			if fields == "raw" {
				where += " AND e.raw_line LIKE ?"
				args = append(args, like)
			} else {
				where += " AND (e.ts_raw LIKE ? OR e.level LIKE ? OR e.component LIKE ? OR e.message LIKE ? OR e.raw_line LIKE ?)"
				args = append(args, like, like, like, like, like)
			}
		}
	}
	if attrs != "" {
		preds, aargs, err := attrsPredicates(attrs, "e.")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		where += preds
		args = append(args, aargs...)
	}

	// Сначала total (без limit/offset).
	var total int
	if err := s.db.QueryRowContext(r.Context(),
		"SELECT COUNT(*) FROM t_log_entries e JOIN t_files_analyze f ON f.id = e.file_analyze_id WHERE "+where,
		args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Записи: JOIN за filename, сортировка по ts кросс-файл (NULL — в конце).
	query := `SELECT e.id, e.file_analyze_id, f.filename, e.seq, e.format, e.ts, e.ts_raw,
		e.tz_offset, e.tz_inferred, e.level, e.component, e.message, e.raw_line, e.attrs
		FROM t_log_entries e JOIN t_files_analyze f ON f.id = e.file_analyze_id
		WHERE ` + where + `
		ORDER BY e.ts IS NULL, e.ts, e.seq
		LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(r.Context(), query, append(args, limit, offset)...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var (
			entryID                                                      int
			fileAnalyzeID, filename                                      string
			seq                                                          int
			format                                                       string
			ts, tsRaw, tzOffset, lvl, component, message, raw, attrsJSON sql.NullString
			tzInferred                                                   int
		)
		if err := rows.Scan(&entryID, &fileAnalyzeID, &filename, &seq, &format, &ts, &tsRaw,
			&tzOffset, &tzInferred, &lvl, &component, &message, &raw, &attrsJSON); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		m := map[string]any{
			"id":              entryID,
			"file_analyze_id": fileAnalyzeID,
			"filename":        filename,
			"seq":             seq,
			"format":          format,
			"ts":              nullable(ts),
			"ts_raw":          nullable(tsRaw),
			"tz_offset":       nullable(tzOffset),
			"tz_inferred":     tzInferred,
			"level":           nullable(lvl),
			"component":       nullable(component),
			"message":         nullable(message),
			"raw_line":        raw.String,
		}
		if attrsJSON.Valid && attrsJSON.String != "" {
			var attrs map[string]any
			if json.Unmarshal([]byte(attrsJSON.String), &attrs) == nil {
				m["attrs"] = attrs
			} else {
				m["attrs"] = attrsJSON.String
			}
		} else {
			m["attrs"] = map[string]any{}
		}
		items = append(items, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// ---- GET /api/uploads/{id}/timeline?files= → {min_ts, max_ts} ---------------

func (s *Server) getTimeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	files := parseFileList(r.URL.Query().Get("files"))
	var where string
	args := []any{}
	if len(files) > 0 {
		placeholders := make([]string, len(files))
		for i, f := range files {
			placeholders[i] = "?"
			args = append(args, f)
		}
		where = "file_analyze_id IN (" + strings.Join(placeholders, ",") + ")"
	} else {
		where = "file_analyze_id IN (SELECT id FROM t_files_analyze WHERE upload_id=?)"
		args = append(args, id)
	}
	var minTs, maxTs sql.NullString
	_ = s.db.QueryRowContext(r.Context(),
		"SELECT MIN(ts), MAX(ts) FROM t_log_entries WHERE ts IS NOT NULL AND "+where, args...).
		Scan(&minTs, &maxTs)
	resp := map[string]any{"min_ts": nil, "max_ts": nil}
	if minTs.Valid {
		resp["min_ts"] = minTs.String
	}
	if maxTs.Valid {
		resp["max_ts"] = maxTs.String
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---- GET /api/uploads/{id}/lexemes?files=&limit= → [{term, count}] ----------

func (s *Server) getLexemes(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	limit := atoiDefault(r.URL.Query().Get("limit"), 20)
	if limit > 200 {
		limit = 200
	}
	files := parseFileList(r.URL.Query().Get("files"))
	var where string
	args := []any{}
	if len(files) > 0 {
		placeholders := make([]string, len(files))
		for i, f := range files {
			placeholders[i] = "?"
			args = append(args, f)
		}
		where = "file_analyze_id IN (" + strings.Join(placeholders, ",") + ")"
	} else {
		where = "file_analyze_id IN (SELECT id FROM t_files_analyze WHERE upload_id=?)"
		args = append(args, id)
	}
	query := "SELECT message, raw_line FROM t_log_entries WHERE " + where
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	counts := map[string]int{}
	stop := stopWords()
	for rows.Next() {
		var msg, raw sql.NullString
		if err := rows.Scan(&msg, &raw); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		text := msg.String + " " + raw.String
		for _, tok := range tokenize(text) {
			if stop[tok] {
				continue
			}
			counts[tok]++
		}
	}
	type tc struct {
		Term  string `json:"term"`
		Count int    `json:"count"`
	}
	out := make([]tc, 0, len(counts))
	for t, c := range counts {
		out = append(out, tc{Term: t, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Term < out[j].Term
	})
	if len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []tc{}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- GET /api/uploads/{id}/histogram?bucket=...&from=&to=&files= → [{bucket,count}] ----

func (s *Server) getHistogram(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	bucket := q.Get("bucket")
	if bucket == "" {
		bucket = "day"
	}
	files := parseFileList(q.Get("files"))
	var where string
	args := []any{}
	if len(files) > 0 {
		placeholders := make([]string, len(files))
		for i, f := range files {
			placeholders[i] = "?"
			args = append(args, f)
		}
		where = "ts IS NOT NULL AND file_analyze_id IN (" + strings.Join(placeholders, ",") + ")"
	} else {
		where = "ts IS NOT NULL AND file_analyze_id IN (SELECT id FROM t_files_analyze WHERE upload_id=?)"
		args = append(args, id)
	}
	if from := q.Get("from"); from != "" {
		where += " AND ts >= ?"
		args = append(args, from)
	}
	if to := q.Get("to"); to != "" {
		where += " AND ts <= ?"
		args = append(args, to)
	}
	// SQLite strftime для группировки по бакетам.
	bucketExpr := bucketExprFor(bucketFmt(bucket))
	query := "SELECT " + bucketExpr + " AS b, COUNT(*) FROM t_log_entries WHERE " + where + " GROUP BY b ORDER BY b"
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	type bc struct {
		Bucket string `json:"bucket"`
		Count  int    `json:"count"`
	}
	out := []bc{}
	for rows.Next() {
		var b sql.NullString
		var c int
		if err := rows.Scan(&b, &c); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, bc{Bucket: b.String, Count: c})
	}
	writeJSON(w, http.StatusOK, out)
}

// bucketExprFor возвращает SQL-выражение для бакета по формату strftime.
func bucketExprFor(fmt string) string {
	return "strftime('" + fmt + "', ts)"
}

// bucketFmt переводит имя бакета (month/day/hour/minute) в strftime-формат.
// Единый источник формата для getHistogram и getHistogramByFile.
func bucketFmt(bucket string) string {
	switch bucket {
	case "month":
		return "%Y-%m"
	case "day":
		return "%Y-%m-%d"
	case "hour":
		return "%Y-%m-%dT%H"
	case "minute":
		return "%Y-%m-%dT%H:%M"
	default:
		return "%Y-%m-%d"
	}
}

// ---- GET /api/uploads/{id}/histogram-by-file?bucket=&from=&to=&files= → [{bucket, file_analyze_id, count}] ----
// Стекированный по файлам график (US-0006): как histogram, но с разбивкой по
// file_analyze_id в SELECT+GROUP BY. Фильтры: files (subset), from/to (ts-окно).

func (s *Server) getHistogramByFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	bucket := q.Get("bucket")
	if bucket == "" {
		bucket = "day"
	}
	files := parseFileList(q.Get("files"))
	var where string
	args := []any{}
	if len(files) > 0 {
		placeholders := make([]string, len(files))
		for i, f := range files {
			placeholders[i] = "?"
			args = append(args, f)
		}
		where = "ts IS NOT NULL AND file_analyze_id IN (" + strings.Join(placeholders, ",") + ")"
	} else {
		where = "ts IS NOT NULL AND file_analyze_id IN (SELECT id FROM t_files_analyze WHERE upload_id=?)"
		args = append(args, id)
	}
	if from := q.Get("from"); from != "" {
		where += " AND ts >= ?"
		args = append(args, from)
	}
	if to := q.Get("to"); to != "" {
		where += " AND ts <= ?"
		args = append(args, to)
	}
	bucketExpr := bucketExprFor(bucketFmt(bucket))
	query := "SELECT " + bucketExpr + " AS b, file_analyze_id, COUNT(*) FROM t_log_entries WHERE " + where +
		" GROUP BY b, file_analyze_id ORDER BY b, file_analyze_id"
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	type seg struct {
		Bucket        string `json:"bucket"`
		FileAnalyzeID string `json:"file_analyze_id"`
		Count         int    `json:"count"`
	}
	out := []seg{}
	for rows.Next() {
		var b sql.NullString
		var fa string
		var c int
		if err := rows.Scan(&b, &fa, &c); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, seg{Bucket: b.String, FileAnalyzeID: fa, Count: c})
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- Filters (GET/POST/DELETE) ---------------------------------------------

func (s *Server) getFilters(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rows, err := s.db.QueryContext(r.Context(),
		"SELECT id, upload_id, kind, rule, created_at FROM t_view_filters WHERE upload_id=? ORDER BY created_at", id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var fid, uploadID, kind, rule, createdAt string
		if err := rows.Scan(&fid, &uploadID, &kind, &rule, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var ruleObj any
		if json.Unmarshal([]byte(rule), &ruleObj) != nil {
			ruleObj = rule
		}
		out = append(out, map[string]any{
			"id": fid, "upload_id": uploadID, "kind": kind, "rule": ruleObj, "created_at": createdAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) postFilter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Kind string          `json:"kind"`
		Rule json.RawMessage `json:"rule"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "неверный JSON: "+err.Error())
		return
	}
	if body.Kind == "" || len(body.Rule) == 0 {
		writeError(w, http.StatusBadRequest, "нужны kind и rule")
		return
	}
	fid := uuid.NewString()
	createdAt := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(r.Context(),
		"INSERT INTO t_view_filters (id, upload_id, kind, rule, created_at) VALUES (?, ?, ?, ?, ?)",
		fid, id, body.Kind, string(body.Rule), createdAt); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var ruleObj any
	if json.Unmarshal(body.Rule, &ruleObj) != nil {
		ruleObj = string(body.Rule)
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": fid, "upload_id": id, "kind": body.Kind, "rule": ruleObj, "created_at": createdAt,
	})
}

func (s *Server) deleteFilter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	fid := r.PathValue("fid")
	res, err := s.db.ExecContext(r.Context(), "DELETE FROM t_view_filters WHERE id=? AND upload_id=?", fid, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "фильтр не найден")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Highlights (GET/POST/DELETE) -------------------------------------------

func (s *Server) getHighlights(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rows, err := s.db.QueryContext(r.Context(),
		"SELECT id, upload_id, text, color, lexeme, created_at FROM t_view_highlights WHERE upload_id=? ORDER BY created_at", id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var hid, uploadID, text, color, createdAt string
		var lexeme int
		if err := rows.Scan(&hid, &uploadID, &text, &color, &lexeme, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, map[string]any{
			"id": hid, "upload_id": uploadID, "text": text, "color": color, "lexeme": lexeme, "created_at": createdAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) postHighlight(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Text   string `json:"text"`
		Color  string `json:"color"`
		Lexeme int    `json:"lexeme"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "неверный JSON: "+err.Error())
		return
	}
	if body.Text == "" || body.Color == "" {
		writeError(w, http.StatusBadRequest, "нужны text и color")
		return
	}
	hid := uuid.NewString()
	createdAt := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(r.Context(),
		"INSERT INTO t_view_highlights (id, upload_id, text, color, lexeme, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		hid, id, body.Text, body.Color, body.Lexeme, createdAt); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": hid, "upload_id": id, "text": body.Text, "color": body.Color, "lexeme": body.Lexeme, "created_at": createdAt,
	})
}

func (s *Server) deleteHighlight(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	hid := r.PathValue("hid")
	res, err := s.db.ExecContext(r.Context(), "DELETE FROM t_view_highlights WHERE id=? AND upload_id=?", hid, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "правило не найдено")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- DELETE /api/uploads/{id}/view-state (204) ------------------------------

func (s *Server) deleteViewState(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, _ = s.db.ExecContext(r.Context(), "DELETE FROM t_view_filters WHERE upload_id=?", id)
	_, _ = s.db.ExecContext(r.Context(), "DELETE FROM t_view_highlights WHERE upload_id=?", id)
	w.WriteHeader(http.StatusNoContent)
}

// ---- Presets (GET/POST/DELETE) — US-0006 -----------------------------------
// Снимок состояния просмотра (фильтры/таймлайн/подсветка/выбранные файлы/режим)
// как именованный JSON-blob в t_view_presets. Загрузка восстанавливает состояние;
// snapshot — копия на момент сохранения (правки позже не синхронизируются).

func (s *Server) getPresets(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rows, err := s.db.QueryContext(r.Context(),
		"SELECT id, upload_id, name, snapshot, created_at FROM t_view_presets WHERE upload_id=? ORDER BY created_at", id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var pid, uploadID, name, snapshot, createdAt string
		if err := rows.Scan(&pid, &uploadID, &name, &snapshot, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var snapObj any
		if json.Unmarshal([]byte(snapshot), &snapObj) != nil {
			snapObj = snapshot
		}
		out = append(out, map[string]any{
			"id": pid, "upload_id": uploadID, "name": name, "snapshot": snapObj, "created_at": createdAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) postPreset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Name     string          `json:"name"`
		Snapshot json.RawMessage `json:"snapshot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "неверный JSON: "+err.Error())
		return
	}
	if body.Name == "" || len(body.Snapshot) == 0 {
		writeError(w, http.StatusBadRequest, "нужны name и snapshot")
		return
	}
	pid := uuid.NewString()
	createdAt := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(r.Context(),
		"INSERT INTO t_view_presets (id, upload_id, name, snapshot, created_at) VALUES (?, ?, ?, ?, ?)",
		pid, id, body.Name, string(body.Snapshot), createdAt); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var snapObj any
	if json.Unmarshal(body.Snapshot, &snapObj) != nil {
		snapObj = string(body.Snapshot)
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": pid, "upload_id": id, "name": body.Name, "snapshot": snapObj, "created_at": createdAt,
	})
}

func (s *Server) deletePreset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pid := r.PathValue("pid")
	res, err := s.db.ExecContext(r.Context(), "DELETE FROM t_view_presets WHERE id=? AND upload_id=?", pid, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "пресет не найден")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Annotations (GET/POST/DELETE) — US-0006 --------------------------------
// Заметки-пины: entry-pin (file_analyze_id+entry_id) или time-pin (ts). entry_id
// без FK к t_log_entries — допускаем dangling (frontend: бейдж «вне страницы»);
// аннотации не исчезают при повторном ингесте файла.

func (s *Server) getAnnotations(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rows, err := s.db.QueryContext(r.Context(),
		"SELECT id, upload_id, file_analyze_id, entry_id, ts, note, color, created_at FROM t_annotations WHERE upload_id=? ORDER BY created_at", id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var aid, uploadID, note, color, createdAt string
		var fileAnalyzeID, ts sql.NullString
		var entryID sql.NullInt64
		if err := rows.Scan(&aid, &uploadID, &fileAnalyzeID, &entryID, &ts, &note, &color, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		m := map[string]any{
			"id":         aid,
			"upload_id":  uploadID,
			"note":       note,
			"color":      color,
			"created_at": createdAt,
		}
		m["file_analyze_id"] = nullable(fileAnalyzeID)
		m["entry_id"] = nullableInt(entryID)
		m["ts"] = nullable(ts)
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) postAnnotation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		FileAnalyzeID *string `json:"file_analyze_id"`
		EntryID       *int64  `json:"entry_id"`
		Ts            *string `json:"ts"`
		Note          string  `json:"note"`
		Color         string  `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "неверный JSON: "+err.Error())
		return
	}
	if body.Note == "" || body.Color == "" {
		writeError(w, http.StatusBadRequest, "нужны note и color")
		return
	}
	// Пин: либо ts (time-pin), либо file_analyze_id+entry_id вместе (entry-pin).
	entryPin := body.FileAnalyzeID != nil && *body.FileAnalyzeID != "" && body.EntryID != nil
	timePin := body.Ts != nil && *body.Ts != ""
	if entryPin && timePin {
		writeError(w, http.StatusBadRequest, "укажите только один тип пина: либо ts, либо file_analyze_id+entry_id")
		return
	}
	if !entryPin && !timePin {
		writeError(w, http.StatusBadRequest, "нужен либо ts (time-pin), либо file_analyze_id+entry_id (entry-pin)")
		return
	}
	// file_analyze_id и entry_id должны быть заданы вместе (без половинчатых entry-pin).
	if (body.FileAnalyzeID != nil && *body.FileAnalyzeID != "") != (body.EntryID != nil) {
		writeError(w, http.StatusBadRequest, "file_analyze_id и entry_id должны быть заданы вместе")
		return
	}
	aid := uuid.NewString()
	createdAt := time.Now().UTC().Format(time.RFC3339)
	var fa sql.NullString
	var eid sql.NullInt64
	var ts sql.NullString
	if body.FileAnalyzeID != nil && *body.FileAnalyzeID != "" {
		fa = sql.NullString{String: *body.FileAnalyzeID, Valid: true}
	}
	if body.EntryID != nil {
		eid = sql.NullInt64{Int64: *body.EntryID, Valid: true}
	}
	if body.Ts != nil && *body.Ts != "" {
		ts = sql.NullString{String: *body.Ts, Valid: true}
	}
	if _, err := s.db.ExecContext(r.Context(),
		"INSERT INTO t_annotations (id, upload_id, file_analyze_id, entry_id, ts, note, color, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		aid, id, fa, eid, ts, body.Note, body.Color, createdAt); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":              aid,
		"upload_id":       id,
		"file_analyze_id": nullable(fa),
		"entry_id":        nullableInt(eid),
		"ts":              nullable(ts),
		"note":            body.Note,
		"color":           body.Color,
		"created_at":      createdAt,
	})
}

func (s *Server) deleteAnnotation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	aid := r.PathValue("aid")
	res, err := s.db.ExecContext(r.Context(), "DELETE FROM t_annotations WHERE id=? AND upload_id=?", aid, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "аннотация не найдена")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- helpers ----------------------------------------------------------------

// parseFileList разбирает comma-separated список file_analyze_id.
func parseFileList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// attrsPredicates разбирает "k1:v1,k2:v2" в SQL-предикаты по json_extract(attrs).
// alias — префикс колонки ("" для search, "e." для correlate). Возвращает
// конкатенацию " AND json_extract(<alias>attrs, ?) LIKE ?" для каждой пары и args
// ("$.k", "%v%"). Отсутствующий ключ → json_extract NULL → NULL LIKE '%x%' = NULL
// (falsy) → строка исключена (фильтр «ключ существует со значением»).
// Пустое значение ("k:") → LIKE '%%' (фильтр «ключ существует»). MVP: без
// экранирования запятых/двоеточий в значениях.
func attrsPredicates(attrs, alias string) (string, []any, error) {
	pairs := strings.Split(attrs, ",")
	var preds []string
	var args []any
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, val, found := strings.Cut(pair, ":")
		key = strings.TrimSpace(key)
		if key == "" {
			return "", nil, fmt.Errorf("неверный attrs-фильтр %q (ожидалось key:value)", pair)
		}
		if !found {
			// "key" без ':' — трактуем как «ключ существует».
			preds = append(preds, "json_extract("+alias+"attrs, ?) LIKE ?")
			args = append(args, "$."+key, "%%")
			continue
		}
		preds = append(preds, "json_extract("+alias+"attrs, ?) LIKE ?")
		args = append(args, "$."+key, "%"+strings.TrimSpace(val)+"%")
	}
	if len(preds) == 0 {
		return "", nil, fmt.Errorf("пустой attrs-фильтр")
	}
	return " AND " + strings.Join(preds, " AND "), args, nil
}

// scanLogEntry сканирует строку t_log_entries в map (для search).
func scanLogEntry(rows *sql.Rows) (map[string]any, error) {
	var (
		entryID                                                        int
		fileAnalyzeID                                                  string
		seq                                                            int
		format                                                         string
		ts, tsRaw, tzOffset, level, component, message, raw, attrsJSON sql.NullString
		tzInferred                                                     int
	)
	if err := rows.Scan(&entryID, &fileAnalyzeID, &seq, &format, &ts, &tsRaw, &tzOffset, &tzInferred, &level, &component, &message, &raw, &attrsJSON); err != nil {
		return nil, err
	}
	m := map[string]any{
		"id":              entryID,
		"file_analyze_id": fileAnalyzeID,
		"seq":             seq,
		"format":          format,
		"ts":              nullable(ts),
		"ts_raw":          nullable(tsRaw),
		"tz_offset":       nullable(tzOffset),
		"tz_inferred":     tzInferred,
		"level":           nullable(level),
		"component":       nullable(component),
		"message":         nullable(message),
		"raw_line":        raw.String,
	}
	if attrsJSON.Valid && attrsJSON.String != "" {
		var attrs map[string]any
		if json.Unmarshal([]byte(attrsJSON.String), &attrs) == nil {
			m["attrs"] = attrs
		} else {
			m["attrs"] = attrsJSON.String
		}
	} else {
		m["attrs"] = map[string]any{}
	}
	return m, nil
}

// tokenize разбивает текст на токены (lower-case, alphanumeric).
func tokenize(s string) []string {
	s = strings.ToLower(s)
	var out []string
	cur := strings.Builder{}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			cur.WriteRune(r)
		} else {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// stopWords — минимальный набор стоп-слов для лексем.
func stopWords() map[string]bool {
	return map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "to": true, "in": true, "of": true,
		"and": true, "or": true, "for": true, "on": true, "at": true, "by": true, "with": true,
		"this": true, "that": true, "it": true, "be": true, "as": true, "from": true,
	}
}

// Гарантия импорта errors.
var _ = errors.Is
