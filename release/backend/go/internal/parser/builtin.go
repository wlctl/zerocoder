// Package parser: builtin.go — встроенные парсеры форматов логов по образцам
// sample-logs/ (см. ingestion.spec.md → parsers.parse_patterns).
//
// Порядок detection: oracle, weblogic, wls_stdout, java, access, odl; fallback text.
package parser

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// builtinParsers возвращает список встроенных парсеров в порядке detection.
func builtinParsers() []Parser {
	return []Parser{
		NewOracleParser(),
		NewWeblogicParser(),
		NewWlsStdoutParser(),
		NewJavaParser(),
		NewAccessParser(),
		NewOdlParser(),
		NewTextParser(),
	}
}

// Экспортированные конструкторы форматов. Используются как built-in, так и
// плагинами .so (parsers/<fmt>/main.go: func New() Parser { return parser.NewXxxParser() }).
// Плагин с тем же Name() заменяет built-in при загрузке (override по имени).

// NewOracleParser возвращает парсер формата oracle (alert.log).
func NewOracleParser() Parser { return &oracleParser{} }

// NewWeblogicParser возвращает парсер формата weblogic (.log, ####<...>).
func NewWeblogicParser() Parser { return &weblogicParser{} }

// NewWlsStdoutParser возвращает парсер формата wls_stdout (.out, nodemanager).
func NewWlsStdoutParser() Parser { return &wlsStdoutParser{} }

// NewJavaParser возвращает парсер формата java (log4j-style).
func NewJavaParser() Parser { return &javaParser{} }

// NewAccessParser возвращает парсер формата access (apache common log).
func NewAccessParser() Parser { return &accessParser{} }

// NewOdlParser возвращает парсер формата odl (Oracle Diagnostic Log).
func NewOdlParser() Parser { return &odlParser{} }

// NewTextParser возвращает fallback-парсер text (посточный, всегда есть в хосте).
func NewTextParser() Parser { return &textParser{} }

// ---- Уровни (severity_dictionary) -------------------------------------------

// normalizeLevel приводит сырой уровень к нормализованному значению
// (error|warning|info|debug|trace|critical) по словарю из spec.
func normalizeLevel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	low := strings.ToLower(raw)
	switch low {
	case "error", "alert", "critical", "emergency", "severe", "fatal":
		if low == "critical" || low == "emergency" || low == "fatal" {
			return "critical"
		}
		return "error"
	case "warning", "warn":
		return "warning"
	case "info", "notice", "notification", "config":
		return "info"
	case "debug", "finest", "fine", "finer":
		return "debug"
	case "trace":
		return "trace"
	}
	// Спец-маркеры Oracle/ошибок.
	if strings.Contains(low, "ora-") || strings.Contains(low, "error") || strings.Contains(low, "critical") || strings.Contains(low, "emergency") {
		if strings.Contains(low, "critical") || strings.Contains(low, "emergency") {
			return "critical"
		}
		return "error"
	}
	if strings.Contains(low, "warn") {
		return "warning"
	}
	return ""
}

// levelByContent пытается извлечь уровень из текста записи (для oracle/text).
func levelByContent(text string) string {
	low := strings.ToLower(text)
	if strings.Contains(low, "ora-") || strings.Contains(low, "errors") || strings.Contains(low, "critical") || strings.Contains(low, "emergency") || strings.Contains(low, "fatal") {
		if strings.Contains(low, "critical") || strings.Contains(low, "emergency") || strings.Contains(low, "fatal") {
			return "critical"
		}
		return "error"
	}
	if strings.Contains(low, "warning") || strings.Contains(low, "warn") {
		return "warning"
	}
	return ""
}

// ---- Oracle -----------------------------------------------------------------

type oracleParser struct{}

func (p *oracleParser) Name() string { return "oracle" }

var oracleDetectRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+[-+]\d{2}:\d{2}$`)

func (p *oracleParser) Detect(sample []string) bool {
	for _, l := range sample {
		if oracleDetectRe.MatchString(strings.TrimSpace(l)) {
			return true
		}
	}
	return false
}

func (p *oracleParser) Parse(lines <-chan string, emit func(Record)) error {
	var (
		buf       []string
		headTS    string
		seq       int
		defaultTZ = "UTC" // oracle всегда имеет явный offset, defaultTZ не используется
	)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		raw := strings.Join(buf, "\n")
		seq++
		rec := Record{Seq: seq, Format: "oracle", Raw: raw, Attrs: map[string]any{}}
		if headTS != "" {
			dt := ParseDateTime(headTS, defaultTZ)
			rec.Ts = dt.Ts
			rec.TsRaw = dt.TsRaw
			rec.TZOffset = dt.TZOffset
			rec.TZInferred = dt.TZInferred
		}
		// Сообщение — первая непустая строка после timestamp.
		for i := 1; i < len(buf); i++ {
			if s := strings.TrimSpace(buf[i]); s != "" {
				rec.Message = s
				break
			}
		}
		if rec.Message == "" && len(buf) > 0 {
			rec.Message = strings.TrimSpace(buf[0])
		}
		rec.Level = levelByContent(raw)
		emit(rec)
		buf = nil
		headTS = ""
	}
	for l := range lines {
		trimmed := strings.TrimRight(l, "\r")
		if oracleDetectRe.MatchString(strings.TrimSpace(trimmed)) {
			flush()
			headTS = strings.TrimSpace(trimmed)
			buf = append(buf, headTS)
			continue
		}
		if headTS == "" {
			// Строки до первого timestamp — отдельные записи (free text).
			if strings.TrimSpace(trimmed) == "" {
				continue
			}
			seq++
			emit(Record{Seq: seq, Format: "oracle", Raw: trimmed, Message: strings.TrimSpace(trimmed), Attrs: map[string]any{}})
			continue
		}
		buf = append(buf, trimmed)
	}
	flush()
	return nil
}

// ---- WebLogic (.log) --------------------------------------------------------

type weblogicParser struct{}

func (p *weblogicParser) Name() string { return "weblogic" }

func (p *weblogicParser) Detect(sample []string) bool {
	for _, l := range sample {
		if strings.HasPrefix(strings.TrimSpace(l), "####<") {
			return true
		}
	}
	return false
}

var weblogicHeadRe = regexp.MustCompile(`^####<(.*)$`)

// Поля weblogic-записи разделены "> <". Парсим головную строку: разбиваем по
// " <" с учётом, что первое поле начинается с "####<ts>".
func (p *weblogicParser) Parse(lines <-chan string, emit func(Record)) error {
	var (
		buf       []string
		head      string // головная строка (####<...>)
		seq       int
		defaultTZ = "UTC"
	)
	flush := func() {
		if head == "" {
			return
		}
		raw := strings.Join(buf, "\n")
		seq++
		rec := Record{Seq: seq, Format: "weblogic", Raw: raw, Attrs: map[string]any{}}
		fields := splitWeblogicFields(head)
		// fields: [0]ts [1]Severity [2]Subsystem [3]Machine [4]Server [5]Thread [6]... [N-3]Millis [N-2]Code [N-1]Msg
		if len(fields) >= 1 {
			rec.TsRaw = fields[0]
		}
		// Millis (epoch ms) — предпоследнее перед Code? По spec: <{Millis}> <{Code}> <{Msg}>.
		// Ищем Millis как числовое поле перед Code.
		if len(fields) >= 3 {
			if ms, err := strconv.ParseInt(strings.TrimSpace(fields[len(fields)-3]), 10, 64); err == nil {
				t := time.UnixMilli(ms).UTC()
				rec.Ts = &t
				rec.TZInferred = false
				rec.TZOffset = "Z"
			}
		}
		if len(fields) >= 2 {
			rec.Level = normalizeLevel(fields[1])
		}
		if len(fields) >= 3 {
			rec.Component = fields[2]
		}
		// Attrs: machine, server, thread, code, millis
		if len(fields) >= 6 {
			rec.Attrs["machine"] = fields[3]
			rec.Attrs["server"] = fields[4]
			rec.Attrs["thread"] = fields[5]
		}
		if len(fields) >= 3 {
			rec.Attrs["millis"] = fields[len(fields)-3]
			rec.Attrs["code"] = fields[len(fields)-2]
		}
		// Message — последнее поле + возможные multiline-продолжения.
		if len(fields) >= 1 {
			rec.Message = strings.TrimSpace(fields[len(fields)-1])
		}
		if len(buf) > 1 {
			extra := strings.Join(buf[1:], "\n")
			if rec.Message != "" {
				rec.Message = rec.Message + "\n" + extra
			} else {
				rec.Message = extra
			}
		}
		emit(rec)
		buf = nil
		head = ""
	}
	for l := range lines {
		trimmed := strings.TrimRight(l, "\r")
		if strings.HasPrefix(strings.TrimSpace(trimmed), "####<") {
			flush()
			head = strings.TrimSpace(trimmed)
			buf = append(buf, head)
			continue
		}
		if head == "" {
			// Строки до первой weblogic-записи — игнорируем (обычно пусто).
			if strings.TrimSpace(trimmed) == "" {
				continue
			}
			seq++
			emit(Record{Seq: seq, Format: "weblogic", Raw: trimmed, Message: strings.TrimSpace(trimmed), Attrs: map[string]any{}})
			continue
		}
		buf = append(buf, trimmed)
	}
	flush()
	_ = defaultTZ
	return nil
}

// splitWeblogicFields разбивает головную строку "####<ts> <Sev> <Sub> ... <Msg>"
// на поля по разделителю "> <" (с учётом ведущего "####<" и завершающего ">").
func splitWeblogicFields(head string) []string {
	s := head
	s = strings.TrimPrefix(s, "####<")
	s = strings.TrimSuffix(s, ">")
	// Разделяем по "> <".
	return strings.Split(s, "> <")
}

// ---- WLS stdout (.out, nodemanager) -----------------------------------------

type wlsStdoutParser struct{}

func (p *wlsStdoutParser) Name() string { return "wls_stdout" }

var (
	wlsStdoutDetectRe = regexp.MustCompile(`^<[A-Z][a-z]{2} \d{1,2}, \d{4} \d{1,2}:\d{2}:\d{2} (AM|PM) [^>]+>`)
	wlsNodemanagerRe  = regexp.MustCompile(`^<\d{4}-\d{2}-\d{2} GMT[-+]\d+ \d{2}:\d{2}:\d{2}>`)
)

func (p *wlsStdoutParser) Detect(sample []string) bool {
	for _, l := range sample {
		t := strings.TrimSpace(l)
		if wlsStdoutDetectRe.MatchString(t) || wlsNodemanagerRe.MatchString(t) {
			return true
		}
	}
	return false
}

func (p *wlsStdoutParser) Parse(lines <-chan string, emit func(Record)) error {
	var (
		buf       []string
		head      string
		seq       int
		defaultTZ = "UTC"
	)
	isHead := func(l string) bool {
		return wlsStdoutDetectRe.MatchString(l) || wlsNodemanagerRe.MatchString(l)
	}
	flush := func() {
		if head == "" {
			return
		}
		raw := strings.Join(buf, "\n")
		seq++
		rec := Record{Seq: seq, Format: "wls_stdout", Raw: raw, Attrs: map[string]any{}}
		// Парсим головную строку: <{ts}> <{LEVEL}> <{Component}> <{Msg}>
		inner := head
		inner = strings.TrimPrefix(inner, "<")
		inner = strings.TrimSuffix(inner, ">")
		fields := strings.SplitN(inner, "> <", 4)
		if len(fields) >= 1 {
			rec.TsRaw = strings.TrimSpace(fields[0])
			dt := ParseDateTime(rec.TsRaw, defaultTZ)
			rec.Ts = dt.Ts
			rec.TZOffset = dt.TZOffset
			rec.TZInferred = dt.TZInferred
		}
		if len(fields) >= 2 {
			rec.Level = normalizeLevel(fields[1])
		}
		if len(fields) >= 3 {
			rec.Component = fields[2]
		}
		if len(fields) >= 4 {
			rec.Message = strings.TrimSpace(fields[3])
		}
		if len(buf) > 1 {
			extra := strings.Join(buf[1:], "\n")
			if rec.Message != "" {
				rec.Message = rec.Message + "\n" + extra
			} else {
				rec.Message = extra
			}
		}
		emit(rec)
		buf = nil
		head = ""
	}
	for l := range lines {
		trimmed := strings.TrimRight(l, "\r")
		if isHead(strings.TrimSpace(trimmed)) {
			flush()
			head = strings.TrimSpace(trimmed)
			buf = append(buf, head)
			continue
		}
		if head == "" {
			if strings.TrimSpace(trimmed) == "" {
				continue
			}
			seq++
			emit(Record{Seq: seq, Format: "wls_stdout", Raw: trimmed, Message: strings.TrimSpace(trimmed), Attrs: map[string]any{}})
			continue
		}
		buf = append(buf, trimmed)
	}
	flush()
	return nil
}

// ---- Java (log4j-style) -----------------------------------------------------

type javaParser struct{}

func (p *javaParser) Name() string { return "java" }

var (
	javaDetectRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d{3}\s+(INFO|WARN|ERROR|DEBUG|TRACE|FATAL)\b`)
	javaHeadRe     = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d{3})\s+(INFO|WARN|ERROR|DEBUG|TRACE|FATAL)\s*(?:\[([^\]]*)\])?\s*([^\s]+)\s*-\s*(.*)$`)
	javaContinueRe = regexp.MustCompile(`^\s+at |^Caused by:|^\t|^\s+\.\.\. `)
)

func (p *javaParser) Detect(sample []string) bool {
	for _, l := range sample {
		if javaDetectRe.MatchString(strings.TrimSpace(l)) {
			return true
		}
	}
	return false
}

func (p *javaParser) Parse(lines <-chan string, emit func(Record)) error {
	var (
		buf       []string
		head      string
		seq       int
		defaultTZ = "UTC"
	)
	flush := func() {
		if head == "" {
			return
		}
		raw := strings.Join(buf, "\n")
		seq++
		rec := Record{Seq: seq, Format: "java", Raw: raw, Attrs: map[string]any{}}
		m := javaHeadRe.FindStringSubmatch(head)
		if m != nil {
			rec.TsRaw = m[1]
			dt := ParseDateTime(m[1], defaultTZ)
			rec.Ts = dt.Ts
			rec.TZOffset = dt.TZOffset
			rec.TZInferred = dt.TZInferred
			rec.Level = normalizeLevel(m[2])
			if m[3] != "" {
				rec.Attrs["thread"] = m[3]
			}
			rec.Component = m[4]
			rec.Message = strings.TrimSpace(m[5])
		}
		if len(buf) > 1 {
			extra := strings.Join(buf[1:], "\n")
			if rec.Message != "" {
				rec.Message = rec.Message + "\n" + extra
			} else {
				rec.Message = extra
			}
		}
		emit(rec)
		buf = nil
		head = ""
	}
	for l := range lines {
		trimmed := strings.TrimRight(l, "\r")
		if javaDetectRe.MatchString(strings.TrimSpace(trimmed)) {
			flush()
			head = strings.TrimSpace(trimmed)
			buf = append(buf, head)
			continue
		}
		if head != "" {
			if javaContinueRe.MatchString(trimmed) || strings.TrimSpace(trimmed) == "" {
				buf = append(buf, trimmed)
				continue
			}
			// Не continuation и не head → новая запись без стандартного header.
			flush()
			if strings.TrimSpace(trimmed) == "" {
				continue
			}
			seq++
			emit(Record{Seq: seq, Format: "java", Raw: trimmed, Message: strings.TrimSpace(trimmed), Attrs: map[string]any{}})
			continue
		}
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		seq++
		emit(Record{Seq: seq, Format: "java", Raw: trimmed, Message: strings.TrimSpace(trimmed), Attrs: map[string]any{}})
	}
	flush()
	return nil
}

// ---- Access (apache common) ------------------------------------------------

type accessParser struct{}

func (p *accessParser) Name() string { return "access" }

var (
	accessDetectRe = regexp.MustCompile(`^\S+ \S+ \S+ \[\d{2}/\w{3}/\d{4}:\d{2}:\d{2}:\d{2} [-+]\d{4}\] "`)
	accessFullRe   = regexp.MustCompile(`^(\S+) (\S+) (\S+) \[([^\]]+)\] "(\S+) (\S+) (\S+)" (\d+) (\S+)`)
)

func (p *accessParser) Detect(sample []string) bool {
	for _, l := range sample {
		if accessDetectRe.MatchString(strings.TrimSpace(l)) {
			return true
		}
	}
	return false
}

func (p *accessParser) Parse(lines <-chan string, emit func(Record)) error {
	seq := 0
	for l := range lines {
		trimmed := strings.TrimRight(l, "\r")
		seq++
		rec := Record{Seq: seq, Format: "access", Raw: trimmed, Attrs: map[string]any{}}
		m := accessFullRe.FindStringSubmatch(strings.TrimSpace(trimmed))
		if m != nil {
			rec.TsRaw = m[4] // "16/May/2018:18:34:08 +0400"
			dt := ParseDateTime(m[4], "UTC")
			rec.Ts = dt.Ts
			rec.TZOffset = dt.TZOffset
			rec.TZInferred = dt.TZInferred
			rec.Component = m[5] + " " + m[6] // method path
			rec.Message = m[5] + " " + m[6] + " " + m[7]
			status, _ := strconv.Atoi(m[8])
			rec.Attrs["host"] = m[1]
			rec.Attrs["method"] = m[5]
			rec.Attrs["path"] = m[6]
			rec.Attrs["proto"] = m[7]
			rec.Attrs["status"] = status
			rec.Attrs["size"] = m[9]
			rec.Level = levelFromHTTPStatus(status)
		} else {
			rec.Message = strings.TrimSpace(trimmed)
		}
		emit(rec)
	}
	return nil
}

// levelFromHTTPStatus возвращает уровень по HTTP-статусу: 5xx→error, 4xx→warning, иначе info.
func levelFromHTTPStatus(status int) string {
	switch {
	case status >= 500:
		return "error"
	case status >= 400:
		return "warning"
	default:
		return "info"
	}
}

// ---- ODL (Oracle Diagnostic Log) -------------------------------------------

type odlParser struct{}

func (p *odlParser) Name() string { return "odl" }

var (
	odlDetectRe = regexp.MustCompile(`^\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+[+-]\d{2}:\d{2}\]`)
	odlHeadRe   = regexp.MustCompile(`^\[([^\]]+)\] \[([^\]]*)\] \[([^\]]*)\] \[([^\]]*)\] \[([^\]]*)\] \[tid: ([^\]]*)\] \[userId: ([^\]]*)\] \[ecid: ([^\]]*)\]\s*(.*)$`)
)

func (p *odlParser) Detect(sample []string) bool {
	for _, l := range sample {
		if odlDetectRe.MatchString(strings.TrimSpace(l)) {
			return true
		}
	}
	return false
}

func (p *odlParser) Parse(lines <-chan string, emit func(Record)) error {
	var (
		buf  []string
		head string
		seq  int
	)
	isHead := func(l string) bool { return odlDetectRe.MatchString(l) }
	flush := func() {
		if head == "" {
			return
		}
		raw := strings.Join(buf, "\n")
		seq++
		rec := Record{Seq: seq, Format: "odl", Raw: raw, Attrs: map[string]any{}}
		m := odlHeadRe.FindStringSubmatch(head)
		if m != nil {
			rec.TsRaw = m[1]
			dt := ParseDateTime(m[1], "UTC")
			rec.Ts = dt.Ts
			rec.TZOffset = dt.TZOffset
			rec.TZInferred = dt.TZInferred
			rec.Component = m[2]
			rec.Level = normalizeLevel(m[3])
			rec.Attrs["module"] = m[5]
			rec.Attrs["tid"] = m[6]
			rec.Attrs["userId"] = m[7]
			rec.Attrs["ecid"] = m[8]
			rec.Message = strings.TrimSpace(m[9])
		}
		if len(buf) > 1 {
			extra := strings.Join(buf[1:], "\n")
			if rec.Message != "" {
				rec.Message = rec.Message + "\n" + extra
			} else {
				rec.Message = extra
			}
		}
		emit(rec)
		buf = nil
		head = ""
	}
	for l := range lines {
		trimmed := strings.TrimRight(l, "\r")
		if isHead(strings.TrimSpace(trimmed)) {
			flush()
			head = strings.TrimSpace(trimmed)
			buf = append(buf, head)
			continue
		}
		if head == "" {
			if strings.TrimSpace(trimmed) == "" {
				continue
			}
			seq++
			emit(Record{Seq: seq, Format: "odl", Raw: trimmed, Message: strings.TrimSpace(trimmed), Attrs: map[string]any{}})
			continue
		}
		buf = append(buf, trimmed)
	}
	flush()
	return nil
}

// ---- Text (fallback) -------------------------------------------------------

type textParser struct{}

func (p *textParser) Name() string { return "text" }

func (p *textParser) Detect(sample []string) bool { return false } // fallback, никогда не детектится сам

func (p *textParser) Parse(lines <-chan string, emit func(Record)) error {
	seq := 0
	for l := range lines {
		trimmed := strings.TrimRight(l, "\r")
		if strings.TrimSpace(trimmed) == "" {
			continue // пустые строки пропускаем
		}
		seq++
		rec := Record{Seq: seq, Format: "text", Raw: trimmed, Attrs: map[string]any{}}
		s := strings.TrimSpace(trimmed)
		rec.Message = s
		// Пытаемся вытянуть дату/уровень.
		if s != "" {
			dt := tryExtractDate(s)
			if dt.Ts != nil {
				rec.Ts = dt.Ts
				rec.TsRaw = dt.TsRaw
				rec.TZOffset = dt.TZOffset
				rec.TZInferred = dt.TZInferred
			}
			rec.Level = levelByContent(s)
		}
		emit(rec)
	}
	return nil
}

// tryExtractDate пытается найти дату в произвольной текстовой строке.
func tryExtractDate(s string) DateTimeResult {
	// Пробуем все layout-ы: ищем подстроку, похожую на дату.
	dt := ParseDateTime(s, "UTC")
	if dt.Ts != nil {
		return dt
	}
	return DateTimeResult{TsRaw: s}
}
