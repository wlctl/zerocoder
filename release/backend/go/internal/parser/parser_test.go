package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sampleRoot — путь к фикстурам sample-logs.
func sampleRoot(t *testing.T) string {
	t.Helper()
	// Из internal/parser поднимаемся к корню проекта.
	abs, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "sample-logs"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("sample-logs недоступен: %v", err)
	}
	return abs
}

func readSample(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(sampleRoot(t), rel))
	if err != nil {
		t.Skipf("нельзя прочитать %s: %v", rel, err)
	}
	return string(b)
}

// runParser запускает парсер на тексте и возвращает записи.
func runParser(t *testing.T, p Parser, text string) []Record {
	t.Helper()
	lines := make(chan string, 16)
	go func() {
		defer close(lines)
		for _, l := range strings.Split(text, "\n") {
			lines <- strings.TrimRight(l, "\r")
		}
	}()
	var recs []Record
	if err := p.Parse(lines, func(r Record) { recs = append(recs, r) }); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return recs
}

func TestDetectOracle(t *testing.T) {
	text := readSample(t, "oracle-db/alert_orclcdb.log")
	sample := FirstNonEmptyLines(strings.Split(text, "\n"), 20)
	m := NewManager("", nil)
	p := m.Detect(sample)
	if p == nil || p.Name() != "oracle" {
		t.Fatalf("Detect = %v, want oracle", p)
	}
}

func TestOracleParse(t *testing.T) {
	text := readSample(t, "oracle-db/alert_orclcdb.log")
	p := &oracleParser{}
	recs := runParser(t, p, text)
	if len(recs) == 0 {
		t.Fatal("нет записей")
	}
	// Первая запись — timestamp + "Starting ORACLE instance".
	if recs[0].Ts == nil {
		t.Errorf("rec[0].Ts nil")
	} else {
		want := time.Date(2019, 5, 31, 14, 52, 53, 818309000, time.FixedZone("", -4*3600)).UTC()
		if !recs[0].Ts.Equal(want) {
			t.Errorf("rec[0].Ts = %v, want %v", *recs[0].Ts, want)
		}
	}
	if !strings.Contains(recs[1].Raw, "Starting ORACLE instance") {
		t.Errorf("rec[1].Raw = %q", recs[1].Raw)
	}
}

func TestDetectWeblogic(t *testing.T) {
	text := readSample(t, "weblogic/AdminServer.log")
	sample := FirstNonEmptyLines(strings.Split(text, "\n"), 20)
	m := NewManager("", nil)
	p := m.Detect(sample)
	if p == nil || p.Name() != "weblogic" {
		t.Fatalf("Detect = %v, want weblogic", p)
	}
}

func TestWeblogicParse(t *testing.T) {
	text := readSample(t, "weblogic/AdminServer.log")
	p := &weblogicParser{}
	recs := runParser(t, p, text)
	if len(recs) == 0 {
		t.Fatal("нет записей")
	}
	if recs[0].Ts == nil {
		t.Fatal("rec[0].Ts nil")
	}
	// Millis 1458769894385 → UTC.
	want := time.UnixMilli(1458769894385).UTC()
	if !recs[0].Ts.Equal(want) {
		t.Errorf("rec[0].Ts = %v, want %v", *recs[0].Ts, want)
	}
	if recs[0].Level != "info" {
		t.Errorf("rec[0].Level = %q, want info", recs[0].Level)
	}
	if recs[0].Component != "Security" {
		t.Errorf("rec[0].Component = %q, want Security", recs[0].Component)
	}
}

func TestDetectWlsStdout(t *testing.T) {
	text := readSample(t, "weblogic/AdminServer.out")
	sample := FirstNonEmptyLines(strings.Split(text, "\n"), 20)
	m := NewManager("", nil)
	p := m.Detect(sample)
	if p == nil || p.Name() != "wls_stdout" {
		t.Fatalf("Detect = %v, want wls_stdout", p)
	}
}

func TestWlsStdoutParse(t *testing.T) {
	text := readSample(t, "weblogic/AdminServer.out")
	p := &wlsStdoutParser{}
	recs := runParser(t, p, text)
	if len(recs) == 0 {
		t.Fatal("нет записей")
	}
	if recs[0].Ts == nil {
		t.Fatal("rec[0].Ts nil")
	}
	// <Mar 24, 2016 12:51:34 AM GMT+03:00> → смещение +03:00, tz_inferred=false.
	if recs[0].TZInferred {
		t.Errorf("rec[0].TZInferred = true, want false (явный offset)")
	}
	if recs[0].Level != "info" {
		t.Errorf("rec[0].Level = %q, want info", recs[0].Level)
	}
}

func TestWlsStdoutNodemanagerVariant(t *testing.T) {
	text := readSample(t, "weblogic/nodemanager.log")
	sample := FirstNonEmptyLines(strings.Split(text, "\n"), 20)
	m := NewManager("", nil)
	p := m.Detect(sample)
	if p == nil || p.Name() != "wls_stdout" {
		t.Fatalf("Detect = %v, want wls_stdout (nodemanager variant)", p)
	}
	recs := runParser(t, p, text)
	// Должна найтись запись с форматом даты "YYYY-MM-DD GMT-N HH:MM:SS".
	found := false
	for _, r := range recs {
		if strings.Contains(r.TsRaw, "GMT-3") && r.Ts != nil {
			found = true
		}
	}
	if !found {
		t.Errorf("не разобрана nodemanager-запись с GMT-3; всего записей %d", len(recs))
	}
}

func TestDetectJava(t *testing.T) {
	text := readSample(t, "java-general/install.weblogic.log")
	sample := FirstNonEmptyLines(strings.Split(text, "\n"), 20)
	m := NewManager("", nil)
	p := m.Detect(sample)
	if p == nil || p.Name() != "java" {
		t.Fatalf("Detect = %v, want java", p)
	}
}

func TestJavaParseMultiline(t *testing.T) {
	text := readSample(t, "java-general/install.weblogic.log")
	p := &javaParser{}
	recs := runParser(t, p, text)
	if len(recs) == 0 {
		t.Fatal("нет записей")
	}
	if recs[0].Ts == nil {
		t.Fatal("rec[0].Ts nil")
	}
	if recs[0].TZInferred != true {
		t.Errorf("rec[0].TZInferred = %v, want true (нет TZ в логе)", recs[0].TZInferred)
	}
	// Найдём запись со stack trace (InvocationTargetException) — multiline.
	var stackRec *Record
	for i := range recs {
		if strings.Contains(recs[i].Raw, "InvocationTargetException") {
			stackRec = &recs[i]
			break
		}
	}
	if stackRec == nil {
		t.Fatal("не найдена запись со stack trace")
	}
	if !strings.Contains(stackRec.Raw, "at sun.reflect") {
		t.Errorf("multiline не склеен: raw=%q", stackRec.Raw)
	}
}

func TestDetectAccess(t *testing.T) {
	text := readSample(t, "proxy/access.log")
	sample := FirstNonEmptyLines(strings.Split(text, "\n"), 20)
	m := NewManager("", nil)
	p := m.Detect(sample)
	if p == nil || p.Name() != "access" {
		t.Fatalf("Detect = %v, want access", p)
	}
}

func TestAccessParse(t *testing.T) {
	text := readSample(t, "proxy/access.log")
	p := &accessParser{}
	recs := runParser(t, p, text)
	if len(recs) == 0 {
		t.Fatal("нет записей")
	}
	if recs[0].Ts == nil {
		t.Fatal("rec[0].Ts nil")
	}
	if recs[0].TZInferred {
		t.Errorf("rec[0].TZInferred = true, want false (явный +0400)")
	}
	// 302 → info (не 4xx/5xx).
	if recs[0].Level != "info" {
		t.Errorf("rec[0].Level = %q, want info (302)", recs[0].Level)
	}
}

func TestDetectODL(t *testing.T) {
	text := readSample(t, "weblogic/1/AdminServer-diagnostic.log")
	sample := FirstNonEmptyLines(strings.Split(text, "\n"), 20)
	m := NewManager("", nil)
	p := m.Detect(sample)
	if p == nil || p.Name() != "odl" {
		t.Fatalf("Detect = %v, want odl", p)
	}
}

func TestODLParseMultiline(t *testing.T) {
	text := readSample(t, "weblogic/1/AdminServer-diagnostic.log")
	p := &odlParser{}
	recs := runParser(t, p, text)
	if len(recs) == 0 {
		t.Fatal("нет записей")
	}
	if recs[0].Ts == nil {
		t.Fatal("rec[0].Ts nil")
	}
	// Найдём запись с [[ block ]] — multiline.
	var blockRec *Record
	for i := range recs {
		if strings.Contains(recs[i].Raw, "[[") {
			blockRec = &recs[i]
			break
		}
	}
	if blockRec == nil {
		t.Fatal("не найдена ODL-запись с [[ блоком")
	}
	if !strings.Contains(blockRec.Raw, "Cause:") {
		t.Errorf("multiline [[ ]] не склеен: raw=%q", blockRec.Raw)
	}
}

func TestTextFallback(t *testing.T) {
	m := NewManager("", nil)
	// Ни один detect не сработает → text.
	p := m.Detect([]string{"произвольная строка без даты"})
	if p != nil {
		t.Fatalf("Detect вернул %v, want nil (fallback)", p)
	}
	tp := m.ParserByName("text")
	if tp == nil {
		t.Fatal("text парсер недоступен")
	}
}

func TestDateTimeLayouts(t *testing.T) {
	cases := []struct {
		s     string
		want  string
		infer bool
	}{
		{"2019-05-31T14:52:53.818309-04:00", "2019-05-31T18:52:53.818309Z", false},
		{"16/May/2018:18:34:08 +0400", "2018-05-16T14:34:08Z", false},
		{"2012-03-28 16:15:40,637", "2012-03-28T16:15:40.637Z", true}, // java без TZ → UTC (defaultTZ=UTC)
	}
	for _, tc := range cases {
		dt := ParseDateTime(tc.s, "UTC")
		if dt.Ts == nil {
			t.Errorf("ParseDateTime(%q) Ts nil", tc.s)
			continue
		}
		got := dt.Ts.UTC().Format(time.RFC3339Nano)
		if got != tc.want {
			t.Errorf("ParseDateTime(%q) = %v, want %v", tc.s, got, tc.want)
		}
		if dt.TZInferred != tc.infer {
			t.Errorf("ParseDateTime(%q) inferred=%v, want %v", tc.s, dt.TZInferred, tc.infer)
		}
	}
}

func TestDetectOrderTextLast(t *testing.T) {
	m := NewManager("", nil)
	names := m.Names()
	// text должен быть последним.
	if names[len(names)-1] != "text" {
		t.Errorf("порядок detection: text не последний: %v", names)
	}
}
