//go:build plugin

// Плагин-парсер формата oracle (alert.log). Собирается как Go plugin (.so):
//
//	go build -buildmode=plugin -tags plugin ./parsers/oracle
//
// Логика — в internal/parser (NewOracleParser); плагин лишь экспортирует символ
// New() func() parser.Parser. При загрузке (LA_PARSERS_DIR) заменяет built-in
// по имени (override). Модификация — пересборка только этого .so + рестарт.
package main

import "github.com/irav/dev-agent/internal/parser"

func New() parser.Parser { return parser.NewOracleParser() }
