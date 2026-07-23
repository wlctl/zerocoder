//go:build plugin

// Плагин-парсер формата access (apache common log). См. parsers/oracle/main.go.
package main

import "github.com/irav/dev-agent/internal/parser"

func New() parser.Parser { return parser.NewAccessParser() }
