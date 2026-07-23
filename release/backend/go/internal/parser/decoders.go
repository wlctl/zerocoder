// Package parser: decoders.go — кодировочные таблицы для декодирования в UTF-8.
// Использует golang.org/x/text/encoding/charmap для Windows-125x и ISO-8859-x.
package parser

import "golang.org/x/text/encoding/charmap"

// decoderFor возвращает decoder для кодировки по каноничному имени.
func decoderFor(enc string) (decoder, bool) {
	switch enc {
	case "Windows-1251":
		return charmap.Windows1251.NewDecoder(), true
	case "Windows-1252":
		return charmap.Windows1252.NewDecoder(), true
	case "ISO-8859-1":
		return charmap.ISO8859_1.NewDecoder(), true
	case "ISO-8859-5":
		return charmap.ISO8859_5.NewDecoder(), true
	}
	return nil, false
}

// decoder — минимальный интерфейс, реализуемый charmap-декодерами.
type decoder interface {
	Bytes(b []byte) ([]byte, error)
}
