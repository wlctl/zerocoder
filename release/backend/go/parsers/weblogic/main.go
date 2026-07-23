//go:build plugin

// Плагин-парсер формата weblogic (.log, ####<...>). См. parsers/oracle/main.go.
package main

import "github.com/irav/dev-agent/internal/parser"

func New() parser.Parser { return parser.NewWeblogicParser() }
