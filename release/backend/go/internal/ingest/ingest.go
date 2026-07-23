// Package ingest реализует загрузку и ингестию лог-файлов: потоковый приём
// multipart, потоковый MD5, дедуп по md5, проверка лимитов, распаковка zip,
// detect→parse→batch INSERT t_log_entries, постобработка.
//
// Источник спецификации: architect/specs/ingestion.spec.md (flow).
// Связанная пользовательская история: US-0002.
package ingest

import (
	"archive/zip"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/irav/dev-agent/internal/parser"
	"github.com/irav/dev-agent/internal/postprocess"
)

// Limits — параметры лимитов загрузки.
type Limits struct {
	MaxFileSize  int64
	MaxFileCount int
}

// Service — сервис загрузки и ингестии.
type Service struct {
	db        *sql.DB
	parsers   *parser.Manager
	postproc  *postprocess.Manager
	limits    Limits
	defaultTZ string
}

// NewService создаёт сервис ингестии.
func NewService(db *sql.DB, parsers *parser.Manager, postproc *postprocess.Manager, limits Limits, defaultTZ string) *Service {
	return &Service{
		db: db, parsers: parsers, postproc: postproc,
		limits: limits, defaultTZ: defaultTZ,
	}
}

// Limit возвращает максимальный размер входящего файла (байты).
func (s *Service) Limit() int64 { return s.limits.MaxFileSize }

// MaxFileCount возвращает максимальное число файлов за запрос.
func (s *Service) MaxFileCount() int { return s.limits.MaxFileCount }

// RunPostprocessPublic пере-запускает постобработку файла и обновляет t_files_analyze.
func (s *Service) RunPostprocessPublic(ctx context.Context, faID, format string) (postprocess.Summary, error) {
	summary, err := s.runPostprocess(ctx, faID, format)
	if err != nil {
		return summary, err
	}
	summaryJSON, _ := postprocess.MarshalSummary(summary)
	ppAt := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx,
		`UPDATE t_files_analyze SET pp_status='done', pp_at=?, summary=?, duration_sec=?, first_ts=?, last_ts=? WHERE id=?`,
		ppAt, string(summaryJSON), summary.DurationSec, summary.FirstTs, summary.LastTs, faID)
	return summary, nil
}

// UploadResult — результат загрузки одного файла.
type UploadResult struct {
	UploadID string        `json:"upload_id"`
	Filename string        `json:"filename"`
	Kind     string        `json:"kind"`
	MD5      string        `json:"md5"`
	Size     int64         `json:"size_bytes"`
	Status   string        `json:"status"`
	Error    string        `json:"error,omitempty"`
	Files    []AnalyzeFile `json:"files,omitempty"`
}

// AnalyzeFile — краткая информация о распакованном файле.
type AnalyzeFile struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	Format      string `json:"format"`
	Status      string `json:"status"`
	RecordCount int    `json:"record_count"`
	Error       string `json:"error,omitempty"`
}

// ErrDuplicate — входящий файл уже загружен (MD5 совпадает).
var ErrDuplicate = errors.New("Файл уже был загружен ранее")

// ErrTooLarge — превышен MAX_FILE_SIZE.
var ErrTooLarge = errors.New("превышен MAX_FILE_SIZE")

// ErrTooManyFiles — превышен MAX_FILE_COUNT.
var ErrTooManyFiles = errors.New("превышен MAX_FILE_COUNT")

// IngestStream принимает один входящий файл (контент + имя), считает MD5 потоково,
// проверяет лимиты, дедуп, вставляет t_files_upload, распаковывает (zip) или
// берёт одиночный файл, парсит и постобрабатывает каждый t_files_analyze.
func (s *Service) IngestStream(ctx context.Context, filename string, r io.Reader) (*UploadResult, error) {
	// Потоковый MD5 + размер.
	h := md5.New()
	counter := &countingReader{}
	tee := io.TeeReader(r, io.MultiWriter(h, counter))

	// Читаем весь поток в буфер для распаковки/парсинга. Для MVP — буфер в памяти
	// с проверкой лимита; потоковый zip-reader требует io.ReaderAt + size.
	buf := &limitedBuffer{max: s.limits.MaxFileSize}
	if _, err := io.Copy(buf, tee); err != nil {
		if errors.Is(err, errLimitExceeded) {
			return nil, ErrTooLarge
		}
		return nil, fmt.Errorf("чтение файла: %w", err)
	}
	size := buf.Len()
	md5sum := hex.EncodeToString(h.Sum(nil))

	// Дедуп.
	var existingID string
	err := s.db.QueryRowContext(ctx, "SELECT id FROM t_files_upload WHERE md5 = ?", md5sum).Scan(&existingID)
	if err == nil {
		return nil, fmt.Errorf("%w (md5=%s, id=%s)", ErrDuplicate, md5sum, existingID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("дедуп-проверка: %w", err)
	}

	kind := "file"
	if isZipFilename(filename) || isZipContent(buf.Bytes()) {
		kind = "zip"
	}

	uploadID := uuid.NewString()
	uploadedAt := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO t_files_upload (id, filename, md5, size_bytes, kind, status, uploaded_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uploadID, filename, md5sum, size, kind, "uploaded", uploadedAt); err != nil {
		return nil, fmt.Errorf("insert t_files_upload: %w", err)
	}

	res := &UploadResult{
		UploadID: uploadID, Filename: filename, Kind: kind, MD5: md5sum, Size: size, Status: "uploaded",
	}

	if kind == "zip" {
		files, err := s.processZip(ctx, uploadID, buf.Bytes())
		if err != nil {
			res.Status = "failed"
			res.Error = err.Error()
			s.setUploadError(ctx, uploadID, "failed", err.Error())
			return res, nil
		}
		res.Files = files
	} else {
		// Одиночный файл.
		fa, err := s.processSingleFile(ctx, uploadID, filename, buf.Bytes())
		if err != nil {
			res.Status = "failed"
			res.Error = err.Error()
			s.setUploadError(ctx, uploadID, "failed", err.Error())
			return res, nil
		}
		res.Files = []AnalyzeFile{*fa}
	}

	res.Status = "parsed"
	_, _ = s.db.ExecContext(ctx, "UPDATE t_files_upload SET status=? WHERE id=?", "parsed", uploadID)
	return res, nil
}

// processSingleFile создаёт t_files_analyze 1:1, парсит и постобрабатывает.
func (s *Service) processSingleFile(ctx context.Context, uploadID, filename string, content []byte) (*AnalyzeFile, error) {
	faID := uuid.NewString()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO t_files_analyze (id, upload_id, filename, path_in_archive, format, status, record_count)
		 VALUES (?, ?, ?, NULL, 'text', 'pending', 0)`,
		faID, uploadID, filename); err != nil {
		return nil, fmt.Errorf("insert t_files_analyze: %w", err)
	}
	return s.parseAndPostprocess(ctx, faID, filename, content)
}

// processZip распаковывает zip рекурсивно, для каждого текстового файла —
// t_files_analyze + parse + postprocess.
func (s *Service) processZip(ctx context.Context, uploadID string, content []byte) ([]AnalyzeFile, error) {
	zr, err := zip.NewReader(bytesReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("zip open: %w", err)
	}
	var files []AnalyzeFile
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !isLikelyTextFilename(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			files = append(files, AnalyzeFile{Filename: f.Name, Status: "failed", Error: err.Error()})
			continue
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			files = append(files, AnalyzeFile{Filename: f.Name, Status: "failed", Error: err.Error()})
			continue
		}
		faID := uuid.NewString()
		base := path.Base(f.Name)
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO t_files_analyze (id, upload_id, filename, path_in_archive, format, status, record_count)
			 VALUES (?, ?, ?, ?, 'text', 'pending', 0)`,
			faID, uploadID, base, f.Name); err != nil {
			files = append(files, AnalyzeFile{Filename: f.Name, Status: "failed"})
			continue
		}
		fa, err := s.parseAndPostprocess(ctx, faID, base, content)
		if err != nil {
			files = append(files, AnalyzeFile{ID: faID, Filename: base, Status: "failed"})
			continue
		}
		files = append(files, *fa)
	}
	return files, nil
}

// parseAndPostprocess определяет формат, парсит потоково, батчит INSERT в
// t_log_entries, обновляет record_count/status=parsed, запускает постобработку.
func (s *Service) parseAndPostprocess(ctx context.Context, faID, filename string, content []byte) (*AnalyzeFile, error) {
	// Определение кодировки + декодирование в UTF-8.
	encoding := parser.DetectEncoding(content)
	text := parser.DecodeToUTF8(content, encoding)

	// Detect формата по sample первых ~20 непустых строк.
	sample := readSampleLines(text, 20)
	p := s.parsers.Detect(sample)
	format := "text"
	if p != nil {
		format = p.Name()
	} else {
		p = s.parsers.ParserByName("text")
	}
	if p == nil {
		return nil, fmt.Errorf("парсер text недоступен")
	}

	// Потоковый парсинг: строки по каналу, батч INSERT.
	lines := make(chan string, 64)
	parseErr := make(chan error, 1)
	go func() {
		parseErr <- emitLines(text, lines)
	}()

	recordCount, firstTs, lastTs, err := s.batchInsert(ctx, faID, format, p, lines, encoding)
	if err != nil {
		s.setAnalyzeError(ctx, faID, "failed", "", err.Error())
		return &AnalyzeFile{ID: faID, Filename: filename, Format: format, Status: "failed"}, err
	}
	if perr := <-parseErr; perr != nil {
		s.setAnalyzeError(ctx, faID, "failed", "", perr.Error())
		return &AnalyzeFile{ID: faID, Filename: filename, Format: format, Status: "failed"}, perr
	}

	parsedAt := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx,
		`UPDATE t_files_analyze SET format=?, status='parsed', record_count=?, parsed_at=?, encoding=?, first_ts=?, last_ts=? WHERE id=?`,
		format, recordCount, parsedAt, encoding, firstTs, lastTs, faID)

	// Постобработка.
	summary, ppErr := s.runPostprocess(ctx, faID, format)
	if ppErr == nil {
		summaryJSON, _ := postprocess.MarshalSummary(summary)
		ppAt := time.Now().UTC().Format(time.RFC3339)
		_, _ = s.db.ExecContext(ctx,
			`UPDATE t_files_analyze SET pp_status='done', pp_at=?, summary=?, duration_sec=? WHERE id=?`,
			ppAt, string(summaryJSON), summary.DurationSec, faID)
	} else {
		_, _ = s.db.ExecContext(ctx, `UPDATE t_files_analyze SET pp_status='failed' WHERE id=?`, faID)
	}

	return &AnalyzeFile{ID: faID, Filename: filename, Format: format, Status: "pp", RecordCount: recordCount}, nil
}

// batchInsert читает записи из парсера и вставляет батчами в t_log_entries.
func (s *Service) batchInsert(ctx context.Context, faID, format string, p parser.Parser, lines <-chan string, encoding string) (int, string, string, error) {
	const batchSize = 500
	var (
		count   int
		seq     int
		firstTs *time.Time
		lastTs  *time.Time
		pending []recordRow
		flush   func() error
	)
	flush = func() error {
		if len(pending) == 0 {
			return nil
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO t_log_entries (file_analyze_id, seq, format, ts, ts_raw, tz_offset, tz_inferred, level, component, message, raw_line, attrs)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			tx.Rollback()
			return err
		}
		for _, r := range pending {
			var tsVal sql.NullString
			if r.ts != nil {
				tsVal = sql.NullString{String: r.ts.UTC().Format(time.RFC3339Nano), Valid: true}
			}
			var attrsVal sql.NullString
			if r.attrsJSON != "" {
				attrsVal = sql.NullString{String: r.attrsJSON, Valid: true}
			}
			infer := 0
			if r.tzInferred {
				infer = 1
			}
			if _, err := stmt.ExecContext(ctx, faID, r.seq, r.format, tsVal, r.tsRaw, r.tzOffset, infer, nullIfEmpty(r.level), nullIfEmpty(r.component), nullIfEmpty(r.message), r.raw, attrsVal); err != nil {
				stmt.Close()
				tx.Rollback()
				return err
			}
		}
		stmt.Close()
		if err := tx.Commit(); err != nil {
			return err
		}
		pending = pending[:0]
		return nil
	}

	emitErr := p.Parse(lines, func(rec parser.Record) {
		seq++
		rec.Seq = seq
		if rec.Format == "" {
			rec.Format = format
		}
		if rec.Attrs == nil {
			rec.Attrs = map[string]any{}
		}
		attrsJSON, _ := jsonAttrs(rec.Attrs)
		row := recordRow{
			seq: rec.Seq, format: rec.Format, ts: rec.Ts, tsRaw: rec.TsRaw,
			tzOffset: rec.TZOffset, tzInferred: rec.TZInferred, level: rec.Level,
			component: rec.Component, message: rec.Message, raw: rec.Raw, attrsJSON: attrsJSON,
		}
		pending = append(pending, row)
		if rec.Ts != nil {
			if firstTs == nil || rec.Ts.Before(*firstTs) {
				firstTs = rec.Ts
			}
			if lastTs == nil || rec.Ts.After(*lastTs) {
				lastTs = rec.Ts
			}
		}
		if len(pending) >= batchSize {
			if err := flush(); err != nil {
				// Не можем вернуть ошибку из emit; паникуем и ловим в Parse.
				panic(fmt.Errorf("batch insert: %w", err))
			}
		}
		count++
	})
	if emitErr != nil {
		return 0, "", "", emitErr
	}
	if err := flush(); err != nil {
		return 0, "", "", err
	}
	var first, last string
	if firstTs != nil {
		first = firstTs.UTC().Format(time.RFC3339Nano)
	}
	if lastTs != nil {
		last = lastTs.UTC().Format(time.RFC3339Nano)
	}
	return count, first, last, nil
}

// runPostprocess запускает постобработчик для формата.
func (s *Service) runPostprocess(ctx context.Context, faID, format string) (postprocess.Summary, error) {
	pp := s.postproc.PostprocessorFor(format)
	entries := func(emit func(postprocess.Entry)) error {
		rows, err := s.db.QueryContext(ctx,
			`SELECT seq, ts, level, component, message, raw_line, attrs FROM t_log_entries WHERE file_analyze_id = ? ORDER BY seq`, faID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				seq       int
				ts        sql.NullString
				level     sql.NullString
				component sql.NullString
				message   sql.NullString
				raw       string
				attrsJSON sql.NullString
			)
			if err := rows.Scan(&seq, &ts, &level, &component, &message, &raw, &attrsJSON); err != nil {
				return err
			}
			var tsPtr *time.Time
			if ts.Valid {
				if t, err := time.Parse(time.RFC3339Nano, ts.String); err == nil {
					tt := t
					tsPtr = &tt
				}
			}
			var attrs map[string]any
			if attrsJSON.Valid && attrsJSON.String != "" {
				_ = jsonUnmarshalAttrs(attrsJSON.String, &attrs)
			}
			emit(postprocess.Entry{
				Seq: seq, Ts: tsPtr, Level: level.String, Component: component.String,
				Message: message.String, Raw: raw, Attrs: attrs,
			})
		}
		return rows.Err()
	}
	summary, err := pp.Process(faID, entries)
	if err != nil {
		return summary, err
	}
	// Заполняем базовые поля, недоступные из записей: file_size, uploaded_at, encoding.
	var (
		fileSize   int64
		uploadedAt string
		encoding   string
	)
	_ = s.db.QueryRowContext(ctx,
		`SELECT u.size_bytes, u.uploaded_at, COALESCE(a.encoding,'')
		FROM t_files_analyze a JOIN t_files_upload u ON a.upload_id=u.id
		WHERE a.id=?`, faID).Scan(&fileSize, &uploadedAt, &encoding)
	summary.FileSize = fileSize
	summary.UploadedAt = uploadedAt
	summary.Encoding = encoding
	return summary, nil
}

// setUploadError обновляет статус/ошибку загрузки.
func (s *Service) setUploadError(ctx context.Context, uploadID, status, errMsg string) {
	_, _ = s.db.ExecContext(ctx, "UPDATE t_files_upload SET status=?, error=? WHERE id=?", status, errMsg, uploadID)
}

// setAnalyzeError обновляет статус/ошибку файла анализа.
func (s *Service) setAnalyzeError(ctx context.Context, faID, status, _, errMsg string) {
	_, _ = s.db.ExecContext(ctx, "UPDATE t_files_analyze SET status=?, error=? WHERE id=?", status, errMsg, faID)
}

// ---- helpers ----------------------------------------------------------------

type recordRow struct {
	seq        int
	format     string
	ts         *time.Time
	tsRaw      string
	tzOffset   string
	tzInferred bool
	level      string
	component  string
	message    string
	raw        string
	attrsJSON  string
}

type countingReader struct{ n int64 }

func (c *countingReader) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// limitedBuffer накапливает данные до max байт; при превышении возвращает ошибку.
type limitedBuffer struct {
	buf []byte
	max int64
}

var errLimitExceeded = errors.New("превышен лимит размера")

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if int64(len(b.buf))+int64(len(p)) > b.max {
		return 0, errLimitExceeded
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}
func (b *limitedBuffer) Bytes() []byte { return b.buf }
func (b *limitedBuffer) Len() int64    { return int64(len(b.buf)) }

// emitLines построчно отправляет текст в канал lines.
func emitLines(text string, lines chan<- string) error {
	defer close(lines)
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			line := text[start:i]
			lines <- line
			start = i + 1
		}
	}
	if start < len(text) {
		lines <- text[start:]
	}
	return nil
}

// readSampleLines возвращает до n первых непустых строк текста.
func readSampleLines(text string, n int) []string {
	out := make([]string, 0, n)
	start := 0
	for i := 0; i < len(text) && len(out) < n; i++ {
		if text[i] == '\n' {
			line := strings.TrimRight(text[start:i], "\r")
			if strings.TrimSpace(line) != "" {
				out = append(out, line)
			}
			start = i + 1
		}
	}
	if start < len(text) && len(out) < n {
		line := strings.TrimRight(text[start:], "\r")
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// isZipFilename проверяет расширение.
func isZipFilename(name string) bool {
	return strings.EqualFold(path.Ext(name), ".zip")
}

// isZipContent проверяет magic-байты zip (PK\x03\x04).
func isZipContent(b []byte) bool {
	return len(b) >= 4 && b[0] == 'P' && b[1] == 'K' && b[2] == 0x03 && b[3] == 0x04
}

// isLikelyTextFilename проверяет, что файл похож на текстовый лог по расширению.
func isLikelyTextFilename(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".log", ".out", ".txt", ".err", "":
		return true
	}
	return false
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
