package postprocess

import (
	"testing"
	"time"

	"github.com/irav/dev-agent/internal/parser"
)

func makeEntries() []parser.Record {
	t1 := mustParse("2026-01-01T10:00:00Z")
	t2 := mustParse("2026-01-01T10:05:00Z")
	t3 := mustParse("2026-01-01T11:00:00Z")
	return []parser.Record{
		{Seq: 1, Ts: &t1, Level: "info", Message: "Server state changed to STARTING", Raw: "..."},
		{Seq: 2, Ts: &t2, Level: "warning", Message: "some warning", Raw: "..."},
		{Seq: 3, Ts: &t3, Level: "error", Message: "Server state changed to SHUTDOWN", Raw: "..."},
	}
}

func mustParse(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestBaseSummary(t *testing.T) {
	recs := makeEntries()
	s, err := Base{}.Process("fa1", entryReaderFromParser(recs))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if s.TotalRecords != 3 {
		t.Errorf("TotalRecords = %d, want 3", s.TotalRecords)
	}
	if s.LevelCounts["info"] != 1 || s.LevelCounts["warning"] != 1 || s.LevelCounts["error"] != 1 {
		t.Errorf("LevelCounts = %v", s.LevelCounts)
	}
	if s.FirstTs != "2026-01-01T10:00:00Z" {
		t.Errorf("FirstTs = %q", s.FirstTs)
	}
	if s.LastTs != "2026-01-01T11:00:00Z" {
		t.Errorf("LastTs = %q", s.LastTs)
	}
	if s.DurationSec != 3600 {
		t.Errorf("DurationSec = %d, want 3600", s.DurationSec)
	}
}

func TestWeblogicSessions(t *testing.T) {
	recs := makeEntries()
	s, err := (&weblogicPP{}).Process("fa1", entryReaderFromParser(recs))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(s.Sessions) != 1 {
		t.Fatalf("Sessions = %d, want 1", len(s.Sessions))
	}
	sess := s.Sessions[0]
	if sess.StartTs != "2026-01-01T10:00:00Z" {
		t.Errorf("StartTs = %q", sess.StartTs)
	}
	if sess.StopTs != "2026-01-01T11:00:00Z" {
		t.Errorf("StopTs = %q", sess.StopTs)
	}
	if sess.DurationSec != 3600 {
		t.Errorf("DurationSec = %d", sess.DurationSec)
	}
}

func TestManagerFallbackToBase(t *testing.T) {
	m := NewManager("", nil)
	pp := m.PostprocessorFor("java") // нет форматного java-наследника → base
	if pp.Name() != "base" {
		t.Errorf("PostprocessorFor(java) = %q, want base", pp.Name())
	}
}

func TestManagerHasOracle(t *testing.T) {
	m := NewManager("", nil)
	if !m.HasFormat("oracle") {
		t.Error("oracle постобработчик недоступен")
	}
	if !m.HasFormat("weblogic") {
		t.Error("weblogic постобработчик недоступен")
	}
}

func TestMarshalSummary(t *testing.T) {
	s := Summary{TotalRecords: 5, LevelCounts: map[string]int{"info": 3, "error": 2}}
	b, err := MarshalSummary(s)
	if err != nil {
		t.Fatalf("MarshalSummary: %v", err)
	}
	if string(b) == "" {
		t.Error("пустой JSON")
	}
}
