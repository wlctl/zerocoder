//go:build plugin

// Плагин-постобработчик формата wls_stdout. См. postprocessors/oracle/main.go.
package main

import "github.com/irav/dev-agent/internal/postprocess"

func New() postprocess.Postprocessor { return postprocess.NewWlsStdoutPostprocessor() }
