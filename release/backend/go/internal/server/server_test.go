package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/irav/dev-agent/internal/config"
	"github.com/irav/dev-agent/internal/db"
	"github.com/irav/dev-agent/internal/parser"
	"github.com/irav/dev-agent/internal/postprocess"
)

// freePort возвращает свободный TCP-порт.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// newTestServer создаёт сервер с БД во временном каталоге, миграциями и менеджерами.
func newTestServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	d, err := db.Open("sqlite:" + filepath.Join(t.TempDir(), "la.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Run(context.Background(), d.DB); err != nil {
		t.Fatalf("db.Run: %v", err)
	}
	cfg := &config.Config{DefaultTZ: "UTC", MaxFileSize: 10 << 30, MaxFileCount: 10}
	pm := parser.NewManager("", nil)
	pp := postprocess.NewManager("", nil)
	srv := New("127.0.0.1:0", d.DB, cfg, pm, pp)
	return srv, d
}

func TestHealthzOK(t *testing.T) {
	srv, _ := newTestServer(t)
	ts := httptest.NewServer(http.HandlerFunc(srv.healthz))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
}

func TestHealthzMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	ts := httptest.NewServer(http.HandlerFunc(srv.healthz))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/healthz", "text/plain", nil)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

// TestListenAndServeBindsAddr проверяет, что сервер реально биндится к порту
// и отвечает на /healthz по реальному TCP-соединению.
func TestListenAndServeBindsAddr(t *testing.T) {
	d, err := db.Open("sqlite:" + filepath.Join(t.TempDir(), "la2.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()
	if err := db.Run(context.Background(), d.DB); err != nil {
		t.Fatalf("db.Run: %v", err)
	}
	cfg := &config.Config{DefaultTZ: "UTC", MaxFileSize: 10 << 30, MaxFileCount: 10}
	pm := parser.NewManager("", nil)
	pp := postprocess.NewManager("", nil)

	port := freePort(t)
	srv := New(fmt.Sprintf("127.0.0.1:%d", port), d.DB, cfg, pm, pp)

	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()

	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	var resp *http.Response
	for i := 0; i < 20; i++ {
		resp, err = http.Get(url)
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server не ответил на %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	<-done
}
