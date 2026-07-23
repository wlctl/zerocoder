// Package config загружает конфигурацию backend LogAnalyzer из файла la.conf
// (формат KEY=VALUE). При отсутствии файла конфиг создаётся из встроенного
// шаблона la.conf.template. Пустые/отсутствующие поля принимают значения
// по умолчанию.
//
// Источник спецификации: architect/specs/config.spec.md (spec_version 0.1.0),
// расширения — architect/specs/ingestion.spec.md (config_additions).
// Связанные пользовательские истории: US-0001, US-0002.
package config

import (
	"bufio"
	_ "embed"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
)

//go:embed la.conf.template
var templateText string

// Defaults — значения по умолчанию для полей конфигурации.
const (
	DefaultSourceDBURL             = "sqlite:la.db"
	DefaultListenAddr              = "localhost"
	DefaultListenPort              = 8888
	DefaultConfFilename            = "la.conf"
	DefaultMaxFileSize       int64 = 10 * 1024 * 1024 * 1024 // 10GB
	DefaultMaxFileCount      int   = 10
	DefaultDefaultTZ               = "UTC"
	DefaultParsersDir              = "./parsers"
	DefaultPostprocessorsDir       = "./postprocessors"
	DefaultFrontendDist            = "release/frontend/dist/la-frontend/browser"
)

// Config — конфигурация backend.
type Config struct {
	SourceDBURL       string // SOURCE_DB_URL
	ListenAddress     string // LISTEN_ADDRESS
	ListenPort        int    // LISTEN_PORT
	MaxFileSize       int64  // MAX_FILE_SIZE (байты)
	MaxFileCount      int    // MAX_FILE_COUNT
	DefaultTZ         string // LA_DEFAULT_TZ
	ParsersDir        string // LA_PARSERS_DIR
	PostprocessorsDir string // LA_POSTPROCESSORS_DIR
	FrontendDist      string // LA_FRONTEND_DIST (путь к собранному Angular dist)
}

// ConfPath возвращает путь к la.conf: env LA_CONF, иначе filename в текущем каталоге.
func ConfPath(filename string) string {
	if filename == "" {
		filename = DefaultConfFilename
	}
	if v := os.Getenv("LA_CONF"); v != "" {
		return v
	}
	return filename
}

// Load читает конфиг из path. Если файл отсутствует — создаёт его из шаблона
// (EnsureFromTemplate) и затем читает. Поля со значением "" получают дефолты.
func Load(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := EnsureFromTemplate(path); err != nil {
			return nil, fmt.Errorf("создание конфига из шаблона %s: %w", path, err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("доступ к конфигу %s: %w", path, err)
	}

	kv, err := parseFile(path)
	if err != nil {
		return nil, err
	}
	return applyDefaults(kv), nil
}

// EnsureFromTemplate создаёт файл path из встроенного шаблона la.conf.template.
// Если файл уже существует — не перезаписывает (возвращает nil).
func EnsureFromTemplate(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte(templateText), 0o644)
}

// TemplateText возвращает текст встроенного шаблона (для тестов и инспекции).
func TemplateText() string { return templateText }

// parseFile читает KEY=VALUE, игнорируя пустые строки и комментарии (#).
func parseFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("открытие конфига %s: %w", path, err)
	}
	defer f.Close()

	kv := make(map[string]string)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("конфиг %s: строка без '=': %q", path, line)
		}
		kv[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("чтение конфига %s: %w", path, err)
	}
	return kv, nil
}

// applyDefaults заполняет Config из kv, подставляя дефолты для пустых/отсутствующих полей.
func applyDefaults(kv map[string]string) *Config {
	c := &Config{
		SourceDBURL:       DefaultSourceDBURL,
		ListenAddress:     DefaultListenAddr,
		ListenPort:        DefaultListenPort,
		MaxFileSize:       DefaultMaxFileSize,
		MaxFileCount:      DefaultMaxFileCount,
		DefaultTZ:         DefaultDefaultTZ,
		ParsersDir:        DefaultParsersDir,
		PostprocessorsDir: DefaultPostprocessorsDir,
		FrontendDist:      DefaultFrontendDist,
	}
	if v := strings.TrimSpace(kv["SOURCE_DB_URL"]); v != "" {
		c.SourceDBURL = v
	}
	if v := strings.TrimSpace(kv["LISTEN_ADDRESS"]); v != "" {
		c.ListenAddress = v
	}
	if v := strings.TrimSpace(kv["LISTEN_PORT"]); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 && port < 65536 {
			c.ListenPort = port
		}
	}
	if v := strings.TrimSpace(kv["MAX_FILE_SIZE"]); v != "" {
		if size, err := parseFileSize(v); err == nil && size > 0 {
			c.MaxFileSize = size
		}
	}
	if v := strings.TrimSpace(kv["MAX_FILE_COUNT"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.MaxFileCount = n
		}
	}
	if v := strings.TrimSpace(kv["LA_DEFAULT_TZ"]); v != "" {
		c.DefaultTZ = v
	}
	if v := strings.TrimSpace(kv["LA_PARSERS_DIR"]); v != "" {
		c.ParsersDir = v
	}
	if v := strings.TrimSpace(kv["LA_POSTPROCESSORS_DIR"]); v != "" {
		c.PostprocessorsDir = v
	}
	if v := strings.TrimSpace(kv["LA_FRONTEND_DIST"]); v != "" {
		c.FrontendDist = v
	}
	// env имеет приоритет над la.conf (NFR: env > la.conf).
	applyEnv(c)
	return c
}

// applyEnv перекрывает поля Config значениями из окружения, если они заданы и
// непусты. Лимиты (порт/размер/счёт) проверяются на корректность; некорректное
// значение env игнорируется (остаётся файловое/дефолтное).
func applyEnv(c *Config) {
	if v := strings.TrimSpace(os.Getenv("SOURCE_DB_URL")); v != "" {
		c.SourceDBURL = v
	}
	if v := strings.TrimSpace(os.Getenv("LISTEN_ADDRESS")); v != "" {
		c.ListenAddress = v
	}
	if v := strings.TrimSpace(os.Getenv("LISTEN_PORT")); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 && port < 65536 {
			c.ListenPort = port
		}
	}
	if v := strings.TrimSpace(os.Getenv("MAX_FILE_SIZE")); v != "" {
		if size, err := parseFileSize(v); err == nil && size > 0 {
			c.MaxFileSize = size
		}
	}
	if v := strings.TrimSpace(os.Getenv("MAX_FILE_COUNT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.MaxFileCount = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("LA_DEFAULT_TZ")); v != "" {
		c.DefaultTZ = v
	}
	if v := strings.TrimSpace(os.Getenv("LA_PARSERS_DIR")); v != "" {
		c.ParsersDir = v
	}
	if v := strings.TrimSpace(os.Getenv("LA_POSTPROCESSORS_DIR")); v != "" {
		c.PostprocessorsDir = v
	}
	if v := strings.TrimSpace(os.Getenv("LA_FRONTEND_DIST")); v != "" {
		c.FrontendDist = v
	}
}

// parseFileSize разбирает человекочитаемый размер (10GB, 512MB, 1GiB, 1024) в байты.
// Поддерживает десятичные (KB/MB/GB/TB) и двоичные (KiB/MiB/GiB/TiB) суффиксы, а также
// голое число. Регистр суффикса не важен.
func parseFileSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("пустой размер")
	}
	// humanize.ParseBytes понимает формы вида "10GB", "512 MB", "1.5GiB".
	n, err := humanize.ParseBytes(s)
	if err != nil {
		return 0, fmt.Errorf("неразобранный размер %q: %w", s, err)
	}
	return int64(n), nil
}

// ListenAddr возвращает "address:port" для HTTP-сервера.
func (c *Config) ListenAddr() string { return fmt.Sprintf("%s:%d", c.ListenAddress, c.ListenPort) }
