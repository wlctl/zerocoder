// Package parser: encoding.go — определение кодировки файла по первым байтам.
// Использует github.com/saintfish/chardet (pure Go). Файл декодируется в текст
// перед парсингом.
//
// Источник спецификации: architect/specs/ingestion.spec.md (encoding_detection).
package parser

import (
	"io"
	"strings"
	"unicode/utf8"

	"github.com/saintfish/chardet"
)

// DetectEncoding определяет кодировку по первым ~64KB байт. Возвращает
// канонизированное имя (UTF-8 / Windows-1251 / ISO-8859-1 / ...). При ошибке
// или пустом вводе возвращает "UTF-8" (безопасный дефолт).
func DetectEncoding(head []byte) string {
	if len(head) == 0 {
		return "UTF-8"
	}
	// Быстрый путь: валидный UTF-8.
	if isValidUTF8(head) {
		return "UTF-8"
	}
	d := chardet.NewTextDetector()
	res, err := d.DetectBest(head)
	if err != nil || res == nil || res.Charset == "" {
		return "UTF-8"
	}
	return canonicalize(res.Charset)
}

// DecodeToUTF8 декодирует весь контент в UTF-8 строку. Если кодировка UTF-8 —
// возвращает как есть. Иначе применяет соответствующую кодировочную таблицу.
func DecodeToUTF8(content []byte, encoding string) string {
	enc := strings.ToUpper(strings.TrimSpace(encoding))
	if enc == "" || enc == "UTF-8" || enc == "UTF8" {
		return string(content)
	}
	if dec, ok := decoderFor(enc); ok {
		if out, err := dec.Bytes(content); err == nil {
			return string(out)
		}
	}
	// Fallback — отдём как UTF-8 (заменит невалидные байты).
	return string(content)
}

// canonicalize приводит имена chardet к каноничному виду.
func canonicalize(name string) string {
	switch strings.ToUpper(name) {
	case "UTF-8", "UTF8":
		return "UTF-8"
	case "WINDOWS-1251", "CP1251":
		return "Windows-1251"
	case "WINDOWS-1252", "CP1252":
		return "Windows-1252"
	case "ISO-8859-1", "ISO8859-1", "LATIN1":
		return "ISO-8859-1"
	case "ISO-8859-5", "ISO8859-5":
		return "ISO-8859-5"
	default:
		return strings.ToUpper(name)
	}
}

// isValidUTF8 проверяет, что buf — валидная UTF-8 последовательность (хоть один
// не-ASCII символ; чистый ASCII тоже считается UTF-8).
func isValidUTF8(buf []byte) bool {
	return utf8.Valid(buf)
}

// SniffHead читает до max байт из r для определения кодировки.
func SniffHead(r io.Reader, max int) ([]byte, error) {
	if max <= 0 {
		max = 64 * 1024
	}
	head := make([]byte, 0, max)
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			head = append(head, buf[:n]...)
			if len(head) >= max {
				return head[:max], nil
			}
		}
		if err == io.EOF {
			return head, nil
		}
		if err != nil {
			return head, err
		}
	}
}
