import { Component, OnInit, inject, signal, computed, Input, effect } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';

import { ApiService } from '../../services/api.service';
import {
  Annotation,
  Bucket,
  FileAnalyze,
  HistogramByFilePoint,
  Highlight,
  Lexeme,
  LogEntry,
  Preset,
  PresetSnapshot,
  TimelineBounds,
  UploadDetail,
  ViewFilter,
} from '../../models';
import { FileTableComponent } from '../../components/file-table/file-table.component';
import { CorrelateTableComponent } from '../../components/correlate-table/correlate-table.component';
import { StackedChartComponent } from '../../components/stacked-chart/stacked-chart.component';
import { AnnotationPanelComponent } from '../../components/annotation-panel/annotation-panel.component';
import { PresetBarComponent } from '../../components/preset-bar/preset-bar.component';

interface SearchFilterUI {
  id?: string;
  q: string;
  fields: 'all' | 'raw';
  mode?: 'text' | 'regex'; // US-0006
  attrs?: string; // US-0006: "k1:v1,k2:v2"
}

interface TimelineState {
  from: string;
  to: string;
  min: string | null;
  max: string | null;
}

@Component({
  selector: 'app-viewer',
  imports: [
    CommonModule,
    FormsModule,
    FileTableComponent,
    CorrelateTableComponent,
    StackedChartComponent,
    AnnotationPanelComponent,
    PresetBarComponent,
  ],
  templateUrl: './viewer.component.html',
  styleUrl: './viewer.component.scss',
})
export class ViewerComponent implements OnInit {
  private api = inject(ApiService);
  private route = inject(ActivatedRoute);
  private router = inject(Router);

  @Input() id = '';

  readonly upload = signal<UploadDetail | null>(null);
  readonly files = signal<FileAnalyze[]>([]);
  readonly selected = signal<Set<string>>(new Set());
  readonly detached = signal<Set<string>>(new Set()); // files opened in separate windows
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);

  // search filters
  readonly searchFilters = signal<SearchFilterUI[]>([]);
  readonly newSearchQ = signal('');
  readonly newSearchFields = signal<'all' | 'raw'>('all');
  readonly searchResults = signal<LogEntry[]>([]);

  // timeline
  readonly timeline = signal<TimelineState>({ from: '', to: '', min: null, max: null });

  // timeline slider (epoch-ms). from/to пустые = полный диапазон (thumbs на концах).
  readonly minMs = computed(() => {
    const m = this.timeline().min;
    return m ? Date.parse(m) : 0;
  });
  readonly maxMs = computed(() => {
    const m = this.timeline().max;
    return m ? Date.parse(m) : 0;
  });
  readonly rangeSpan = computed(() => Math.max(1, this.maxMs() - this.minMs()));
  readonly fromMs = computed(() => (this.timeline().from ? Date.parse(this.timeline().from) : this.minMs()));
  readonly toMs = computed(() => (this.timeline().to ? Date.parse(this.timeline().to) : this.maxMs()));
  readonly fillLeft = computed(() => ((this.fromMs() - this.minMs()) / this.rangeSpan()) * 100);
  readonly fillWidth = computed(() => ((this.toMs() - this.fromMs()) / this.rangeSpan()) * 100);
  // live-подпись во время перетаскивания (коммит — на change)
  readonly dragFromMs = signal<number | null>(null);
  readonly dragToMs = signal<number | null>(null);
  readonly labelFromMs = computed(() => this.dragFromMs() ?? this.fromMs());
  readonly labelToMs = computed(() => this.dragToMs() ?? this.toMs());

  // highlight
  readonly highlights = signal<Highlight[]>([]);
  readonly newHighlightText = signal('');
  readonly newHighlightColor = signal('#ffeb3b');
  readonly lexemes = signal<Lexeme[]>([]);
  readonly selectedLexemes = signal<Set<string>>(new Set());

  // saved filters (raw, for "clear all")
  readonly savedFilters = signal<ViewFilter[]>([]);

  readonly selectedFiles = computed(() =>
    this.files().filter((f) => this.selected().has(f.id) && !this.detached().has(f.id)),
  );

  // Режим корреляции (US-0005): объединённый по ts поток вместо пофайловых таблиц.
  readonly correlateMode = signal(false);
  readonly selectedFileIds = computed(() => this.selectedFiles().map((f) => f.id));

  // US-0006: пресеты, аннотации, regex/attrs-поиск, стекированный по файлам график.
  readonly presets = signal<Preset[]>([]);
  readonly annotations = signal<Annotation[]>([]);
  readonly newSearchMode = signal<'text' | 'regex'>('text');
  readonly newSearchAttrs = signal('');
  readonly fileNames = computed<Record<string, string>>(() => {
    const m: Record<string, string> = {};
    for (const f of this.files()) m[f.id] = f.filename;
    return m;
  });

  // Стекированный по файлам график (только в режиме корреляции).
  readonly histByFile = signal<HistogramByFilePoint[]>([]);
  readonly stackedBucket = signal<Bucket>('hour');

  readonly activeSearchQ = computed(() => {
    // combine all search filters into one OR-ish q for per-file tables (best-effort; spec uses
    // server-side multi-file search separately)
    const qs = this.searchFilters().map((f) => f.q).filter((q) => q.trim().length > 0);
    return qs.join(' ');
  });
  // mode/attrs активного поиска — из первого фильтра, где они заданы (best-effort).
  readonly activeSearchMode = computed(() => this.searchFilters().find((f) => f.mode)?.mode ?? 'text');
  readonly activeSearchAttrs = computed(() => this.searchFilters().find((f) => f.attrs?.trim())?.attrs ?? '');

  constructor() {
    // Перегружаем стекированный график при смене режима/файлов/таймлайна/бакета.
    effect(() => {
      const mode = this.correlateMode();
      const ids = this.selectedFileIds();
      const from = this.timeline().from;
      const to = this.timeline().to;
      const bucket = this.stackedBucket();
      const id = this.upload()?.id;
      if (mode && id) {
        this.loadStacked(id, bucket, { from, to, files: ids });
      } else {
        this.histByFile.set([]);
      }
    });
  }

  ngOnInit(): void {
    this.route.paramMap.subscribe((p) => {
      const id = p.get('id') ?? this.id;
      if (id) this.load(id);
    });
  }

  load(id: string): void {
    this.loading.set(true);
    this.error.set(null);
    this.api.getUpload(id).subscribe({
      next: (u) => {
        this.upload.set(u);
        this.loading.set(false);
        this.loadFiles(id);
        this.loadTimeline(id);
        this.loadSavedState(id);
      },
      error: (e) => {
        this.error.set(e.message);
        this.loading.set(false);
      },
    });
  }

  loadFiles(id: string): void {
    this.api.listFiles(id).subscribe({
      next: (fs) => {
        this.files.set(fs ?? []);
        this.selected.set(new Set((fs ?? []).map((f) => f.id)));
      },
      error: (e) => this.error.set(e.message),
    });
  }

  loadTimeline(id: string): void {
    this.api.timeline(id).subscribe({
      next: (b: TimelineBounds) => {
        this.timeline.set({ from: '', to: '', min: b.min_ts, max: b.max_ts });
      },
      error: () => this.timeline.set({ from: '', to: '', min: null, max: null }),
    });
  }

  loadSavedState(id: string): void {
    this.api.listFilters(id).subscribe({ next: (fs) => this.applySavedFilters(fs), error: () => {} });
    this.api.listHighlights(id).subscribe({ next: (hs) => this.highlights.set(hs ?? []), error: () => {} });
    this.api.lexemes(id).subscribe({ next: (lx) => this.lexemes.set(lx ?? []), error: () => {} });
    this.api.listPresets(id).subscribe({ next: (ps) => this.presets.set(ps ?? []), error: () => {} });
    this.api.listAnnotations(id).subscribe({ next: (an) => this.annotations.set(an ?? []), error: () => {} });
  }

  applySavedFilters(fs: ViewFilter[]): void {
    this.savedFilters.set(fs ?? []);
    const searches: SearchFilterUI[] = [];
    let tlFrom = '';
    let tlTo = '';
    for (const f of fs ?? []) {
      if (f.kind === 'search') {
        const r = f.rule as { q?: string; fields?: 'all' | 'raw'; mode?: 'text' | 'regex'; attrs?: string };
        searches.push({ id: f.id, q: r.q ?? '', fields: r.fields ?? 'all', mode: r.mode, attrs: r.attrs });
      } else if (f.kind === 'timeline') {
        const r = f.rule as { from?: string; to?: string };
        tlFrom = r.from ?? '';
        tlTo = r.to ?? '';
      }
    }
    this.searchFilters.set(searches);
    this.timeline.update((t) => ({ ...t, from: tlFrom, to: tlTo }));
  }

  // ---- file selector ----
  toggleFile(id: string): void {
    this.selected.update((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id); else n.add(id);
      return n;
    });
  }

  selectAll(): void {
    this.selected.set(new Set(this.files().map((f) => f.id)));
  }

  selectNone(): void {
    this.selected.set(new Set());
  }

  // ---- search filters ----
  addSearch(): void {
    const q = this.newSearchQ().trim();
    if (!q) return;
    const mode = this.newSearchMode();
    const attrs = this.newSearchAttrs().trim();
    const ui: SearchFilterUI = { q, fields: this.newSearchFields(), mode, attrs: attrs || undefined };
    this.searchFilters.update((l) => [...l, ui]);
    this.newSearchQ.set('');
    this.newSearchAttrs.set('');
    this.api
      .createFilter(this.uploadId(), { kind: 'search', rule: { q, fields: ui.fields, mode, attrs: attrs || undefined } })
      .subscribe((f) => {
        ui.id = f.id;
      });
    this.runSearch();
  }

  removeSearch(i: number): void {
    const f = this.searchFilters()[i];
    this.searchFilters.update((l) => l.filter((_, idx) => idx !== i));
    if (f.id) this.api.deleteFilter(this.uploadId(), f.id).subscribe();
  }

  runSearch(): void {
    const q = this.activeSearchQ();
    if (!q) {
      this.searchResults.set([]);
      return;
    }
    const files = [...this.selected()];
    this.api
      .search(this.uploadId(), {
        q,
        files,
        fields: this.searchFilters()[0]?.fields ?? 'all',
        mode: this.activeSearchMode(),
        attrs: this.activeSearchAttrs() || undefined,
      })
      .subscribe({ next: (r) => this.searchResults.set(r ?? []), error: () => this.searchResults.set([]) });
  }

  // ---- timeline ----
  setTimelineFrom(v: string): void {
    this.timeline.update((t) => ({ ...t, from: v }));
    this.persistTimeline();
    this.refreshTables();
  }

  setTimelineTo(v: string): void {
    this.timeline.update((t) => ({ ...t, to: v }));
    this.persistTimeline();
    this.refreshTables();
  }

  // slider: live input (без коммита) — обновляет только подпись диапазона.
  onFromInput(ms: number): void {
    const clamped = Math.min(Math.max(ms, this.minMs()), this.toMs());
    this.dragFromMs.set(clamped);
  }

  onToInput(ms: number): void {
    const clamped = Math.max(Math.min(ms, this.maxMs()), this.fromMs());
    this.dragToMs.set(clamped);
  }

  // slider: commit на отпускание (change) — пишем from/to ISO, persist, refresh.
  commitFrom(ms: number): void {
    const clamped = Math.min(Math.max(ms, this.minMs()), this.toMs());
    this.dragFromMs.set(null);
    this.setTimelineFrom(new Date(clamped).toISOString());
  }

  commitTo(ms: number): void {
    const clamped = Math.max(Math.min(ms, this.maxMs()), this.fromMs());
    this.dragToMs.set(null);
    this.setTimelineTo(new Date(clamped).toISOString());
  }

  resetTimeline(): void {
    this.dragFromMs.set(null);
    this.dragToMs.set(null);
    this.setTimelineFrom('');
    this.setTimelineTo('');
  }

  fmtMs(ms: number): string {
    if (!ms) return '—';
    try {
      return new Date(ms).toISOString().replace('T', ' ').slice(0, 19);
    } catch {
      return '—';
    }
  }

  private persistTimeline(): void {
    const t = this.timeline();
    if (!t.from && !t.to) return;
    this.api
      .createFilter(this.uploadId(), { kind: 'timeline', rule: { from: t.from, to: t.to } })
      .subscribe();
  }

  // ---- highlight ----
  addHighlight(): void {
    const text = this.newHighlightText().trim();
    if (!text) return;
    const lexeme = this.selectedLexemes().has(text) ? 1 : 0;
    this.api
      .createHighlight(this.uploadId(), { text, color: this.newHighlightColor(), lexeme })
      .subscribe({
        next: (h) => {
          this.highlights.update((l) => [...l, h]);
          this.newHighlightText.set('');
        },
      });
  }

  addLexemeHighlight(term: string): void {
    this.newHighlightText.set(term);
    this.selectedLexemes.update((s) => new Set(s).add(term));
  }

  removeHighlight(h: Highlight): void {
    this.api.deleteHighlight(this.uploadId(), h.id).subscribe({
      next: () => this.highlights.update((l) => l.filter((x) => x.id !== h.id)),
    });
  }

  // ---- new window ----
  // Хэндлы открытых окон (id -> Window), чтобы основная страница могла закрыть окно
  // и вернуть таблицу («управление от основной страницы», US-0003 ФР-9).
  private windowHandles = new Map<string, Window>();

  openInWindow(id: string): void {
    const existing = this.windowHandles.get(id);
    if (existing && !existing.closed) {
      existing.focus();
      return;
    }
    // Реальный Angular-рендер: открываем SPA-роут /window/:fileId (backend отдаёт
    // index.html + SPA-fallback), в окне грузится FileWindowComponent с таблицей.
    const w = window.open(`/window/${id}`, `la-file-${id}`, 'width=1000,height=700');
    if (!w) {
      // graceful fallback: keep on main page
      alert('Не удалось открыть новое окно (блокировщик?). Таблица остаётся на основной странице.');
      return;
    }
    this.windowHandles.set(id, w);
    this.detached.update((s) => new Set(s).add(id));
    w.addEventListener('beforeunload', () => this.closeWindow(id));
  }

  closeWindow(id: string): void {
    const w = this.windowHandles.get(id);
    if (w) {
      try {
        w.close();
      } catch {
        /* ignore */
      }
      this.windowHandles.delete(id);
    }
    this.detached.update((s) => {
      const n = new Set(s);
      n.delete(id);
      return n;
    });
  }

  // ---- clear all view state ----
  clearAll(): void {
    if (!confirm('Очистить все фильтры и подсветку для этой загрузки?')) return;
    this.api.clearViewState(this.uploadId()).subscribe({
      next: () => {
        this.searchFilters.set([]);
        this.highlights.set([]);
        this.timeline.update((t) => ({ ...t, from: '', to: '' }));
        this.savedFilters.set([]);
      },
      error: (e) => this.error.set(e.message),
    });
  }

  closeTable(id: string): void {
    // closing a file-table unchecks it in selector
    this.selected.update((s) => {
      const n = new Set(s);
      n.delete(id);
      return n;
    });
  }

  refreshTables(): void {
    // trigger child reload via @Input change is automatic through signals in template;
    // FileTableComponent reacts to changed @Input via ngOnChanges-like binding.
  }

  // ---- US-0006: stacked-by-file chart ----
  loadStacked(id: string, bucket: Bucket, opts: { from?: string; to?: string; files?: string[] }): void {
    this.api
      .histogramByFile(id, bucket, opts)
      .subscribe({ next: (h) => this.histByFile.set(h ?? []), error: () => this.histByFile.set([]) });
  }

  onStackedBucket(b: Bucket): void {
    this.stackedBucket.set(b); // effect перегрузит данные
  }

  // ---- US-0006: presets ----
  // preset — снимок на момент сохранения; правки позже не синхронизируются.
  savePreset(name: string): void {
    const snapshot: PresetSnapshot = {
      searchFilters: this.searchFilters().map((f) => ({
        q: f.q,
        fields: f.fields,
        mode: f.mode,
        attrs: f.attrs,
      })),
      timeline: { from: this.timeline().from, to: this.timeline().to },
      highlights: this.highlights().map((h) => ({ text: h.text, color: h.color, lexeme: h.lexeme })),
      selectedFileIds: this.selectedFileIds(),
      correlateMode: this.correlateMode(),
      pageSize: 25,
    };
    this.api.createPreset(this.uploadId(), { name, snapshot }).subscribe({
      next: (p) => this.presets.update((l) => [...l, p]),
      error: (e) => this.error.set(e.message),
    });
  }

  loadPreset(p: Preset): void {
    if (this.highlights().length > 0 || this.searchFilters().length > 0) {
      if (!confirm('Загрузить пресет? Текущие фильтры и подсветка будут заменены снимком пресета.')) return;
    }
    const s = p.snapshot;
    this.searchFilters.set((s.searchFilters ?? []).map((r) => ({ q: r.q, fields: r.fields ?? 'all', mode: r.mode, attrs: r.attrs })));
    this.timeline.update((t) => ({ ...t, from: s.timeline?.from ?? '', to: s.timeline?.to ?? '' }));
    this.highlights.set((s.highlights ?? []).map((h) => ({ id: '', upload_id: this.uploadId(), text: h.text, color: h.color, lexeme: h.lexeme, created_at: '' })));
    const sel = new Set<string>(s.selectedFileIds ?? []);
    this.selected.set(sel);
    this.correlateMode.set(!!s.correlateMode);
    this.runSearch();
  }

  deletePreset(p: Preset): void {
    this.api.deletePreset(this.uploadId(), p.id).subscribe({
      next: () => this.presets.update((l) => l.filter((x) => x.id !== p.id)),
      error: (e) => this.error.set(e.message),
    });
  }

  // ---- US-0006: annotations / pin-points ----
  addAnnotation(payload: { note: string; color: string; ts?: string; file_analyze_id?: string; entry_id?: number }): void {
    this.api
      .createAnnotation(this.uploadId(), payload)
      .subscribe({
        next: (a) => this.annotations.update((l) => [...l, a]),
        error: (e) => this.error.set(e.message),
      });
  }

  removeAnnotation(a: Annotation): void {
    this.api.deleteAnnotation(this.uploadId(), a.id).subscribe({
      next: () => this.annotations.update((l) => l.filter((x) => x.id !== a.id)),
      error: (e) => this.error.set(e.message),
    });
  }

  // entry-pin из кнопки 📌 в строке таблицы (file-table / correlate-table).
  onPinEntry(p: { file_analyze_id: string; entry_id: number | string; ts: string | null }): void {
    const note = prompt('Заметка для пина записи:', `запись #${p.entry_id}`);
    if (note === null) return;
    this.addAnnotation({
      note: note.trim() || `запись #${p.entry_id}`,
      color: '#ef4444',
      file_analyze_id: p.file_analyze_id,
      entry_id: typeof p.entry_id === 'number' ? p.entry_id : Number(p.entry_id),
    });
  }

  back(): void {
    this.router.navigate(['/uploads']);
  }

  private uploadId(): string {
    return this.upload()?.id ?? this.id;
  }

  fmtSize(n: number): string {
    if (!n) return '0 B';
    const u = ['B', 'KB', 'MB', 'GB'];
    let i = 0;
    let v = n;
    while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
    return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${u[i]}`;
  }
}