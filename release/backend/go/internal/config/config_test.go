package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConf пишет тестовый la.conf с заданным содержимым.
func writeConf(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, DefaultConfFilename)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	return p
}

func TestParseFullConfig(t *testing.T) {
	dir := t.TempDir()
	p := writeConf(t, dir, "SOURCE_DB_URL=sqlite:/tmp/test.db\nLISTEN_ADDRESS=0.0.0.0\nLISTEN_PORT=9999\n")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.SourceDBURL != "sqlite:/tmp/test.db" {
		t.Errorf("SourceDBURL = %q", c.SourceDBURL)
	}
	if c.ListenAddress != "0.0.0.0" {
		t.Errorf("ListenAddress = %q", c.ListenAddress)
	}
	if c.ListenPort != 9999 {
		t.Errorf("ListenPort = %d", c.ListenPort)
	}
	if got := c.ListenAddr(); got != "0.0.0.0:9999" {
		t.Errorf("ListenAddr = %q", got)
	}
}

func TestDefaultsWhenFieldsMissing(t *testing.T) {
	dir := t.TempDir()
	// только комментарий + пустые строки
	p := writeConf(t, dir, "# только комментарий\n\n")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.SourceDBURL != DefaultSourceDBURL {
		t.Errorf("SourceDBURL default = %q, want %q", c.SourceDBURL, DefaultSourceDBURL)
	}
	if c.ListenAddress != DefaultListenAddr {
		t.Errorf("ListenAddress default = %q", c.ListenAddress)
	}
	if c.ListenPort != DefaultListenPort {
		t.Errorf("ListenPort default = %d, want %d", c.ListenPort, DefaultListenPort)
	}
}

func TestDefaultsWhenFieldsEmpty(t *testing.T) {
	dir := t.TempDir()
	p := writeConf(t, dir, "SOURCE_DB_URL=\nLISTEN_ADDRESS=\nLISTEN_PORT=\n")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.SourceDBURL != DefaultSourceDBURL {
		t.Errorf("empty SourceDBURL = %q", c.SourceDBURL)
	}
	if c.ListenAddress != DefaultListenAddr {
		t.Errorf("empty ListenAddress = %q", c.ListenAddress)
	}
	if c.ListenPort != DefaultListenPort {
		t.Errorf("empty ListenPort = %d", c.ListenPort)
	}
}

func TestInvalidPortFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	p := writeConf(t, dir, "LISTEN_PORT=notanumber\n")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.ListenPort != DefaultListenPort {
		t.Errorf("invalid port = %d, want default %d", c.ListenPort, DefaultListenPort)
	}
}

func TestCreateFromTemplateWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, DefaultConfFilename)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("конфиг уже существует до Load")
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// файл создан из шаблона
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read created conf: %v", err)
	}
	if !strings.Contains(string(b), "SOURCE_DB_URL=sqlite:la.db") {
		t.Errorf("созданный конфиг не содержит дефолт SOURCE_DB_URL: %s", b)
	}
	// значения соответствуют дефолтам шаблона
	if c.SourceDBURL != "sqlite:la.db" || c.ListenAddress != "localhost" || c.ListenPort != 8888 {
		t.Errorf("config from template = %+v", c)
	}
}

func TestEnsureFromTemplateDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, DefaultConfFilename)
	if err := os.WriteFile(p, []byte("SOURCE_DB_URL=sqlite:custom.db\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := EnsureFromTemplate(p); err != nil {
		t.Fatalf("EnsureFromTemplate: %v", err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "custom.db") {
		t.Errorf("существующий конфиг перезаписан шаблоном: %s", b)
	}
}

func TestConfPathRespectsEnv(t *testing.T) {
	t.Setenv("LA_CONF", "/custom/path/la.conf")
	if got := ConfPath("la.conf"); got != "/custom/path/la.conf" {
		t.Errorf("ConfPath = %q, want env override", got)
	}
}

func TestConfPathDefault(t *testing.T) {
	t.Setenv("LA_CONF", "")
	if got := ConfPath(""); got != DefaultConfFilename {
		t.Errorf("ConfPath = %q, want %q", got, DefaultConfFilename)
	}
}

func TestNewFieldsDefaults(t *testing.T) {
	dir := t.TempDir()
	p := writeConf(t, dir, "# только комментарий\n\n")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.MaxFileSize != DefaultMaxFileSize {
		t.Errorf("MaxFileSize default = %d, want %d", c.MaxFileSize, DefaultMaxFileSize)
	}
	if c.MaxFileCount != DefaultMaxFileCount {
		t.Errorf("MaxFileCount default = %d, want %d", c.MaxFileCount, DefaultMaxFileCount)
	}
	if c.DefaultTZ != DefaultDefaultTZ {
		t.Errorf("DefaultTZ default = %q, want %q", c.DefaultTZ, DefaultDefaultTZ)
	}
	if c.ParsersDir != DefaultParsersDir {
		t.Errorf("ParsersDir default = %q, want %q", c.ParsersDir, DefaultParsersDir)
	}
	if c.PostprocessorsDir != DefaultPostprocessorsDir {
		t.Errorf("PostprocessorsDir default = %q, want %q", c.PostprocessorsDir, DefaultPostprocessorsDir)
	}
}

func TestParseFileSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"10GB", 10_000_000_000},
		{"512MB", 512_000_000},
		{"1GiB", 1 << 30},
		{"1024", 1024},
		{"5KB", 5000},
	}
	for _, tc := range cases {
		got, err := parseFileSize(tc.in)
		if err != nil {
			t.Errorf("parseFileSize(%q) err: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseFileSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestNewFieldsFromConfig(t *testing.T) {
	dir := t.TempDir()
	p := writeConf(t, dir, "MAX_FILE_SIZE=512MB\nMAX_FILE_COUNT=5\nLA_DEFAULT_TZ=Europe/Moscow\nLA_PARSERS_DIR=/opt/parsers\nLA_POSTPROCESSORS_DIR=/opt/pp\n")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.MaxFileSize != 512_000_000 {
		t.Errorf("MaxFileSize = %d", c.MaxFileSize)
	}
	if c.MaxFileCount != 5 {
		t.Errorf("MaxFileCount = %d", c.MaxFileCount)
	}
	if c.DefaultTZ != "Europe/Moscow" {
		t.Errorf("DefaultTZ = %q", c.DefaultTZ)
	}
	if c.ParsersDir != "/opt/parsers" {
		t.Errorf("ParsersDir = %q", c.ParsersDir)
	}
	if c.PostprocessorsDir != "/opt/pp" {
		t.Errorf("PostprocessorsDir = %q", c.PostprocessorsDir)
	}
}

func TestTemplateContainsNewFields(t *testing.T) {
	tpl := TemplateText()
	for _, key := range []string{"MAX_FILE_SIZE", "MAX_FILE_COUNT", "LA_DEFAULT_TZ", "LA_PARSERS_DIR", "LA_POSTPROCESSORS_DIR", "LA_FRONTEND_DIST"} {
		if !strings.Contains(tpl, key) {
			t.Errorf("шаблон не содержит %s", key)
		}
	}
}

func TestEnvOverridesConfig(t *testing.T) {
	dir := t.TempDir()
	p := writeConf(t, dir, "SOURCE_DB_URL=sqlite:fromconf.db\nLISTEN_ADDRESS=localhost\nLISTEN_PORT=8888\nMAX_FILE_COUNT=10\nLA_PARSERS_DIR=./parsers\n")
	// env перекрывает значения из файла.
	t.Setenv("SOURCE_DB_URL", "sqlite:fromenv.db")
	t.Setenv("LISTEN_ADDRESS", "0.0.0.0")
	t.Setenv("LISTEN_PORT", "9999")
	t.Setenv("MAX_FILE_COUNT", "42")
	t.Setenv("LA_PARSERS_DIR", "/env/parsers")
	t.Setenv("LA_FRONTEND_DIST", "/env/dist")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.SourceDBURL != "sqlite:fromenv.db" {
		t.Errorf("env SourceDBURL = %q, want fromenv", c.SourceDBURL)
	}
	if c.ListenAddress != "0.0.0.0" {
		t.Errorf("env ListenAddress = %q", c.ListenAddress)
	}
	if c.ListenPort != 9999 {
		t.Errorf("env ListenPort = %d, want 9999", c.ListenPort)
	}
	if c.MaxFileCount != 42 {
		t.Errorf("env MaxFileCount = %d, want 42", c.MaxFileCount)
	}
	if c.ParsersDir != "/env/parsers" {
		t.Errorf("env ParsersDir = %q", c.ParsersDir)
	}
	if c.FrontendDist != "/env/dist" {
		t.Errorf("env FrontendDist = %q", c.FrontendDist)
	}
}

func TestEnvInvalidPortIgnored(t *testing.T) {
	dir := t.TempDir()
	p := writeConf(t, dir, "LISTEN_PORT=8888\n")
	t.Setenv("LISTEN_PORT", "notanumber")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.ListenPort != 8888 {
		t.Errorf("invalid env port should keep conf value, got %d", c.ListenPort)
	}
}
