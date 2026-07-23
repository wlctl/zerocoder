// Package parser: datetime.go — общий модуль разбора дат/времени для всех парсеров.
// Многоформатный, мультилокальный (en), TZ-обработка по спеке ingestion.spec.md (datetime).
//
// Источник спецификации: architect/specs/ingestion.spec.md (datetime).
package parser

import (
	"fmt"
	"strings"
	"time"
)

// DateTimeResult — результат разбора даты.
type DateTimeResult struct {
	Ts         *time.Time // UTC, nil если не разобрано
	TsRaw      string     // оригинал даты из лога
	TZOffset   string     // смещение оригинала (+03:00 / -04:00 / Z / "")
	TZInferred bool       // true = смещение взято из defaultTZ
}

// Layout-группы по семантике TZ.
//
// offsetLayouts — содержат явное смещение (−04:00, +0700): time.Parse даёт
// корректный offset, tz_inferred=false.
var offsetLayouts = []string{
	"2006-01-02T15:04:05.999999-07:00", // oracle alert.log (ISO8601 + offset)
	"2006-01-02T15:04:05.999999+07:00",
	"2006-01-02T15:04:05.999-07:00", // odl [ISO8601+frac+offset]
	"2006-01-02T15:04:05-07:00",
	"02/Jan/2006:15:04:05 -0700", // access (apache common) [dd/Mon/YYYY:HH:MM:SS +offset]
}

// noTZLayouts — без TZ-части: время трактуется в defaultTZ, tz_inferred=true.
var noTZLayouts = []string{
	"2006-01-02 15:04:05,000", // java log4j (мс запятой, без TZ)
	"Jan 2, 2006 3:04:05 PM",  // wls_stdout без TZ
}

// abbrTZLayouts — с TZ-аббревиатурой (MST): неоднозначна → defaultTZ, tz_inferred=true.
var abbrTZLayouts = []string{
	"Jan 2, 2006 3:04:05 PM MST",  // weblogic .out (12h, TZ abbr/offset)
	"Jan 2, 2006 15:04:05 MST",    // 24h variant
	"Mon Jan 2 15:04:05 MST 2006", // text-general "Wed Mar 28 16:17:29 GMT-4 2012"
}

// AllLayouts возвращает каталог layout-ов (для инспекции/тестов).
func AllLayouts() []string {
	out := make([]string, 0, len(offsetLayouts)+len(noTZLayouts)+len(abbrTZLayouts))
	out = append(out, offsetLayouts...)
	out = append(out, noTZLayouts...)
	out = append(out, abbrTZLayouts...)
	return out
}

// ParseDateTime пытается разобрать s по каталогу layout-ов.
// defaultTZ — таймзона для логов без явного смещения (LA_DEFAULT_TZ).
// Возвращает DateTimeResult; при неудаче Ts=nil, TsRaw сохраняется.
//
// Логика TZ (см. spec.tz_handling):
//   - явный offset (−04:00, +0700) → UTC, tz_inferred=false
//   - TZ аббревиатура (MSK, EST) / нет TZ → defaultTZ, tz_inferred=true
//   - ни один layout не подошёл → Ts=nil, TsRaw сохраняется
func ParseDateTime(s string, defaultTZ string) DateTimeResult {
	s = strings.TrimSpace(s)
	res := DateTimeResult{TsRaw: s}
	if s == "" {
		return res
	}
	loc := defaultLocation(defaultTZ)

	// 1. Спец-формат nodemanager "YYYY-MM-DD GMT-N HH:MM:SS" (явный offset).
	if t, off, ok := parseNodemanager(s); ok {
		return DateTimeResult{Ts: ptrTime(t.UTC()), TsRaw: s, TZOffset: off, TZInferred: false}
	}

	// 2. Layouts с явным offset → UTC, tz_inferred=false.
	for _, layout := range offsetLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			_, offsetSec := t.Zone()
			return DateTimeResult{Ts: ptrTime(t.UTC()), TsRaw: s, TZOffset: formatOffset(offsetSec), TZInferred: false}
		}
	}
	// 3. Layouts без TZ → defaultTZ, tz_inferred=true.
	for _, layout := range noTZLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return DateTimeResult{Ts: ptrTime(t.UTC()), TsRaw: s, TZOffset: "", TZInferred: true}
		}
	}
	// 4. Layouts с TZ-аббревиатурой → defaultTZ (аббревиатуры не трактуем), tz_inferred=true.
	for _, layout := range abbrTZLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return DateTimeResult{Ts: ptrTime(t.UTC()), TsRaw: s, TZOffset: "", TZInferred: true}
		}
	}
	return res
}

// parseNodemanager разбирает "YYYY-MM-DD GMT-N HH:MM:SS" (явный offset).
// Возвращает (UTC time, offset string, true) при успехе.
func parseNodemanager(s string) (time.Time, string, bool) {
	// Формат: "2016-03-16 GMT-3 19:45:41"
	parts := strings.SplitN(s, " GMT", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[1], "-") && !strings.HasPrefix(parts[1], "+") {
		return time.Time{}, "", false
	}
	rest := parts[1] // "-3 19:45:41"
	offFields := strings.SplitN(rest, " ", 2)
	if len(offFields) != 2 {
		return time.Time{}, "", false
	}
	offHours := offFields[0] // "-3"
	var sign int
	n, err := fmt.Sscanf(offHours, "%d", &sign)
	if err != nil || n != 1 {
		return time.Time{}, "", false
	}
	// "2016-03-16" + "19:45:41"
	datePart := strings.TrimSpace(parts[0])
	timePart := strings.TrimSpace(offFields[1])
	combined := datePart + " " + timePart
	t, err := time.ParseInLocation("2006-01-02 15:04:05", combined, time.UTC)
	if err != nil {
		return time.Time{}, "", false
	}
	t = t.Add(time.Duration(sign) * time.Hour * -1) // привести к UTC: UTC = local + sign*(-sign)
	offStr := fmt.Sprintf("%+03d:00", sign)
	return t, offStr, true
}

// defaultLocation возвращает *time.Location для defaultTZ (fallback UTC).
func defaultLocation(defaultTZ string) *time.Location {
	if defaultTZ == "" {
		return time.UTC
	}
	if loc, err := time.LoadLocation(defaultTZ); err == nil {
		return loc
	}
	return time.UTC
}

// formatOffset возвращает смещение в виде "+03:00"/"-04:00"/"Z".
func formatOffset(offsetSec int) string {
	if offsetSec == 0 {
		return "Z"
	}
	sign := "+"
	if offsetSec < 0 {
		sign = "-"
		offsetSec = -offsetSec
	}
	h := offsetSec / 3600
	m := (offsetSec % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, h, m)
}

// ptrTime возвращает указатель на t.
func ptrTime(t time.Time) *time.Time { return &t }
