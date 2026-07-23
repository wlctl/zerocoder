package ingest

import (
	"archive/zip"
	"os"
)

// makeTestZip создаёт zip-архив с файлами из map[name->content].
func makeTestZip(path string, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			zw.Close()
			return err
		}
		if _, err := w.Write([]byte(content)); err != nil {
			zw.Close()
			return err
		}
	}
	return zw.Close()
}
