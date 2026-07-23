package ingest

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/irav/dev-agent/internal/db"
	"github.com/irav/dev-agent/internal/parser"
	"github.com/irav/dev-agent/internal/postprocess"
)

func newService(t *testing.T) (*Service, *db.DB) {
	t.Helper()
	d, err := db.Open("sqlite:" + filepath.Join(t.TempDir(), "la.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Run(context.Background(), d.DB); err != nil {
		t.Fatalf("db.Run: %v", err)
	}
	pm := parser.NewManager("", nil)
	pp := postprocess.NewManager("", nil)
	s := NewService(d.DB, pm, pp, Limits{MaxFileSize: 10 << 20, MaxFileCount: 10}, "UTC")
	return s, d
}

func sampleRoot(t *testing.T) string {
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

func TestIngestSingleFile(t *testing.T) {
	s, d := newService(t)
	content, err := os.ReadFile(filepath.Join(sampleRoot(t), "weblogic/AdminServer.log"))
	if err != nil {
		t.Skipf("нельзя прочитать образец: %v", err)
	}
	res, err := s.IngestStream(context.Background(), "AdminServer.log", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("IngestStream: %v", err)
	}
	if res.Kind != "file" {
		t.Errorf("Kind = %q, want file", res.Kind)
	}
	if len(res.Files) != 1 {
		t.Fatalf("Files = %d, want 1", len(res.Files))
	}
	if res.Files[0].Format != "weblogic" {
		t.Errorf("Format = %q, want weblogic", res.Files[0].Format)
	}
	if res.Files[0].RecordCount == 0 {
		t.Error("RecordCount = 0")
	}
	// Записи в БД.
	var n int
	_ = d.QueryRowContext(context.Background(),
		"SELECT count(*) FROM t_log_entries WHERE file_analyze_id=?", res.Files[0].ID).Scan(&n)
	if n == 0 {
		t.Error("в t_log_entries нет записей")
	}
	// Сводка заполнена.
	var sumJSON, encoding sql.NullString
	_ = d.QueryRowContext(context.Background(),
		"SELECT summary, encoding FROM t_files_analyze WHERE id=?", res.Files[0].ID).Scan(&sumJSON, &encoding)
	if !sumJSON.Valid || sumJSON.String == "" {
		t.Error("summary пуст")
	}
	if encoding.String == "" {
		t.Error("encoding пуст")
	}
}

func TestIngestDuplicate(t *testing.T) {
	s, _ := newService(t)
	content := []byte("line1\nline2\n")
	if _, err := s.IngestStream(context.Background(), "dup.log", bytes.NewReader(content)); err != nil {
		t.Fatalf("first IngestStream: %v", err)
	}
	_, err := s.IngestStream(context.Background(), "dup.log", bytes.NewReader(content))
	if err == nil {
		t.Fatal("ожидалась ошибка дедупа")
	}
	if !isDuplicate(err) {
		t.Errorf("err = %v, want ErrDuplicate", err)
	}
}

func TestIngestTooLarge(t *testing.T) {
	s, _ := newService(t)
	// MaxFileSize = 10MB; отправим 11MB.
	big := bytes.Repeat([]byte("a"), 11<<20)
	_, err := s.IngestStream(context.Background(), "big.log", bytes.NewReader(big))
	if err != ErrTooLarge {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

func TestIngestZip(t *testing.T) {
	s, d := newService(t)
	// Создаём zip с двумя текстовыми логами.
	zipPath := filepath.Join(t.TempDir(), "test.zip")
	if err := makeTestZip(zipPath, map[string]string{
		"a.log":   "####<May 16, 2018 6:26:25 PM MSK> <Notice> <Security> <host> <AdminServer> <main> <<WLS Kernel>> <> <> <1526480785513> <BEA-090082> <Security initializing>\n",
		"b/b.log": "2012-03-28 16:15:40,637 INFO  [readSilentXML] com.bea.Parser - done\n",
	}); err != nil {
		t.Fatalf("makeTestZip: %v", err)
	}
	content, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	res, err := s.IngestStream(context.Background(), "test.zip", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("IngestStream: %v", err)
	}
	if res.Kind != "zip" {
		t.Errorf("Kind = %q, want zip", res.Kind)
	}
	if len(res.Files) != 2 {
		t.Fatalf("Files = %d, want 2", len(res.Files))
	}
	var n int
	_ = d.QueryRowContext(context.Background(),
		"SELECT count(*) FROM t_files_analyze WHERE upload_id=?", res.UploadID).Scan(&n)
	if n != 2 {
		t.Errorf("t_files_analyze count = %d, want 2", n)
	}
}

// isDuplicate проверяет, что ошибка — дедуп.
func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	return err == ErrDuplicate || err.Error() == ErrDuplicate.Error() || bytes.Contains([]byte(err.Error()), []byte(ErrDuplicate.Error()))
}
