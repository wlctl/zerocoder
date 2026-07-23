// Package db открывает подключение к БД по SOURCE_DB_URL и применяет миграции.
//
// MVP поддерживает scheme "sqlite:<path>" (драйвер modernc.org/sqlite, чистый Go,
// без CGo). Файл БД создаётся автоматически при отсутствии. Scheme "postgres://..."
// зарезервирована на будущее (отдельная ЮС) и пока возвращает ошибку.
//
// Источник спецификации: architect/specs/config.spec.md (spec_version 0.1.0).
// Связанная пользовательская история: US-0001.
package db

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"modernc.org/sqlite" // чистый Go SQLite-драйвер (импортируется именованно — нужен sqlite.RegisterScalarFunction)
)

// DB обёртка над *sql.DB для LogAnalyzer.
type DB struct {
	*sql.DB
}

// regexpOnce гарантирует однократную регистрацию SQL-функции REGEXP на процесс.
// modernc.org/sqlite.RegisterScalarFunction возвращает «already registered» при
// повторном вызове (sqlite.go:599), а тесты открывают много БД на процесс
// (newAPIServer → db.Open каждый тест) — без sync.Once второй тест упал бы.
var regexpOnce sync.Once

// regexpCache кэширует скомпилированные паттерны (процесс-глобально, чистый кеш).
var regexpCache sync.Map // map[string]*regexp.Regexp

// registerSQLiteFunctions регистрирует пользовательские SQL-функции драйвера
// modernc.org/sqlite. Регистрация applies ко всем новым соединениям, открытым
// после вызова (документировано драйвером). Идемпотентна через sync.Once.
func registerSQLiteFunctions() error {
	var regErr error
	regexpOnce.Do(func() {
		regErr = sqlite.RegisterScalarFunction("REGEXP", 2,
			func(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
				_ = ctx
				if len(args) != 2 {
					return nil, fmt.Errorf("REGEXP ожидает 2 аргумента")
				}
				pattern, _ := args[0].(string)
				text, _ := args[1].(string)
				if text == "" {
					return false, nil
				}
				re, err := cachedRegexp(pattern)
				if err != nil {
					return nil, err
				}
				return re.MatchString(text), nil
			})
	})
	return regErr
}

// cachedRegexp возвращает скомпилированный паттерн из кеша (или компилирует).
func cachedRegexp(pattern string) (*regexp.Regexp, error) {
	if v, ok := regexpCache.Load(pattern); ok {
		if re, _ := v.(*regexp.Regexp); re != nil {
			return re, nil
		}
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("REGEXP %q: %w", pattern, err)
	}
	regexpCache.Store(pattern, re)
	return re, nil
}

// Open открывает БД по sourceDBURL. Поддерживается "sqlite:<path>".
// Файл создаётся автоматически. Postgres пока не поддерживается.
func Open(sourceDBURL string) (*DB, error) {
	switch {
	case strings.HasPrefix(sourceDBURL, "sqlite:"):
		path := strings.TrimPrefix(sourceDBURL, "sqlite:")
		return openSQLite(path)
	case strings.HasPrefix(sourceDBURL, "postgres://"), strings.HasPrefix(sourceDBURL, "postgresql://"):
		return nil, fmt.Errorf("postgres не поддерживается в этой версии (SOURCE_DB_URL=%s); используйте sqlite:", sourceDBURL)
	default:
		return nil, fmt.Errorf("неизвестный scheme в SOURCE_DB_URL=%q (ожидается sqlite:<path>)", sourceDBURL)
	}
}

// openSQLite открывает/создаёт SQLite-файл и выставляет прагмы.
//
// Прагмы передаются через DSN (_pragma=...) драйвера modernc.org/sqlite — они
// применяются к КАЖДОМУ новому соединению пула database/sql. Это критично для
// PRAGMA foreign_keys=ON: SQLite по умолчанию держит foreign_keys=OFF на каждом
// соединении, а database/sql открывает пул соединений лениво. Ранний вариант
// (PRAGMA через db.Exec) применял FK ON только к ОДНОМУ соединению пула; при
// реальной UI-нагрузке DELETE попадал на соединение без pragma → CASCADE не
// срабатывал → t_files_analyze/t_log_entries не удалялись → агрегаты (getStats)
// не пересчитывались после удаления загрузки. DSN-прагмы закрывают этот баг.
func openSQLite(path string) (*DB, error) {
	// journal_mode=WAL сохраняется в файле БД (один раз), но DSN-форма безвредна
	// при повторе; foreign_keys и busy_timeout — per-connection, обязаны быть в DSN.
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	// Регистрируем SQL-функцию REGEXP (sync.Once — один раз на процесс).
	// Должно выполняться до первого QueryContext, чтобы функция была доступна
	// соединениям пула (US-0006 regex-поиск).
	if err := registerSQLiteFunctions(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("register REGEXP: %w", err)
	}
	if err := sdb.Ping(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("ping sqlite %s: %w", path, err)
	}
	return &DB{DB: sdb}, nil
}
