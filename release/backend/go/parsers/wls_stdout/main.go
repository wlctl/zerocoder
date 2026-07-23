//go:build plugin

// Плагин-парсер формата wls_stdout (.out, nodemanager). См. parsers/oracle/main.go.
package main

import "github.com/irav/dev-agent/internal/parser"

func New() parser.Parser { return parser.NewWlsStdoutParser() }
