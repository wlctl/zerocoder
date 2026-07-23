//go:build plugin

// Плагин-постобработчик формата oracle. Собирается как Go plugin (.so):
//
//	go build -buildmode=plugin -tags plugin ./postprocessors/oracle
//
// Логика — в internal/postprocess (NewOraclePostprocessor); плагин экспортирует
// New() func() postprocess.Postprocessor. При загрузке (LA_POSTPROCESSORS_DIR)
// заменяет built-in по имени (override). Модификация — пересборка только .so + рестарт.
package main

import "github.com/irav/dev-agent/internal/postprocess"

func New() postprocess.Postprocessor { return postprocess.NewOraclePostprocessor() }
