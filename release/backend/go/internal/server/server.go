// Package server — HTTP-сервер backend LogAnalyzer.
//
// REST API (JSON, префикс /api): загрузка/ингестия (US-0002) + viewer (US-0004).
// MVP: SQLite. Источник спецификаций:
//   - architect/specs/ingestion.spec.md (api)
//   - architect/specs/viewer.spec.md (backend.new_endpoints)
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/irav/dev-agent/internal/config"
	"github.com/irav/dev-agent/internal/ingest"
	"github.com/irav/dev-agent/internal/parser"
	"github.com/irav/dev-agent/internal/postprocess"
)

// Server — HTTP-сервер backend.
type Server struct {
	httpServer   *http.Server
	db           *sql.DB
	parsers      *parser.Manager
	postproc     *postprocess.Manager
	ingest       *ingest.Service
	frontendDist string
}

// New создаёт сервер с полным роутингом /api + /healthz.
func New(addr string, sdb *sql.DB, cfg *config.Config, parsers *parser.Manager, postproc *postprocess.Manager) *Server {
	s := &Server{
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           nil,
			ReadHeaderTimeout: 30 * time.Second,
		},
		db:           sdb,
		parsers:      parsers,
		postproc:     postproc,
		frontendDist: cfg.FrontendDist,
		ingest: ingest.NewService(sdb, parsers, postproc, ingest.Limits{
			MaxFileSize:  cfg.MaxFileSize,
			MaxFileCount: cfg.MaxFileCount,
		}, cfg.DefaultTZ),
	}
	s.httpServer.Handler = s.routes()
	return s
}

// routes возвращает http.Handler со всеми эндпоинтами.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", s.healthz)

	// Загрузка/ингестия (US-0002).
	mux.HandleFunc("POST /api/uploads", s.postUploads)
	mux.HandleFunc("GET /api/uploads", s.getUploads)
	mux.HandleFunc("GET /api/uploads/{id}", s.getUpload)
	mux.HandleFunc("DELETE /api/uploads/{id}", s.deleteUpload)

	// Файлы анализа.
	mux.HandleFunc("GET /api/files", s.getFiles)
	mux.HandleFunc("GET /api/files/{id}", s.getFile)
	mux.HandleFunc("GET /api/files/{id}/entries", s.getFileEntries)
	mux.HandleFunc("POST /api/files/{id}/postprocess", s.postFilePostprocess)
	mux.HandleFunc("DELETE /api/files/{id}", s.deleteFile)

	// Парсеры.
	mux.HandleFunc("GET /api/parsers", s.getParsers)

	// Viewer (US-0004).
	mux.HandleFunc("GET /api/stats", s.getStats)
	mux.HandleFunc("GET /api/uploads/{id}/search", s.getSearch)
	mux.HandleFunc("GET /api/uploads/{id}/correlate", s.getCorrelate)
	mux.HandleFunc("GET /api/uploads/{id}/timeline", s.getTimeline)
	mux.HandleFunc("GET /api/uploads/{id}/lexemes", s.getLexemes)
	mux.HandleFunc("GET /api/uploads/{id}/histogram", s.getHistogram)
	mux.HandleFunc("GET /api/uploads/{id}/filters", s.getFilters)
	mux.HandleFunc("POST /api/uploads/{id}/filters", s.postFilter)
	mux.HandleFunc("DELETE /api/uploads/{id}/filters/{fid}", s.deleteFilter)
	mux.HandleFunc("GET /api/uploads/{id}/highlights", s.getHighlights)
	mux.HandleFunc("POST /api/uploads/{id}/highlights", s.postHighlight)
	mux.HandleFunc("DELETE /api/uploads/{id}/highlights/{hid}", s.deleteHighlight)
	mux.HandleFunc("DELETE /api/uploads/{id}/view-state", s.deleteViewState)

	// Viewer 0.6.0 (US-0006): стекированный график, пресеты, аннотации.
	mux.HandleFunc("GET /api/uploads/{id}/histogram-by-file", s.getHistogramByFile)
	mux.HandleFunc("GET /api/uploads/{id}/presets", s.getPresets)
	mux.HandleFunc("POST /api/uploads/{id}/presets", s.postPreset)
	mux.HandleFunc("DELETE /api/uploads/{id}/presets/{pid}", s.deletePreset)
	mux.HandleFunc("GET /api/uploads/{id}/annotations", s.getAnnotations)
	mux.HandleFunc("POST /api/uploads/{id}/annotations", s.postAnnotation)
	mux.HandleFunc("DELETE /api/uploads/{id}/annotations/{aid}", s.deleteAnnotation)

	// Статика frontend (Angular dist) + SPA-fallback на index.html для не-/api.
	s.registerStatic(mux)

	return mux
}

// registerStatic монтирует собранный Angular dist и SPA-fallback.
// distDir — путь к каталогу с index.html (по умолчанию из LA_FRONTEND_DIST или
// release/frontend/dist/la-frontend/browser). Если каталог не существует —
// статика не монтируется (backend работает как чистый API).
func (s *Server) registerStatic(mux *http.ServeMux) {
	distDir := s.frontendDist
	if distDir == "" {
		distDir = config.DefaultFrontendDist
	}
	if info, err := os.Stat(distDir); err != nil || !info.IsDir() {
		return // статика недоступна — API-only режим
	}
	fs := http.FileServer(http.Dir(distDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Не обслуживаем /api и /healthz здесь.
		if strings.HasPrefix(r.URL.Path, "/api") || r.URL.Path == "/healthz" {
			http.NotFound(w, r)
			return
		}
		fullPath := filepath.Join(distDir, filepath.Clean(r.URL.Path))
		if _, err := os.Stat(fullPath); err == nil {
			fs.ServeHTTP(w, r)
			return
		}
		// SPA-fallback: отдаём index.html для клиентских маршрутов (/viewer/:id, ...).
		http.ServeFile(w, r, filepath.Join(distDir, "index.html"))
	})
}

// Addr возвращает адрес прослушивания.
func (s *Server) Addr() string { return s.httpServer.Addr }

// ListenAndServe запускает сервер (блокирующий вызов).
func (s *Server) ListenAndServe() error {
	err := s.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown корректно останавливает сервер.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// ---- helpers ----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// healthz проверяет БД (ping) и возвращает статус.
func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status := "ok"
	if s.db != nil {
		if err := s.db.PingContext(r.Context()); err != nil {
			status = "degraded"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
}
