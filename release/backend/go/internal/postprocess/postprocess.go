// Package postprocess содержит общий интерфейс постобработчиков лог-файлов и
// менеджер (built-in base + форматные наследники + плагины .so). После парсинга
// каждого файла постобработчик читает t_log_entries и строит сводку (Summary),
// которая сохраняется в t_files_analyze (columns + summary JSON).
//
// Источник спецификации: architect/specs/ingestion.spec.md (postprocessors).
// Связанная пользовательская история: US-0002 (AC-11/AC-12/AC-13).
package postprocess

import (
	"encoding/json"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/irav/dev-agent/internal/parser"
)

// Session — интервал старт-стоп (per формат).
type Session struct {
	StartTs     string `json:"start_ts"`
	StopTs      string `json:"stop_ts"`
	DurationSec int64  `json:"duration_sec"`
	Note        string `json:"note,omitempty"`
}

// Summary — сводка по файлу. Базовые поля + форматные расширения (Sessions, Extras).
type Summary struct {
	TotalRecords int            `json:"total_records"`
	FileSize     int64          `json:"file_size"`
	UploadedAt   string         `json:"uploaded_at"`
	Encoding     string         `json:"encoding"`
	FirstTs      string         `json:"first_ts"`
	LastTs       string         `json:"last_ts"`
	DurationSec  int64          `json:"duration_sec"`
	LevelCounts  map[string]int `json:"level_counts"`
	Sessions     []Session      `json:"sessions,omitempty"`
	Extras       map[string]any `json:"extras,omitempty"`
}

// Entry — одна запись для постобработки (проекция t_log_entries).
type Entry struct {
	Seq       int
	Ts        *time.Time
	Level     string
	Component string
	Message   string
	Raw       string
	Attrs     map[string]any
}

// EntryReader — функция, читающая записи файла (вызывает emit для каждой).
type EntryReader func(emit func(Entry)) error

// Postprocessor — интерфейс постобработчика одного формата.
type Postprocessor interface {
	// Name возвращает имя формата ("oracle","weblogic",...).
	Name() string
	// Process читает записи и строит сводку. file_size/uploaded_at/encoding
	// заполняются менеджером после вызова (Base не имеет к ним доступа из записей).
	Process(fileAnalyzeID string, entries EntryReader) (Summary, error)
}

// Base — базовый постобработчик (built-in, всегда есть). Вычисляет общую сводку
// из записей: total_records, first_ts/last_ts/duration_sec, level_counts.
type Base struct{}

// Name возвращает "base".
func (Base) Name() string { return "base" }

// Process строит базовую сводку по записям.
func (Base) Process(fileAnalyzeID string, entries EntryReader) (Summary, error) {
	s := Summary{
		LevelCounts: map[string]int{},
	}
	var first, last *time.Time
	if err := entries(func(e Entry) {
		s.TotalRecords++
		lvl := e.Level
		if lvl == "" {
			lvl = "unknown"
		}
		s.LevelCounts[lvl]++
		if e.Ts != nil {
			if first == nil || e.Ts.Before(*first) {
				first = e.Ts
			}
			if last == nil || e.Ts.After(*last) {
				last = e.Ts
			}
		}
	}); err != nil {
		return s, err
	}
	if first != nil {
		s.FirstTs = first.UTC().Format(time.RFC3339Nano)
	}
	if last != nil {
		s.LastTs = last.UTC().Format(time.RFC3339Nano)
	}
	if first != nil && last != nil {
		s.DurationSec = int64(last.Sub(*first).Seconds())
	}
	return s, nil
}

// Manager управляет набором постобработчиков: base (всегда) + форматные built-in
// + плагины .so из LA_POSTPROCESSORS_DIR.
type Manager struct {
	procs map[string]Postprocessor // по имени формата
}

// NewManager создаёт менеджер с built-in постобработчиками и загружает плагины.
func NewManager(dir string, logger *log.Logger) *Manager {
	m := &Manager{procs: map[string]Postprocessor{}}
	for _, p := range builtinPostprocessors() {
		m.procs[p.Name()] = p
	}
	if dir != "" {
		m.loadPlugins(dir, logger)
	}
	return m
}

// Names возвращает имена доступных постобработчиков.
func (m *Manager) Names() []string {
	names := make([]string, 0, len(m.procs))
	for n := range m.procs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// PostprocessorFor возвращает постобработчик по формату; если плагина для формата
// нет — возвращается built-in base.
func (m *Manager) PostprocessorFor(format string) Postprocessor {
	if p, ok := m.procs[format]; ok {
		return p
	}
	return m.procs["base"]
}

// HasFormat проверяет, есть ли форматный постобработчик (не base).
func (m *Manager) HasFormat(format string) bool {
	_, ok := m.procs[format]
	return ok && format != "base"
}

// MarshalSummary кодирует Summary в JSON.
func MarshalSummary(s Summary) ([]byte, error) {
	return json.Marshal(s)
}

// ---- helpers для форматных наследников --------------------------------------

// findSessions ищет сессии старт-стоп по записям по правилам start/stop.
// startRe/stopRe — подстроки (case-insensitive) маркеров старта/стопа.
func findSessions(entries EntryReader, startMarkers, stopMarkers []string) []Session {
	var sessions []Session
	var curStart *time.Time
	inSession := false
	entries(func(e Entry) {
		text := strings.ToLower(e.Message + " " + e.Raw)
		if !inSession {
			if containsAny(text, startMarkers) {
				if e.Ts != nil {
					t := *e.Ts
					curStart = &t
					inSession = true
				}
			}
		} else {
			if containsAny(text, stopMarkers) && curStart != nil {
				if e.Ts != nil {
					sessions = append(sessions, Session{
						StartTs:     curStart.UTC().Format(time.RFC3339Nano),
						StopTs:      e.Ts.UTC().Format(time.RFC3339Nano),
						DurationSec: int64(e.Ts.Sub(*curStart).Seconds()),
						Note:        "start-stop",
					})
				}
				inSession = false
				curStart = nil
			}
		}
	})
	return sessions
}

// containsAny проверяет, содержит ли text любую из подстрок markers.
func containsAny(text string, markers []string) bool {
	for _, m := range markers {
		if m == "" {
			continue
		}
		if strings.Contains(text, strings.ToLower(m)) {
			return true
		}
	}
	return false
}

// entryReaderFromParser конвертирует []parser.Record в EntryReader (для тестов).
func entryReaderFromParser(recs []parser.Record) EntryReader {
	return func(emit func(Entry)) error {
		for _, r := range recs {
			emit(Entry{
				Seq: r.Seq, Ts: r.Ts, Level: r.Level, Component: r.Component,
				Message: r.Message, Raw: r.Raw, Attrs: r.Attrs,
			})
		}
		return nil
	}
}
