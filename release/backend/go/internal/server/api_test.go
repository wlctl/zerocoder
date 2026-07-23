package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irav/dev-agent/internal/config"
	"github.com/irav/dev-agent/internal/db"
	"github.com/irav/dev-agent/internal/parser"
	"github.com/irav/dev-agent/internal/postprocess"
)

func newAPIServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	d, err := db.Open("sqlite:" + filepath.Join(t.TempDir(), "la.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Run(context.Background(), d.DB); err != nil {
		t.Fatalf("db.Run: %v", err)
	}
	cfg := &config.Config{DefaultTZ: "UTC", MaxFileSize: 50 << 20, MaxFileCount: 10}
	pm := parser.NewManager("", nil)
	pp := postprocess.NewManager("", nil)
	srv := New("127.0.0.1:0", d.DB, cfg, pm, pp)
	return srv, d
}

func doReq(t *testing.T, srv *Server, method, path string, body io.Reader, contentType string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rr := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rr, req)
	return rr.Code, rr.Body.Bytes()
}

func sampleRootAbs(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "sample-logs"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("sample-logs недоступен: %v", err)
	}
	return abs
}

// uploadFile выполняет multipart POST /api/uploads с одним файлом и возвращает
// код + первый элемент results[] (один файл → один результат).
func uploadFile(t *testing.T, srv *Server, filename, content string) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	fw.Write([]byte(content))
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/uploads", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rr, req)
	var top map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &top)
	results, _ := top["results"].([]any)
	if len(results) == 0 {
		return rr.Code, top
	}
	return rr.Code, results[0].(map[string]any)
}

func TestPostUploadAndEntries(t *testing.T) {
	srv, _ := newAPIServer(t)
	content := "####<May 16, 2018 6:26:25 PM MSK> <Notice> <Security> <host> <AdminServer> <main> <<WLS Kernel>> <> <> <1526480785513> <BEA-090082> <Security initializing>\n" +
		"####<May 16, 2018 6:26:31 PM MSK> <Notice> <WebLogicServer> <host> <AdminServer> <main> <<WLS Kernel>> <> <> <1526480791332> <BEA-000365> <Server state changed to STARTING>\n"
	code, body := uploadFile(t, srv, "test.log", content)
	if code != http.StatusCreated {
		t.Fatalf("upload status = %d, body=%s", code, body)
	}
	uploadID, _ := body["upload_id"].(string)
	if uploadID == "" {
		t.Fatal("нет upload_id")
	}
	files, _ := body["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	fa := files[0].(map[string]any)
	faID, _ := fa["file_analyze_id"].(string)
	if faID == "" {
		t.Fatal("нет file_analyze_id")
	}

	// GET /api/uploads.
	code, b := doReq(t, srv, http.MethodGet, "/api/uploads", nil, "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/uploads = %d", code)
	}
	var list []map[string]any
	json.Unmarshal(b, &list)
	if len(list) != 1 {
		t.Errorf("uploads list len = %d, want 1", len(list))
	}

	// GET /api/uploads/{id}.
	code, b = doReq(t, srv, http.MethodGet, "/api/uploads/"+uploadID, nil, "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/uploads/{id} = %d", code)
	}
	var detail map[string]any
	json.Unmarshal(b, &detail)
	if detail["file_count"].(float64) != 1 {
		t.Errorf("file_count = %v, want 1", detail["file_count"])
	}

	// GET /api/files/{id}/entries.
	code, b = doReq(t, srv, http.MethodGet, "/api/files/"+faID+"/entries?limit=10", nil, "")
	if code != http.StatusOK {
		t.Fatalf("entries = %d", code)
	}
	var entries map[string]any
	json.Unmarshal(b, &entries)
	items, _ := entries["items"].([]any)
	if len(items) != 2 {
		t.Errorf("entries items = %d, want 2", len(items))
	}
	if entries["total"].(float64) != 2 {
		t.Errorf("total = %v, want 2", entries["total"])
	}
	// raw_line присутствует.
	first := items[0].(map[string]any)
	if _, ok := first["raw_line"]; !ok {
		t.Error("нет raw_line в записи")
	}
}

func TestUploadDuplicate(t *testing.T) {
	srv, _ := newAPIServer(t)
	content := "line1\nline2\n"
	code, first := uploadFile(t, srv, "dup.log", content)
	if code != http.StatusCreated {
		t.Fatalf("first upload = %d", code)
	}
	firstID, _ := first["upload_id"].(string)
	code, body := uploadFile(t, srv, "dup.log", content)
	if code != http.StatusCreated {
		t.Fatalf("duplicate upload = %d, want 201 (per-file result)", code)
	}
	if body["status"] != "duplicate" {
		t.Errorf("status = %v, want duplicate", body["status"])
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "Файл уже был загружен") {
		t.Errorf("error = %q", msg)
	}
	if body["duplicate"] != true {
		t.Errorf("duplicate flag = %v, want true", body["duplicate"])
	}
	if body["existing_upload_id"] != firstID {
		t.Errorf("existing_upload_id = %v, want %s", body["existing_upload_id"], firstID)
	}
}

func TestUploadMultipleFiles(t *testing.T) {
	srv, _ := newAPIServer(t)
	// Два файла в одном multipart-запросе.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw1, _ := mw.CreateFormFile("file", "a.log")
	fw1.Write([]byte("####<May 16, 2018 6:26:25 PM MSK> <Notice> <Security> <h> <s> <m> <<K>> <> <> <1526480785513> <BEA-090082> <Security initializing>\n"))
	fw2, _ := mw.CreateFormFile("file", "b.log")
	fw2.Write([]byte("2018-05-16 18:26:25,123 INFO  com.x.Main - started\n"))
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/uploads", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("multi upload = %d, body=%s", rr.Code, rr.Body.String())
	}
	var top map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &top)
	results, _ := top["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	for _, r := range results {
		rm := r.(map[string]any)
		if rm["status"] != "parsed" {
			t.Errorf("result %v status = %v, want parsed", rm["filename"], rm["status"])
		}
		if rm["upload_id"] == "" {
			t.Errorf("result %v missing upload_id", rm["filename"])
		}
	}
}

func TestCorrelate(t *testing.T) {
	srv, _ := newAPIServer(t)
	// Один zip с двумя логами → одна загрузка (общий upload_id), два t_files_analyze.
	// a.log — weblogic (ts из epoch-millis: 14:26:25Z, 14:26:31Z);
	// b.log — java/log4j (defaultTZ=UTC: 14:26:28Z). Записи чередуются кросс-файл:
	// a@14:26:25, b@14:26:28, a@14:26:31.
	zipPath := filepath.Join(t.TempDir(), "corr.zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(zipFile)
	w, err := zw.Create("a.log")
	if err != nil {
		t.Fatalf("zip a.log: %v", err)
	}
	w.Write([]byte("####<May 16, 2018 6:26:25 PM MSK> <Notice> <Security> <h> <s> <m> <<K>> <> <> <1526480785513> <BEA-090082> <first>\n" +
		"####<May 16, 2018 6:26:31 PM MSK> <Info> <WebLogicServer> <h> <s> <m> <<K>> <> <> <1526480791332> <BEA-1> <third>\n"))
	w, err = zw.Create("b.log")
	if err != nil {
		t.Fatalf("zip b.log: %v", err)
	}
	w.Write([]byte("2018-05-16 14:26:28,123 INFO  com.x.Main - mid\n"))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	zipFile.Close()

	zipBytes, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "corr.zip")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	fw.Write(zipBytes)
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/uploads", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("zip upload = %d, body=%s", rr.Code, rr.Body.String())
	}
	var top map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &top)
	results, _ := top["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1 (zip)", len(results))
	}
	rm := results[0].(map[string]any)
	if rm["kind"] != "zip" {
		t.Errorf("kind = %v, want zip", rm["kind"])
	}
	if rm["status"] != "parsed" {
		t.Fatalf("zip status = %v, want parsed", rm["status"])
	}
	uploadID, _ := rm["upload_id"].(string)
	if uploadID == "" {
		t.Fatal("нет upload_id")
	}
	files, _ := rm["files"].([]any)
	if len(files) != 2 {
		t.Fatalf("zip files len = %d, want 2", len(files))
	}
	fileIDs := map[string]string{} // filename -> file_analyze_id
	for _, f := range files {
		fa := f.(map[string]any)
		fileIDs[fa["filename"].(string)], _ = fa["file_analyze_id"].(string)
	}
	if fileIDs["a.log"] == "" || fileIDs["b.log"] == "" {
		t.Fatalf("не получили file_analyze_id: %v", fileIDs)
	}

	// GET /correlate без фильтров → все 3 записи, упорядоченные по ts кросс-файл.
	code, b := doReq(t, srv, http.MethodGet,
		"/api/uploads/"+uploadID+"/correlate?files="+fileIDs["a.log"]+","+fileIDs["b.log"], nil, "")
	if code != http.StatusOK {
		t.Fatalf("correlate = %d, body=%s", code, string(b))
	}
	var page map[string]any
	json.Unmarshal(b, &page)
	if page["total"].(float64) != 3 {
		t.Errorf("total = %v, want 3", page["total"])
	}
	items, _ := page["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items len = %d, want 3", len(items))
	}
	wantOrder := []string{"a.log", "b.log", "a.log"}
	for i, want := range wantOrder {
		m := items[i].(map[string]any)
		if m["filename"] != want {
			t.Errorf("items[%d].filename = %v, want %s (порядок по ts кросс-файл нарушен)", i, m["filename"], want)
		}
		if m["file_analyze_id"] == nil {
			t.Errorf("items[%d] нет file_analyze_id", i)
		}
		if m["ts"] == nil {
			t.Errorf("items[%d] ts = nil, ожидалась метка", i)
		}
	}

	// Фильтр by from/to-окно: только b@14:26:28 → total=1.
	code, b = doReq(t, srv, http.MethodGet,
		"/api/uploads/"+uploadID+"/correlate?files="+fileIDs["a.log"]+","+fileIDs["b.log"]+
			"&from=2018-05-16T14:26:26Z&to=2018-05-16T14:26:30Z", nil, "")
	if code != http.StatusOK {
		t.Fatalf("correlate(filtered) = %d, body=%s", code, string(b))
	}
	json.Unmarshal(b, &page)
	if page["total"].(float64) != 1 {
		t.Errorf("filtered total = %v, want 1", page["total"])
	}
	items, _ = page["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["filename"] != "b.log" {
		t.Errorf("filtered items = %v, want [b.log]", items)
	}

	// Фильтр по files subset (только a.log) → 2 записи.
	code, b = doReq(t, srv, http.MethodGet,
		"/api/uploads/"+uploadID+"/correlate?files="+fileIDs["a.log"], nil, "")
	if code != http.StatusOK {
		t.Fatalf("correlate(subset) = %d, body=%s", code, string(b))
	}
	json.Unmarshal(b, &page)
	if page["total"].(float64) != 2 {
		t.Errorf("subset total = %v, want 2", page["total"])
	}

	// Пустая выборка (несуществующий upload) → {items:[], total:0}.
	code, b = doReq(t, srv, http.MethodGet, "/api/uploads/nope/correlate", nil, "")
	if code != http.StatusOK {
		t.Fatalf("correlate(empty) = %d", code)
	}
	json.Unmarshal(b, &page)
	if page["total"].(float64) != 0 {
		t.Errorf("empty total = %v, want 0", page["total"])
	}
	items, _ = page["items"].([]any)
	if len(items) != 0 {
		t.Errorf("empty items len = %d, want 0", len(items))
	}
}

func TestStatsEmpty(t *testing.T) {
	srv, _ := newAPIServer(t)
	code, b := doReq(t, srv, http.MethodGet, "/api/stats", nil, "")
	if code != http.StatusOK {
		t.Fatalf("stats = %d", code)
	}
	var stats map[string]int64
	json.Unmarshal(b, &stats)
	if stats["upload_count"] != 0 {
		t.Errorf("upload_count = %d, want 0", stats["upload_count"])
	}
}

func TestHistogram(t *testing.T) {
	srv, _ := newAPIServer(t)
	content := "####<May 16, 2018 6:26:25 PM MSK> <Notice> <Security> <host> <AdminServer> <main> <<WLS Kernel>> <> <> <1526480785513> <BEA-090082> <msg1>\n" +
		"####<May 16, 2018 6:27:25 PM MSK> <Info> <WebLogicServer> <host> <AdminServer> <main> <<WLS Kernel>> <> <> <1526480845513> <BEA-1> <msg2>\n"
	code, body := uploadFile(t, srv, "h.log", content)
	if code != http.StatusCreated {
		t.Fatalf("upload = %d", code)
	}
	uploadID := body["upload_id"].(string)
	// histogram by minute.
	code, b := doReq(t, srv, http.MethodGet, "/api/uploads/"+uploadID+"/histogram?bucket=minute", nil, "")
	if code != http.StatusOK {
		t.Fatalf("histogram = %d", code)
	}
	var buckets []map[string]any
	json.Unmarshal(b, &buckets)
	if len(buckets) == 0 {
		t.Error("histogram пуст")
	}
}

func TestTimeline(t *testing.T) {
	srv, _ := newAPIServer(t)
	content := "####<May 16, 2018 6:26:25 PM MSK> <Notice> <Security> <host> <AdminServer> <main> <<WLS Kernel>> <> <> <1526480785513> <BEA-090082> <msg1>\n"
	code, body := uploadFile(t, srv, "t.log", content)
	if code != http.StatusCreated {
		t.Fatalf("upload = %d", code)
	}
	uploadID := body["upload_id"].(string)
	code, b := doReq(t, srv, http.MethodGet, "/api/uploads/"+uploadID+"/timeline", nil, "")
	if code != http.StatusOK {
		t.Fatalf("timeline = %d", code)
	}
	var tl map[string]any
	json.Unmarshal(b, &tl)
	if tl["min_ts"] == nil {
		t.Error("min_ts nil, expected value")
	}
}

func TestSearch(t *testing.T) {
	srv, _ := newAPIServer(t)
	content := "####<May 16, 2018 6:26:25 PM MSK> <Notice> <Security> <host> <AdminServer> <main> <<WLS Kernel>> <> <> <1526480785513> <BEA-090082> <Security initializing>\n"
	code, body := uploadFile(t, srv, "s.log", content)
	if code != http.StatusCreated {
		t.Fatalf("upload = %d", code)
	}
	uploadID := body["upload_id"].(string)
	code, b := doReq(t, srv, http.MethodGet, "/api/uploads/"+uploadID+"/search?q=Security", nil, "")
	if code != http.StatusOK {
		t.Fatalf("search = %d", code)
	}
	var items []map[string]any
	json.Unmarshal(b, &items)
	if len(items) == 0 {
		t.Error("search пуст")
	}
	if _, ok := items[0]["file_analyze_id"]; !ok {
		t.Error("нет file_analyze_id в результате search")
	}
}

func TestLexemes(t *testing.T) {
	srv, _ := newAPIServer(t)
	content := "####<May 16, 2018 6:26:25 PM MSK> <Notice> <Security> <host> <AdminServer> <main> <<WLS Kernel>> <> <> <1526480785513> <BEA-090082> <Security initializing domain>\n"
	code, body := uploadFile(t, srv, "l.log", content)
	if code != http.StatusCreated {
		t.Fatalf("upload = %d", code)
	}
	uploadID := body["upload_id"].(string)
	code, b := doReq(t, srv, http.MethodGet, "/api/uploads/"+uploadID+"/lexemes?limit=5", nil, "")
	if code != http.StatusOK {
		t.Fatalf("lexemes = %d", code)
	}
	var items []map[string]any
	json.Unmarshal(b, &items)
	if len(items) == 0 {
		t.Error("lexemes пуст")
	}
}

func TestFiltersCRUD(t *testing.T) {
	srv, _ := newAPIServer(t)
	// Вставим upload вручную в БД srv (для FK).
	_, err := srv.db.ExecContext(context.Background(),
		`INSERT INTO t_files_upload (id, filename, md5, size_bytes, kind, status, uploaded_at)
		VALUES ('up-filter', 'f.log', 'md5filter', 10, 'file', 'parsed', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert upload: %v", err)
	}
	// POST filter.
	body := strings.NewReader(`{"kind":"search","rule":{"q":"err"}}`)
	code, b := doReq(t, srv, http.MethodPost, "/api/uploads/up-filter/filters", body, "application/json")
	if code != http.StatusCreated {
		t.Fatalf("POST filter = %d, body=%s", code, b)
	}
	var f map[string]any
	json.Unmarshal(b, &f)
	fid, _ := f["id"].(string)
	if fid == "" {
		t.Fatal("нет id фильтра")
	}
	// GET filters.
	code, b = doReq(t, srv, http.MethodGet, "/api/uploads/up-filter/filters", nil, "")
	if code != http.StatusOK {
		t.Fatalf("GET filters = %d", code)
	}
	var fl []map[string]any
	json.Unmarshal(b, &fl)
	if len(fl) != 1 {
		t.Errorf("filters len = %d, want 1", len(fl))
	}
	// DELETE filter.
	code, _ = doReq(t, srv, http.MethodDelete, "/api/uploads/up-filter/filters/"+fid, nil, "")
	if code != http.StatusNoContent {
		t.Errorf("DELETE filter = %d, want 204", code)
	}
}

func TestHighlightsCRUD(t *testing.T) {
	srv, _ := newAPIServer(t)
	_, err := srv.db.ExecContext(context.Background(),
		`INSERT INTO t_files_upload (id, filename, md5, size_bytes, kind, status, uploaded_at)
		VALUES ('up-hl', 'f.log', 'md5hl', 10, 'file', 'parsed', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert upload: %v", err)
	}
	body := strings.NewReader(`{"text":"error","color":"#ff0000","lexeme":0}`)
	code, b := doReq(t, srv, http.MethodPost, "/api/uploads/up-hl/highlights", body, "application/json")
	if code != http.StatusCreated {
		t.Fatalf("POST highlight = %d, body=%s", code, b)
	}
	var h map[string]any
	json.Unmarshal(b, &h)
	hid, _ := h["id"].(string)
	if hid == "" {
		t.Fatal("нет id highlight")
	}
	// GET.
	code, _ = doReq(t, srv, http.MethodGet, "/api/uploads/up-hl/highlights", nil, "")
	if code != http.StatusOK {
		t.Fatalf("GET highlights = %d", code)
	}
	// DELETE.
	code, _ = doReq(t, srv, http.MethodDelete, "/api/uploads/up-hl/highlights/"+hid, nil, "")
	if code != http.StatusNoContent {
		t.Errorf("DELETE highlight = %d, want 204", code)
	}
}

func TestDeleteViewState(t *testing.T) {
	srv, _ := newAPIServer(t)
	_, err := srv.db.ExecContext(context.Background(),
		`INSERT INTO t_files_upload (id, filename, md5, size_bytes, kind, status, uploaded_at)
		VALUES ('up-vs', 'f.log', 'md5vs', 10, 'file', 'parsed', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert upload: %v", err)
	}
	srv.db.ExecContext(context.Background(),
		`INSERT INTO t_view_filters (id, upload_id, kind, rule, created_at) VALUES ('vf', 'up-vs', 'search', '{}', '2026-01-01T00:00:00Z')`)
	srv.db.ExecContext(context.Background(),
		`INSERT INTO t_view_highlights (id, upload_id, text, color, lexeme, created_at) VALUES ('vh', 'up-vs', 'x', '#fff', 0, '2026-01-01T00:00:00Z')`)
	code, _ := doReq(t, srv, http.MethodDelete, "/api/uploads/up-vs/view-state", nil, "")
	if code != http.StatusNoContent {
		t.Errorf("DELETE view-state = %d, want 204", code)
	}
	var n int
	srv.db.QueryRowContext(context.Background(), "SELECT count(*) FROM t_view_filters WHERE upload_id='up-vs'").Scan(&n)
	if n != 0 {
		t.Errorf("filters после view-state = %d, want 0", n)
	}
}

func TestDeleteUploadCascade(t *testing.T) {
	srv, _ := newAPIServer(t)
	content := "####<May 16, 2018 6:26:25 PM MSK> <Notice> <Security> <host> <AdminServer> <main> <<WLS Kernel>> <> <> <1526480785513> <BEA-090082> <msg>\n"
	code, body := uploadFile(t, srv, "c.log", content)
	if code != http.StatusCreated {
		t.Fatalf("upload = %d", code)
	}
	uploadID := body["upload_id"].(string)
	// Добавим view-state.
	srv.db.ExecContext(context.Background(),
		`INSERT INTO t_view_filters (id, upload_id, kind, rule, created_at) VALUES ('vf2', ?, 'search', '{}', '2026-01-01T00:00:00Z')`, uploadID)
	// До удаления: есть записи и файлы (каскад-тест осмыслен) + агрегаты > 0.
	var recsBefore int64
	srv.db.QueryRowContext(context.Background(), "SELECT count(*) FROM t_log_entries").Scan(&recsBefore)
	if recsBefore == 0 {
		t.Fatal("t_log_entries пуст до delete — тест некорректен")
	}
	if c, b := doReq(t, srv, http.MethodGet, "/api/stats", nil, ""); c != http.StatusOK {
		t.Fatalf("GET /api/stats = %d, body=%s", c, b)
	} else {
		var st map[string]any
		json.Unmarshal(b, &st)
		if rc, _ := st["record_count"].(float64); rc != float64(recsBefore) {
			t.Errorf("stats.record_count до delete = %v, want %d", st["record_count"], recsBefore)
		}
	}
	// DELETE upload.
	code, _ = doReq(t, srv, http.MethodDelete, "/api/uploads/"+uploadID, nil, "")
	if code != http.StatusNoContent {
		t.Errorf("DELETE upload = %d, want 204", code)
	}
	var n int
	srv.db.QueryRowContext(context.Background(), "SELECT count(*) FROM t_view_filters WHERE upload_id=?", uploadID).Scan(&n)
	if n != 0 {
		t.Errorf("view_filters после cascade = %d, want 0", n)
	}
	// Регрессия футера «Агрегаты» (баг 0.5.0): после удаления загрузки агрегаты
	// не пересчитывались — PRAGMA foreign_keys=ON применялась только к одному
	// соединению пула database/sql, CASCADE не срабатывал на остальных →
	// t_files_analyze/t_log_entries оставались, COUNT(*) в getStats не падал.
	// Фикс: DSN-pragma foreign_keys(1) на каждое соединение (db.openSQLite).
	var recsAfter, filesAfter int64
	srv.db.QueryRowContext(context.Background(), "SELECT count(*) FROM t_log_entries").Scan(&recsAfter)
	srv.db.QueryRowContext(context.Background(), "SELECT count(*) FROM t_files_analyze").Scan(&filesAfter)
	if recsAfter != 0 {
		t.Errorf("t_log_entries после cascade = %d, want 0 (foreign_keys не на всех соединениях пула?)", recsAfter)
	}
	if filesAfter != 0 {
		t.Errorf("t_files_analyze после cascade = %d, want 0", filesAfter)
	}
	// Эндпоинт /api/stats тоже должен показать 0 записей/файлов.
	if c, b := doReq(t, srv, http.MethodGet, "/api/stats", nil, ""); c != http.StatusOK {
		t.Fatalf("GET /api/stats после delete = %d, body=%s", c, b)
	} else {
		var st map[string]any
		json.Unmarshal(b, &st)
		if rc, _ := st["record_count"].(float64); rc != 0 {
			t.Errorf("stats.record_count после delete = %v, want 0 (футер не пересчитан)", st["record_count"])
		}
		if fc, _ := st["file_count"].(float64); fc != 0 {
			t.Errorf("stats.file_count после delete = %v, want 0", st["file_count"])
		}
	}
}

// TestGetFiles — регрессия бага 0.5.0: GET /api/files?upload_id= возвращал 500
// из-за несоответствия числа колонок SELECT (16) и приёмников rows.Scan (13) в
// scanFileRows. Фронтенд listFiles получал ошибку → селектор файлов пуст.
func TestGetFiles(t *testing.T) {
	srv, _ := newAPIServer(t)
	content := "####<May 16, 2018 6:26:25 PM MSK> <Notice> <Security> <host> <AdminServer> <main> <<WLS Kernel>> <> <> <1526480785513> <BEA-090082> <Security initializing>\n"
	code, body := uploadFile(t, srv, "test.log", content)
	if code != http.StatusCreated {
		t.Fatalf("upload = %d, body=%s", code, body)
	}
	uploadID, _ := body["upload_id"].(string)
	if uploadID == "" {
		t.Fatal("нет upload_id")
	}
	code, b := doReq(t, srv, http.MethodGet, "/api/files?upload_id="+uploadID, nil, "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/files?upload_id= = %d, body=%s (регрессия 500)", code, b)
	}
	var list []map[string]any
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatalf("unmarshal files: %v body=%s", err, b)
	}
	if len(list) != 1 {
		t.Fatalf("files len = %d, want 1", len(list))
	}
	f := list[0]
	if f["upload_id"] != uploadID {
		t.Errorf("file upload_id = %v, want %s", f["upload_id"], uploadID)
	}
	if f["format"] != "weblogic" {
		t.Errorf("file format = %v, want weblogic", f["format"])
	}
	if rc, _ := f["record_count"].(float64); rc != 1 {
		t.Errorf("record_count = %v, want 1", f["record_count"])
	}
	if f["summary"] == nil {
		t.Error("summary отсутствует (ожидался postprocess-summary)")
	}
	// Без upload_id — тоже не должно падать (список всех файлов).
	if c, b2 := doReq(t, srv, http.MethodGet, "/api/files", nil, ""); c != http.StatusOK {
		t.Errorf("GET /api/files (all) = %d, want 200, body=%s", c, b2)
	}
}

func TestGetParsers(t *testing.T) {
	srv, _ := newAPIServer(t)
	code, b := doReq(t, srv, http.MethodGet, "/api/parsers", nil, "")
	if code != http.StatusOK {
		t.Fatalf("parsers = %d", code)
	}
	var body map[string]any
	json.Unmarshal(b, &body)
	parsers, _ := body["parsers"].([]any)
	if len(parsers) == 0 {
		t.Error("parsers пуст")
	}
}

func TestDeleteUploadNotFound(t *testing.T) {
	srv, _ := newAPIServer(t)
	code, _ := doReq(t, srv, http.MethodDelete, "/api/uploads/nonexistent", nil, "")
	if code != http.StatusNotFound {
		t.Errorf("DELETE missing upload = %d, want 404", code)
	}
}

// uploadZip2 загружает zip с a.log (weblogic, 2 записи: 14:26:25Z, 14:26:31Z) и
// b.log (java/log4j, 1 запись: 14:26:28Z) в одной загрузке; возвращает upload_id
// и map filename -> file_analyze_id. Общий helper для тестов 0.6.0.
func uploadZip2(t *testing.T, srv *Server) (string, map[string]string) {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "two.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(zf)
	w, _ := zw.Create("a.log")
	w.Write([]byte("####<May 16, 2018 6:26:25 PM MSK> <Notice> <Security> <h> <s> <m> <<K>> <> <> <1526480785513> <BEA-090082> <first>\n" +
		"####<May 16, 2018 6:26:31 PM MSK> <Info> <WebLogicServer> <h> <s> <m> <<K>> <> <> <1526480791332> <BEA-1> <third>\n"))
	w, _ = zw.Create("b.log")
	w.Write([]byte("2018-05-16 14:26:28,123 INFO  com.x.Main - mid\n"))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	zf.Close()
	zb, _ := os.ReadFile(zipPath)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "two.zip")
	fw.Write(zb)
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/uploads", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("zip upload = %d, body=%s", rr.Code, rr.Body.String())
	}
	var top map[string]any
	json.Unmarshal(rr.Body.Bytes(), &top)
	results, _ := top["results"].([]any)
	rm := results[0].(map[string]any)
	uploadID, _ := rm["upload_id"].(string)
	files, _ := rm["files"].([]any)
	ids := map[string]string{}
	for _, f := range files {
		fa := f.(map[string]any)
		ids[fa["filename"].(string)], _ = fa["file_analyze_id"].(string)
	}
	return uploadID, ids
}

// TestSearchRegex — mode=regex: серверный REGEXP; плохой паттерн → 400.
func TestSearchRegex(t *testing.T) {
	srv, _ := newAPIServer(t)
	content := "####<May 16, 2018 6:26:25 PM MSK> <Notice> <Security> <h> <s> <m> <<K>> <> <> <1526480785513> <BEA-090082> <Security initializing>\n" +
		"####<May 16, 2018 6:26:31 PM MSK> <Info> <WebLogicServer> <h> <s> <m> <<K>> <> <> <1526480791332> <BEA-1> <Server state changed>\n"
	code, body := uploadFile(t, srv, "test.log", content)
	if code != http.StatusCreated {
		t.Fatalf("upload = %d, body=%s", code, body)
	}
	uploadID, _ := body["upload_id"].(string)
	// regex-поиск по «Security» → попадает первая строка.
	code, b := doReq(t, srv, http.MethodGet, "/api/uploads/"+uploadID+"/search?q=Security&mode=regex", nil, "")
	if code != http.StatusOK {
		t.Fatalf("search regex = %d, body=%s", code, b)
	}
	var items []map[string]any
	json.Unmarshal(b, &items)
	if len(items) != 1 {
		t.Errorf("regex hits = %d, want 1", len(items))
	}
	// regex по raw_line.
	code, b = doReq(t, srv, http.MethodGet, "/api/uploads/"+uploadID+"/search?q=%5E.*Security.*%24&mode=regex&fields=raw", nil, "")
	if code != http.StatusOK {
		t.Fatalf("search regex raw = %d, body=%s", code, b)
	}
	json.Unmarshal(b, &items)
	if len(items) != 1 {
		t.Errorf("regex raw hits = %d, want 1", len(items))
	}
	// плохой паттерн → 400 (предкомпиляция в Go).
	code, b = doReq(t, srv, http.MethodGet, "/api/uploads/"+uploadID+"/search?q=%5Bbad(&mode=regex", nil, "")
	if code != http.StatusBadRequest {
		t.Errorf("search bad regex = %d, want 400, body=%s", code, b)
	}
}

// TestSearchAttrs — фильтр по json_extract(attrs): попадание/AND/отсутствие ключа.
func TestSearchAttrs(t *testing.T) {
	srv, _ := newAPIServer(t)
	ctx := context.Background()
	if _, err := srv.db.ExecContext(ctx,
		`INSERT INTO t_files_upload (id, filename, md5, size_bytes, kind, status, uploaded_at)
		VALUES ('up-attrs', 'a.log', 'md5attrs', 10, 'file', 'parsed', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert upload: %v", err)
	}
	if _, err := srv.db.ExecContext(ctx,
		`INSERT INTO t_files_analyze (id, upload_id, filename, format, status, record_count)
		VALUES ('fa-attrs', 'up-attrs', 'a.log', 'text', 'parsed', 3)`); err != nil {
		t.Fatalf("insert file: %v", err)
	}
	rows := []struct {
		seq   int
		raw   string
		attrs string
	}{
		{1, "line1", `{"user":"alice","status":200}`},
		{2, "line2", `{"user":"bob","status":404}`},
		{3, "line3", `{"user":"alice","status":404}`},
	}
	for _, r := range rows {
		if _, err := srv.db.ExecContext(ctx,
			`INSERT INTO t_log_entries (file_analyze_id, seq, format, raw_line, attrs) VALUES ('fa-attrs', ?, 'text', ?, ?)`,
			r.seq, r.raw, r.attrs); err != nil {
			t.Fatalf("insert entry %d: %v", r.seq, err)
		}
	}
	// attrs=user:alice → 2 (seq1, seq3).
	code, b := doReq(t, srv, http.MethodGet, "/api/uploads/up-attrs/search?attrs=user:alice", nil, "")
	if code != http.StatusOK {
		t.Fatalf("search attrs = %d, body=%s", code, b)
	}
	var items []map[string]any
	json.Unmarshal(b, &items)
	if len(items) != 2 {
		t.Errorf("attrs user:alice hits = %d, want 2", len(items))
	}
	// attrs=user:alice,status:200 → 1 (AND, seq1).
	code, b = doReq(t, srv, http.MethodGet, "/api/uploads/up-attrs/search?attrs=user:alice,status:200", nil, "")
	if code != http.StatusOK {
		t.Fatalf("search attrs AND = %d, body=%s", code, b)
	}
	json.Unmarshal(b, &items)
	if len(items) != 1 {
		t.Errorf("attrs user:alice,status:200 hits = %d, want 1", len(items))
	}
	// отсутствующий ключ → json_extract NULL → 0.
	code, b = doReq(t, srv, http.MethodGet, "/api/uploads/up-attrs/search?attrs=nonexistent:foo", nil, "")
	if code != http.StatusOK {
		t.Fatalf("search attrs missing = %d, body=%s", code, b)
	}
	json.Unmarshal(b, &items)
	if len(items) != 0 {
		t.Errorf("attrs nonexistent:foo hits = %d, want 0", len(items))
	}
}

// TestCorrelateRegex — regex/attrs в общем where: total COUNT-safe.
func TestCorrelateRegex(t *testing.T) {
	srv, _ := newAPIServer(t)
	uploadID, ids := uploadZip2(t, srv)
	files := ids["a.log"] + "," + ids["b.log"]
	// regex по «mid» (только b.log) → total=1, COUNT-safe.
	code, b := doReq(t, srv, http.MethodGet,
		"/api/uploads/"+uploadID+"/correlate?files="+files+"&q=mid&mode=regex", nil, "")
	if code != http.StatusOK {
		t.Fatalf("correlate regex = %d, body=%s", code, b)
	}
	var page map[string]any
	json.Unmarshal(b, &page)
	if page["total"].(float64) != 1 {
		t.Errorf("correlate regex total = %v, want 1 (COUNT-safe)", page["total"])
	}
	// плохой regex → 400.
	code, b = doReq(t, srv, http.MethodGet,
		"/api/uploads/"+uploadID+"/correlate?files="+files+"&q=%5Bbad(&mode=regex", nil, "")
	if code != http.StatusBadRequest {
		t.Errorf("correlate bad regex = %d, want 400, body=%s", code, b)
	}
}

// TestHistogramByFile — стекированный график: per-file сегменты по бакету.
func TestHistogramByFile(t *testing.T) {
	srv, _ := newAPIServer(t)
	uploadID, ids := uploadZip2(t, srv)
	files := ids["a.log"] + "," + ids["b.log"]
	// bucket=hour: все 3 записи в одном часе → 2 сегмента (a.log:2, b.log:1).
	code, b := doReq(t, srv, http.MethodGet,
		"/api/uploads/"+uploadID+"/histogram-by-file?bucket=hour&files="+files, nil, "")
	if code != http.StatusOK {
		t.Fatalf("histogram-by-file = %d, body=%s", code, b)
	}
	var segs []map[string]any
	json.Unmarshal(b, &segs)
	if len(segs) != 2 {
		t.Fatalf("segments = %d, want 2 (per-file)", len(segs))
	}
	countByID := map[string]int{}
	for _, s := range segs {
		fa, _ := s["file_analyze_id"].(string)
		c, _ := s["count"].(float64)
		countByID[fa] = int(c)
	}
	if countByID[ids["a.log"]] != 2 {
		t.Errorf("a.log count = %d, want 2", countByID[ids["a.log"]])
	}
	if countByID[ids["b.log"]] != 1 {
		t.Errorf("b.log count = %d, want 1", countByID[ids["b.log"]])
	}
	// окно from/to: только b@14:26:28 → 1 сегмент (b.log:1).
	code, b = doReq(t, srv, http.MethodGet,
		"/api/uploads/"+uploadID+"/histogram-by-file?bucket=hour&files="+files+
			"&from=2018-05-16T14:26:26Z&to=2018-05-16T14:26:30Z", nil, "")
	if code != http.StatusOK {
		t.Fatalf("histogram-by-file window = %d, body=%s", code, b)
	}
	json.Unmarshal(b, &segs)
	if len(segs) != 1 {
		t.Errorf("window segments = %d, want 1 (b.log)", len(segs))
	}
}

// TestPresetsCRUD — пресеты: POST/GET/DELETE + 404 на повторный delete.
func TestPresetsCRUD(t *testing.T) {
	srv, _ := newAPIServer(t)
	ctx := context.Background()
	if _, err := srv.db.ExecContext(ctx,
		`INSERT INTO t_files_upload (id, filename, md5, size_bytes, kind, status, uploaded_at)
		VALUES ('up-preset', 'p.log', 'md5preset', 10, 'file', 'parsed', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert upload: %v", err)
	}
	body := strings.NewReader(`{"name":"моя конфигурация","snapshot":{"q":"err","from":"2026-01-01T00:00:00Z"}}`)
	code, b := doReq(t, srv, http.MethodPost, "/api/uploads/up-preset/presets", body, "application/json")
	if code != http.StatusCreated {
		t.Fatalf("POST preset = %d, body=%s", code, b)
	}
	var p map[string]any
	json.Unmarshal(b, &p)
	pid, _ := p["id"].(string)
	if pid == "" {
		t.Fatal("нет id пресета")
	}
	if p["name"] != "моя конфигурация" {
		t.Errorf("preset name = %v", p["name"])
	}
	if snap, ok := p["snapshot"].(map[string]any); !ok || snap["q"] != "err" {
		t.Errorf("preset snapshot round-trip неверен: %v", p["snapshot"])
	}
	// GET list → 1.
	code, b = doReq(t, srv, http.MethodGet, "/api/uploads/up-preset/presets", nil, "")
	if code != http.StatusOK {
		t.Fatalf("GET presets = %d", code)
	}
	var list []map[string]any
	json.Unmarshal(b, &list)
	if len(list) != 1 {
		t.Errorf("presets len = %d, want 1", len(list))
	}
	// DELETE → 204.
	code, _ = doReq(t, srv, http.MethodDelete, "/api/uploads/up-preset/presets/"+pid, nil, "")
	if code != http.StatusNoContent {
		t.Errorf("DELETE preset = %d, want 204", code)
	}
	// повторный DELETE → 404.
	code, _ = doReq(t, srv, http.MethodDelete, "/api/uploads/up-preset/presets/"+pid, nil, "")
	if code != http.StatusNotFound {
		t.Errorf("DELETE preset again = %d, want 404", code)
	}
}

// TestAnnotationsCRUD — аннотации-пины: entry-pin, time-pin, валидация, null→JSON null.
func TestAnnotationsCRUD(t *testing.T) {
	srv, _ := newAPIServer(t)
	ctx := context.Background()
	if _, err := srv.db.ExecContext(ctx,
		`INSERT INTO t_files_upload (id, filename, md5, size_bytes, kind, status, uploaded_at)
		VALUES ('up-ann', 'a.log', 'md5ann', 10, 'file', 'parsed', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert upload: %v", err)
	}
	if _, err := srv.db.ExecContext(ctx,
		`INSERT INTO t_files_analyze (id, upload_id, filename, format, status, record_count)
		VALUES ('fa-ann', 'up-ann', 'a.log', 'text', 'parsed', 1)`); err != nil {
		t.Fatalf("insert file: %v", err)
	}
	// entry-pin: file_analyze_id + entry_id.
	body := strings.NewReader(`{"file_analyze_id":"fa-ann","entry_id":42,"note":"вот тут ошибка","color":"#ef4444"}`)
	code, b := doReq(t, srv, http.MethodPost, "/api/uploads/up-ann/annotations", body, "application/json")
	if code != http.StatusCreated {
		t.Fatalf("POST entry-pin = %d, body=%s", code, b)
	}
	var a1 map[string]any
	json.Unmarshal(b, &a1)
	aid1, _ := a1["id"].(string)
	if a1["entry_id"].(float64) != 42 {
		t.Errorf("entry-pin entry_id = %v, want 42", a1["entry_id"])
	}
	if a1["ts"] != nil {
		t.Errorf("entry-pin ts = %v, want null", a1["ts"])
	}
	// time-pin: ts.
	body = strings.NewReader(`{"ts":"2026-01-01T12:00:00Z","note":"точка во времени","color":"#3b82f6"}`)
	code, b = doReq(t, srv, http.MethodPost, "/api/uploads/up-ann/annotations", body, "application/json")
	if code != http.StatusCreated {
		t.Fatalf("POST time-pin = %d, body=%s", code, b)
	}
	var a2 map[string]any
	json.Unmarshal(b, &a2)
	aid2, _ := a2["id"].(string)
	if a2["file_analyze_id"] != nil {
		t.Errorf("time-pin file_analyze_id = %v, want null", a2["file_analyze_id"])
	}
	// невалид: только entry_id без file_analyze_id → 400.
	body = strings.NewReader(`{"entry_id":1,"note":"x","color":"#000"}`)
	code, _ = doReq(t, srv, http.MethodPost, "/api/uploads/up-ann/annotations", body, "application/json")
	if code != http.StatusBadRequest {
		t.Errorf("POST half entry-pin = %d, want 400", code)
	}
	// невалид: нет note → 400.
	body = strings.NewReader(`{"ts":"2026-01-01T12:00:00Z","color":"#000"}`)
	code, _ = doReq(t, srv, http.MethodPost, "/api/uploads/up-ann/annotations", body, "application/json")
	if code != http.StatusBadRequest {
		t.Errorf("POST no note = %d, want 400", code)
	}
	// невалид: смешанный пин (и ts, и entry) → 400.
	body = strings.NewReader(`{"ts":"2026-01-01T12:00:00Z","file_analyze_id":"fa-ann","entry_id":1,"note":"x","color":"#000"}`)
	code, _ = doReq(t, srv, http.MethodPost, "/api/uploads/up-ann/annotations", body, "application/json")
	if code != http.StatusBadRequest {
		t.Errorf("POST mixed pin = %d, want 400", code)
	}
	// GET list → 2.
	code, b = doReq(t, srv, http.MethodGet, "/api/uploads/up-ann/annotations", nil, "")
	if code != http.StatusOK {
		t.Fatalf("GET annotations = %d", code)
	}
	var list []map[string]any
	json.Unmarshal(b, &list)
	if len(list) != 2 {
		t.Errorf("annotations len = %d, want 2", len(list))
	}
	// DELETE entry-pin → 204; повторный → 404.
	code, _ = doReq(t, srv, http.MethodDelete, "/api/uploads/up-ann/annotations/"+aid1, nil, "")
	if code != http.StatusNoContent {
		t.Errorf("DELETE annotation = %d, want 204", code)
	}
	code, _ = doReq(t, srv, http.MethodDelete, "/api/uploads/up-ann/annotations/"+aid1, nil, "")
	if code != http.StatusNotFound {
		t.Errorf("DELETE annotation again = %d, want 404", code)
	}
	_ = aid2
}

// TestREGEXPRegistrationIdempotent — db.Open дважды на процесс: sync.Once
// гарантирует однократную регистрацию REGEXP (без него 2-й Open упал бы).
func TestREGEXPRegistrationIdempotent(t *testing.T) {
	d1, err := db.Open("sqlite:" + filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatalf("first db.Open: %v", err)
	}
	defer d1.Close()
	d2, err := db.Open("sqlite:" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatalf("second db.Open: %v", err)
	}
	defer d2.Close()
	// REGEXP работает на обеих БД (функция процесс-глобальна).
	for _, d := range []*db.DB{d1, d2} {
		if err := db.Run(context.Background(), d.DB); err != nil {
			t.Fatalf("Run: %v", err)
		}
		var got int
		if err := d.QueryRowContext(context.Background(),
			"SELECT REGEXP('Sec', 'Security')").Scan(&got); err != nil {
			t.Fatalf("REGEXP query: %v", err)
		}
		if got != 1 {
			t.Errorf("REGEXP('Sec','Security') = %d, want 1", got)
		}
	}
}
