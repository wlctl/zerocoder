// Package server: handlers_ingest.go — обработчики эндпоинтов ингестии (US-0002).
//
// Формы ответов согласованы с frontend-контрактом (см. отчёт).
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/irav/dev-agent/internal/ingest"
)

// ---- POST /api/uploads (multipart: file(s)|zip) ----------------------------

// postUploads — приём multipart с одним или несколькими файлами одновременно.
// Каждый входящий файл обрабатывается независимо: потоковый MD5, дедуп по md5,
// лимиты, распаковка (zip), парсинг, постобработка. Один файл (или один zip) —
// одна строка t_files_upload. Дубликат/ошибка одного файла не прерывает остальные.
//
// Ответ 201: {results:[{upload_id, filename, kind, md5, size_bytes, status, error?,
//
//	duplicate?, existing_upload_id?, files:[{file_analyze_id, filename,
//	path_in_archive, format, status, record_count, summary?, error?}]}]}.
//
// status ∈ {parsed, failed, duplicate}. 400 — только для malformed multipart /
// нет файлов / превышен MAX_FILE_COUNT.
func (s *Server) postUploads(w http.ResponseWriter, r *http.Request) {
	// Лимит тела запроса: до MaxFileSize * MaxFileCount (с защитой от переполнения),
	// реальный пофайловый лимит enforcement — в IngestStream (limitedBuffer).
	maxBody := s.ingest.Limit()
	if n := int64(s.ingest.MaxFileCount()); n > 0 && maxBody <= math.MaxInt64/n {
		maxBody *= n
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxBody)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "неверный multipart: "+err.Error())
		return
	}
	if r.MultipartForm == nil || len(r.MultipartForm.File) == 0 {
		writeError(w, http.StatusBadRequest, "нет файлов в запросе")
		return
	}
	type fileSrc struct {
		name string
		r    io.Reader
		c    io.Closer
	}
	var sources []fileSrc
	for _, files := range r.MultipartForm.File {
		for _, fh := range files {
			f, err := fh.Open()
			if err != nil {
				writeError(w, http.StatusBadRequest, "открытие файла: "+err.Error())
				return
			}
			sources = append(sources, fileSrc{name: fh.Filename, r: f, c: f})
		}
	}
	defer func() {
		for _, src := range sources {
			src.c.Close()
		}
	}()
	if len(sources) == 0 {
		writeError(w, http.StatusBadRequest, "нет файлов")
		return
	}
	if len(sources) > s.ingest.MaxFileCount() {
		writeError(w, http.StatusBadRequest, ingest.ErrTooManyFiles.Error())
		return
	}

	// Сохраняем исходный порядок файлов (по полю, как прислал клиент) для детерминизма.
	sort.SliceStable(sources, func(i, j int) bool { return sources[i].name < sources[j].name })

	results := make([]map[string]any, 0, len(sources))
	for _, src := range sources {
		results = append(results, s.ingestOne(r.Context(), src.name, src.r))
	}
	writeJSON(w, http.StatusCreated, map[string]any{"results": results})
}

// ingestOne обрабатывает один входящий файл и формирует элемент результата.
func (s *Server) ingestOne(ctx context.Context, filename string, r io.Reader) map[string]any {
	res, err := s.ingest.IngestStream(ctx, filename, r)
	if err != nil {
		entry := map[string]any{
			"filename": filename,
			"status":   "failed",
		}
		if errors.Is(err, ingest.ErrDuplicate) {
			entry["status"] = "duplicate"
			entry["error"] = ingest.ErrDuplicate.Error()
			entry["duplicate"] = true
			if id, ok := extractDuplicateID(err); ok {
				entry["existing_upload_id"] = id
			}
			return entry
		}
		if errors.Is(err, ingest.ErrTooLarge) {
			entry["error"] = ingest.ErrTooLarge.Error()
			return entry
		}
		entry["error"] = err.Error()
		return entry
	}
	entry := map[string]any{
		"upload_id":  res.UploadID,
		"filename":   res.Filename,
		"kind":       res.Kind,
		"md5":        res.MD5,
		"size_bytes": res.Size,
		"status":     res.Status,
	}
	if res.Error != "" {
		entry["error"] = res.Error
	}
	files := make([]map[string]any, 0, len(res.Files))
	for _, f := range res.Files {
		fe := map[string]any{
			"file_analyze_id": f.ID,
			"filename":        f.Filename,
			"format":          f.Format,
			"status":          f.Status,
			"record_count":    f.RecordCount,
		}
		if f.Error != "" {
			fe["error"] = f.Error
		}
		// Подгрузим summary/path_in_archive из БД.
		var pathInArchive, sumJSON sql.NullString
		_ = s.db.QueryRowContext(ctx,
			"SELECT path_in_archive, summary FROM t_files_analyze WHERE id=?", f.ID).
			Scan(&pathInArchive, &sumJSON)
		fe["path_in_archive"] = nullable(pathInArchive)
		if sumJSON.Valid && sumJSON.String != "" {
			var sum map[string]any
			if jsonUnmarshalSafe(sumJSON.String, &sum) {
				fe["summary"] = sum
			}
		}
		files = append(files, fe)
	}
	entry["files"] = files
	return entry
}

// extractDuplicateID пытается извлечь existing upload_id из ошибки дедупа.
func extractDuplicateID(err error) (string, bool) {
	msg := err.Error()
	// IngestStream упаковывает как "... (md5=..., id=<uuid>)"
	idx := strings.LastIndex(msg, "id=")
	if idx < 0 {
		return "", false
	}
	rest := msg[idx+3:]
	rest = strings.TrimRight(rest, ") ")
	// validate uuid
	if _, perr := uuid.Parse(rest); perr == nil {
		return rest, true
	}
	return "", false
}

// ---- GET /api/uploads (массив Upload[] + meta через /api/stats) -------------

func (s *Server) getUploads(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sortBy := q.Get("sort")
	if sortBy == "" {
		sortBy = "uploaded_at"
	}
	allowedSort := map[string]bool{
		"filename": true, "size_bytes": true, "uploaded_at": true, "status": true, "md5": true,
	}
	if !allowedSort[sortBy] {
		sortBy = "uploaded_at"
	}
	dir := "ASC"
	if strings.EqualFold(q.Get("dir"), "desc") {
		dir = "DESC"
	}
	filterText := q.Get("filter")

	query := "SELECT id, filename, md5, size_bytes, kind, status, uploaded_at, error FROM t_files_upload"
	args := []any{}
	if filterText != "" {
		query += " WHERE filename LIKE ?"
		args = append(args, "%"+filterText+"%")
	}
	query += " ORDER BY " + sortBy + " " + dir

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	list := []map[string]any{}
	for rows.Next() {
		var (
			id, filename, md5, kind, status, uploadedAt string
			size                                        int64
			errVal                                      sql.NullString
		)
		if err := rows.Scan(&id, &filename, &md5, &size, &kind, &status, &uploadedAt, &errVal); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		entry := map[string]any{
			"id":          id,
			"filename":    filename,
			"md5":         md5,
			"size_bytes":  size,
			"kind":        kind,
			"status":      status,
			"uploaded_at": uploadedAt,
		}
		if errVal.Valid {
			entry["error"] = errVal.String
		}
		list = append(list, entry)
	}
	writeJSON(w, http.StatusOK, list)
}

// ---- GET /api/uploads/{id} (UploadDetail) ----------------------------------

func (s *Server) getUpload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var (
		filename, md5, kind, status, uploadedAt string
		size                                    int64
		errVal                                  sql.NullString
	)
	err := s.db.QueryRowContext(r.Context(),
		"SELECT filename, md5, size_bytes, kind, status, uploaded_at, error FROM t_files_upload WHERE id=?", id).
		Scan(&filename, &md5, &size, &kind, &status, &uploadedAt, &errVal)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "загрузка не найдена")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// first/last ts + file_count + summary (для одиночного лога) по t_files_analyze.
	var fileCount int
	var firstTs, lastTs sql.NullString
	_ = s.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*), MIN(first_ts), MAX(last_ts) FROM t_files_analyze WHERE upload_id=?`, id).
		Scan(&fileCount, &firstTs, &lastTs)

	summary := map[string]any{}
	if kind == "file" {
		var sumJSON sql.NullString
		_ = s.db.QueryRowContext(r.Context(), "SELECT summary FROM t_files_analyze WHERE upload_id=? LIMIT 1", id).Scan(&sumJSON)
		if sumJSON.Valid && sumJSON.String != "" {
			_ = jsonUnmarshalSafe(sumJSON.String, &summary)
		}
	}
	detail := map[string]any{
		"id":          id,
		"filename":    filename,
		"md5":         md5,
		"size_bytes":  size,
		"kind":        kind,
		"status":      status,
		"uploaded_at": uploadedAt,
		"file_count":  fileCount,
		"first_ts":    nullable(firstTs),
		"last_ts":     nullable(lastTs),
		"summary":     summary,
	}
	if errVal.Valid {
		detail["error"] = errVal.String
	}
	writeJSON(w, http.StatusOK, detail)
}

// ---- DELETE /api/uploads/{id} (204, каскад FK CASCADE incl. t_view_*) -------

func (s *Server) deleteUpload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	res, err := s.db.ExecContext(r.Context(), "DELETE FROM t_files_upload WHERE id=?", id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "загрузка не найдена")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- GET /api/files?upload_id= (массив FileAnalyze[]) -----------------------

func (s *Server) getFiles(w http.ResponseWriter, r *http.Request) {
	uploadID := r.URL.Query().Get("upload_id")
	query := `SELECT id, upload_id, filename, path_in_archive, md5, format, status, record_count,
		parsed_at, encoding, first_ts, last_ts, duration_sec, pp_status, summary, error
		FROM t_files_analyze`
	args := []any{}
	if uploadID != "" {
		query += " WHERE upload_id=?"
		args = append(args, uploadID)
	}
	query += " ORDER BY filename"
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	list, err := scanFileRows(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// ---- GET /api/files/{id} (FileAnalyze) --------------------------------------

func (s *Server) getFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	row := s.db.QueryRowContext(r.Context(),
		`SELECT id, upload_id, filename, path_in_archive, md5, format, status, record_count,
		parsed_at, encoding, first_ts, last_ts, duration_sec, pp_status, pp_at, summary, error
		FROM t_files_analyze WHERE id=?`, id)
	f, err := scanFileRowFull(row)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "файл не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// ---- GET /api/files/{id}/entries (items/total/limit/offset) -----------------

func (s *Server) getFileEntries(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	limit := atoiDefault(q.Get("limit"), 100)
	if limit > 1000 {
		limit = 1000
	}
	offset := atoiDefault(q.Get("offset"), 0)

	where := "file_analyze_id=?"
	args := []any{id}
	if lvl := q.Get("level"); lvl != "" {
		where += " AND level=?"
		args = append(args, lvl)
	}
	if from := q.Get("from"); from != "" {
		where += " AND ts >= ?"
		args = append(args, from)
	}
	if to := q.Get("to"); to != "" {
		where += " AND ts <= ?"
		args = append(args, to)
	}
	if search := q.Get("q"); search != "" {
		like := "%" + search + "%"
		where += " AND (message LIKE ? OR raw_line LIKE ? OR component LIKE ?)"
		args = append(args, like, like, like)
	}

	// total.
	var total int
	countArgs := append([]any{}, args...)
	_ = s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM t_log_entries WHERE "+where, countArgs...).Scan(&total)

	query := `SELECT id, file_analyze_id, seq, format, ts, ts_raw, tz_offset, tz_inferred, level, component, message, raw_line, attrs
		FROM t_log_entries WHERE ` + where + " ORDER BY seq LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var (
			entryID                                                        int
			fileAnalyzeID                                                  string
			seq                                                            int
			format                                                         string
			ts, tsRaw, tzOffset, level, component, message, raw, attrsJSON sql.NullString
			tzInferred                                                     int
		)
		if err := rows.Scan(&entryID, &fileAnalyzeID, &seq, &format, &ts, &tsRaw, &tzOffset, &tzInferred, &level, &component, &message, &raw, &attrsJSON); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
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
		items = append(items, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// ---- POST /api/files/{id}/postprocess (пере-запуск) -------------------------

func (s *Server) postFilePostprocess(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var format string
	err := s.db.QueryRowContext(r.Context(), "SELECT format FROM t_files_analyze WHERE id=?", id).Scan(&format)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "файл не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	summary, err := s.ingest.RunPostprocessPublic(r.Context(), id, format)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "постобработка: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// ---- DELETE /api/files/{id} (204, каскад t_log_entries) ---------------------

func (s *Server) deleteFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	res, err := s.db.ExecContext(r.Context(), "DELETE FROM t_files_analyze WHERE id=?", id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "файл не найден")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- GET /api/parsers -------------------------------------------------------

func (s *Server) getParsers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"parsers":        s.parsers.Names(),
		"postprocessors": s.postproc.Names(),
	})
}
