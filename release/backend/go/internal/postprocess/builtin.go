// Package postprocess: builtin.go — встроенные форматные постобработчики
// (наследники Base). Расширяют базовую сводку сессиями старт-стоп по правилам
// из spec (postprocessors.rules_by_format) и level_counts по категориям.
package postprocess

// builtinPostprocessors возвращает built-in постобработчики: base + форматные.
func builtinPostprocessors() []Postprocessor {
	return []Postprocessor{
		NewBasePostprocessor(),
		NewOraclePostprocessor(),
		NewWeblogicPostprocessor(),
		NewWlsStdoutPostprocessor(),
		NewOdlPostprocessor(),
	}
}

// Экспортированные конструкторы постобработчиков. Используются как built-in, так
// и плагинами .so (postprocessors/<fmt>/main.go: func New() Postprocessor { ... }).
// Плагин с тем же Name() заменяет built-in (override — postprocess.Manager.procs
// хранит по имени, последний выигрывает).

// NewBasePostprocessor возвращает базовый постобработчик (всегда есть в хосте).
func NewBasePostprocessor() Postprocessor { return Base{} }

// NewOraclePostprocessor возвращает постобработчик oracle (сессии старт-стоп).
func NewOraclePostprocessor() Postprocessor { return &oraclePP{} }

// NewWeblogicPostprocessor возвращает постобработчик weblogic.
func NewWeblogicPostprocessor() Postprocessor { return &weblogicPP{} }

// NewWlsStdoutPostprocessor возвращает постобработчик wls_stdout.
func NewWlsStdoutPostprocessor() Postprocessor { return &wlsStdoutPP{} }

// NewOdlPostprocessor возвращает постобработчик odl.
func NewOdlPostprocessor() Postprocessor { return &odlPP{} }

// ---- oracle -----------------------------------------------------------------

type oraclePP struct{ Base }

func (oraclePP) Name() string { return "oracle" }

func (p *oraclePP) Process(id string, entries EntryReader) (Summary, error) {
	s, err := p.Base.Process(id, entries)
	if err != nil {
		return s, err
	}
	s.Sessions = findSessions(entries,
		[]string{"Starting ORACLE instance"},
		[]string{"Shutting down instance", "Instance terminated"})
	return s, nil
}

// ---- weblogic ---------------------------------------------------------------

type weblogicPP struct{ Base }

func (weblogicPP) Name() string { return "weblogic" }

func (p *weblogicPP) Process(id string, entries EntryReader) (Summary, error) {
	s, err := p.Base.Process(id, entries)
	if err != nil {
		return s, err
	}
	s.Sessions = findSessions(entries,
		[]string{"Server state changed to STARTING", "Server state changed to RUNNING"},
		[]string{"Server state changed to SHUTDOWN", "Server state changed to FAILED"})
	return s, nil
}

// ---- wls_stdout (по аналогии с weblogic) ------------------------------------

type wlsStdoutPP struct{ Base }

func (wlsStdoutPP) Name() string { return "wls_stdout" }

func (p *wlsStdoutPP) Process(id string, entries EntryReader) (Summary, error) {
	s, err := p.Base.Process(id, entries)
	if err != nil {
		return s, err
	}
	s.Sessions = findSessions(entries,
		[]string{"Server state changed to STARTING", "Server state changed to RUNNING"},
		[]string{"Server state changed to SHUTDOWN", "Server state changed to FAILED"})
	return s, nil
}

// ---- odl --------------------------------------------------------------------

type odlPP struct{ Base }

func (odlPP) Name() string { return "odl" }

func (p *odlPP) Process(id string, entries EntryReader) (Summary, error) {
	s, err := p.Base.Process(id, entries)
	if err != nil {
		return s, err
	}
	// ODL: start/stop по диагностическим событиям компонента; в MVP — без сессий.
	s.Sessions = nil
	return s, nil
}

// java/access/text — форматные постобработчики не реализованы как наследники:
// sessions=[] (по spec). Base применяется через fallback (PostprocessorFor → base).
