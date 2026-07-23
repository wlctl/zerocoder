// Package parser содержит общий интерфейс парсеров логов и менеджер парсеров
// (built-in + плагины .so). Каждый парсер разбирает один формат лога; Manager
// определяет формат по sample первых строк и запускает соответствующий парсер
// потоково.
//
// Источник спецификации: architect/specs/ingestion.spec.md (parsers).
// Связанная пользовательская история: US-0002.
package parser

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"plugin"
	"sort"
	"strings"
	"time"
)

// Record — одна логическая запись лога (multiline-блок целиком в Raw).
type Record struct {
	Seq        int
	Format     string
	Ts         *time.Time
	TsRaw      string
	TZOffset   string
	TZInferred bool
	Level      string
	Component  string
	Message    string
	Raw        string
	Attrs      map[string]any
}

// Parser — интерфейс парсера одного формата лога.
type Parser interface {
	// Name возвращает имя формата ("oracle","weblogic",...).
	Name() string
	// Detect возвращает true, если sample первых строк соответствует формату.
	Detect(sample []string) bool
	// Parse потоково читает строки и вызывает emit для каждой логической записи.
	Parse(lines <-chan string, emit func(Record)) error
}

// Manager управляет набором парсеров: built-in + плагины .so из LA_PARSERS_DIR.
type Manager struct {
	parsers []Parser // в порядке detection
}

// NewManager создаёт менеджер с built-in парсерами и загружает плагины из dir.
// Ошибка загрузки плагина не фатальна (плагин опционален для MVP).
func NewManager(dir string, logger *log.Logger) *Manager {
	m := &Manager{parsers: builtinParsers()}
	if dir != "" {
		m.loadPlugins(dir, logger)
	}
	// Стабильный порядок detection: oracle, weblogic, wls_stdout, java, access, odl, text.
	sort.SliceStable(m.parsers, func(i, j int) bool {
		return detectionOrder(m.parsers[i].Name()) < detectionOrder(m.parsers[j].Name())
	})
	return m
}

// Names возвращает имена всех загруженных парсеров.
func (m *Manager) Names() []string {
	names := make([]string, 0, len(m.parsers))
	for _, p := range m.parsers {
		names = append(names, p.Name())
	}
	return names
}

// Detect определяет формат по sample первых непустых строк. Возвращает парсер
// или nil, если ни один не подошёл (fallback text всегда есть).
func (m *Manager) Detect(sample []string) Parser {
	for _, p := range m.parsers {
		if p.Detect(sample) {
			return p
		}
	}
	return nil
}

// ParserByName возвращает парсер по имени формата или nil.
func (m *Manager) ParserByName(name string) Parser {
	for _, p := range m.parsers {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// loadPlugins сканирует каталог dir на .so, открывает каждый через plugin.Open
// и ищет символ "New" func() Parser. Ошибки логирует, но не прерывает работу.
func (m *Manager) loadPlugins(dir string, logger *log.Logger) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if logger != nil {
			logger.Printf("parser plugins: dir %s недоступен: %v", dir, err)
		}
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".so") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		p, err := plugin.Open(path)
		if err != nil {
			if logger != nil {
				logger.Printf("parser plugin %s: open: %v", path, err)
			}
			continue
		}
		sym, err := p.Lookup("New")
		if err != nil {
			if logger != nil {
				logger.Printf("parser plugin %s: lookup New: %v", path, err)
			}
			continue
		}
		newFn, ok := sym.(func() Parser)
		if !ok {
			if logger != nil {
				logger.Printf("parser plugin %s: New имеет неверную сигнатуру", path)
			}
			continue
		}
		func() {
			defer func() { _ = recover() }()
			np := newFn()
			// Override: плагин с тем же Name() заменяет built-in (модификация
			// парсера — пересборка только .so + рестарт, без пересборки бинарника).
			name := np.Name()
			filtered := m.parsers[:0]
			for _, p := range m.parsers {
				if p.Name() != name {
					filtered = append(filtered, p)
				}
			}
			m.parsers = append(filtered, np)
			if logger != nil {
				logger.Printf("parser plugin loaded: %s (name=%s)", path, name)
			}
		}()
	}
}

// detectionOrder возвращает приоритет формата для упорядочения detection.
func detectionOrder(name string) int {
	switch name {
	case "oracle":
		return 0
	case "weblogic":
		return 1
	case "wls_stdout":
		return 2
	case "java":
		return 3
	case "access":
		return 4
	case "odl":
		return 5
	case "text":
		return 100
	default:
		return 50
	}
}

// FirstNonEmptyLines возвращает до n первых непустых строк из lines.
func FirstNonEmptyLines(lines []string, n int) []string {
	out := make([]string, 0, n)
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, l)
		if len(out) >= n {
			break
		}
	}
	return out
}

// formatErr оборачивает ошибку парсера именем формата.
func formatErr(format string, err error) error {
	return fmt.Errorf("parser %s: %w", format, err)
}
