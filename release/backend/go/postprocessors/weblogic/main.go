//go:build plugin

// Плагин-постобработчик формата weblogic. См. postprocessors/oracle/main.go.
package main

import "github.com/irav/dev-agent/internal/postprocess"

func New() postprocess.Postprocessor { return postprocess.NewWeblogicPostprocessor() }
