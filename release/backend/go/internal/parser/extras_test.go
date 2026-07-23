package parser

import (
	"testing"
)

func TestTextParserBasic(t *testing.T) {
	p := &textParser{}
	recs := runParser(t, p, "some random line\n##### Wed Mar 28 16:17:29 GMT-4 2012 Starting up\nmore\n")
	if len(recs) != 3 {
		t.Fatalf("recs = %d, want 3", len(recs))
	}
	if recs[0].Format != "text" {
		t.Errorf("rec[0].Format = %q", recs[0].Format)
	}
}

func TestTextParserNeverDetects(t *testing.T) {
	p := &textParser{}
	if p.Detect([]string{"anything"}) {
		t.Error("text Detect должен всегда возвращать false (fallback)")
	}
}

func TestDetectEncodingUTF8(t *testing.T) {
	got := DetectEncoding([]byte("hello мир UTF-8 текст"))
	if got != "UTF-8" {
		t.Errorf("DetectEncoding = %q, want UTF-8", got)
	}
}

func TestDetectEncodingEmpty(t *testing.T) {
	if got := DetectEncoding(nil); got != "UTF-8" {
		t.Errorf("DetectEncoding(nil) = %q, want UTF-8", got)
	}
}

func TestDecodeToUTF8Passthrough(t *testing.T) {
	out := DecodeToUTF8([]byte("привет"), "UTF-8")
	if out != "привет" {
		t.Errorf("DecodeToUTF8 = %q", out)
	}
}

func TestManagerNamesIncludesText(t *testing.T) {
	m := NewManager("", nil)
	names := m.Names()
	found := false
	for _, n := range names {
		if n == "text" {
			found = true
		}
	}
	if !found {
		t.Errorf("text отсутствует в менеджере: %v", names)
	}
}

func TestFirstNonEmptyLines(t *testing.T) {
	got := FirstNonEmptyLines([]string{"", "a", "", "b", "c"}, 2)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("FirstNonEmptyLines = %v", got)
	}
}
