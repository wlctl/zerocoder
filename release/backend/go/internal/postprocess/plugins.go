// Package postprocess: plugins.go — загрузка плагинов-постобработчиков .so.
package postprocess

import (
	"log"
	"os"
	"path/filepath"
	"plugin"
	"strings"
)

// loadPlugins сканирует каталог dir на .so, открывает через plugin.Open и ищет
// символ "New" func() Postprocessor. Ошибки логирует, не прерывает работу.
func (m *Manager) loadPlugins(dir string, logger *log.Logger) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if logger != nil {
			logger.Printf("postprocess plugins: dir %s недоступен: %v", dir, err)
		}
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".so") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		p, err := plugin.Open(path)
		if err != nil {
			if logger != nil {
				logger.Printf("postprocess plugin %s: open: %v", path, err)
			}
			continue
		}
		sym, err := p.Lookup("New")
		if err != nil {
			if logger != nil {
				logger.Printf("postprocess plugin %s: lookup New: %v", path, err)
			}
			continue
		}
		newFn, ok := sym.(func() Postprocessor)
		if !ok {
			if logger != nil {
				logger.Printf("postprocess plugin %s: New имеет неверную сигнатуру", path)
			}
			continue
		}
		func() {
			defer func() { _ = recover() }()
			pp := newFn()
			m.procs[pp.Name()] = pp
			if logger != nil {
				logger.Printf("postprocess plugin loaded: %s", path)
			}
		}()
	}
}
