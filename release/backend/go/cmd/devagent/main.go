// Package main — точка входа backend LogAnalyzer (la).
//
// При старте: загружает la.conf (создаёт из шаблона при отсутствии), открывает БД
// по SOURCE_DB_URL (SQLite для MVP, авто-создание), применяет миграции, загружает
// парсеры-плагины (LA_PARSERS_DIR) и постобработчики (LA_POSTPROCESSORS_DIR),
// поднимает HTTP на LISTEN_ADDRESS:LISTEN_PORT с REST API /api + статикой
// frontend (SPA). Graceful shutdown по SIGINT/SIGTERM.
//
// Источник спецификаций: architect/specs/{config,ingestion,viewer}.spec.md.
// Пользовательские истории: US-0001, US-0002, US-0004.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/irav/dev-agent/internal/config"
	"github.com/irav/dev-agent/internal/db"
	"github.com/irav/dev-agent/internal/parser"
	"github.com/irav/dev-agent/internal/postprocess"
	"github.com/irav/dev-agent/internal/server"
)

const version = "0.6.0"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("la backend %s starting", version)

	confPath := config.ConfPath(config.DefaultConfFilename)
	cfg, err := config.Load(confPath)
	if err != nil {
		log.Fatalf("config load %s: %v", confPath, err)
	}
	log.Printf("config: SOURCE_DB_URL=%s listen=%s max_file_size=%d max_file_count=%d tz=%s",
		cfg.SourceDBURL, cfg.ListenAddr(), cfg.MaxFileSize, cfg.MaxFileCount, cfg.DefaultTZ)

	database, err := db.Open(cfg.SourceDBURL)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer database.Close()

	migrCtx, migrCancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = db.Run(migrCtx, database.DB)
	migrCancel()
	if err != nil {
		log.Fatalf("db migrations: %v", err)
	}
	log.Printf("db migrations applied")

	// Парсеры (built-in + плагины .so из LA_PARSERS_DIR).
	parserMgr := parser.NewManager(cfg.ParsersDir, log.Default())
	log.Printf("parsers loaded: %v", parserMgr.Names())

	// Постобработчики (built-in + плагины .so из LA_POSTPROCESSORS_DIR).
	postprocMgr := postprocess.NewManager(cfg.PostprocessorsDir, log.Default())
	log.Printf("postprocessors loaded: %v", postprocMgr.Names())

	srv := server.New(cfg.ListenAddr(), database.DB, cfg, parserMgr, postprocMgr)

	// Graceful shutdown по SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("shutdown signal received")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Printf("HTTP listening on %s", cfg.ListenAddr())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http: %v", err)
	}
	log.Printf("la backend stopped")
}
