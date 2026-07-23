import { Component, Input, Output, EventEmitter, OnChanges, SimpleChanges, inject, signal, computed } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';

import { ApiService } from '../../services/api.service';
import { Bucket, FileAnalyze, Highlight, HistogramBucketPoint, LogEntry } from '../../models';
import { PerFileChartComponent } from '../per-file-chart/per-file-chart.component';

@Component({
  selector: 'app-file-table',
  imports: [CommonModule, FormsModule, PerFileChartComponent],
  templateUrl: './file-table.component.html',
  styleUrl: './file-table.component.scss',
})
export class FileTableComponent implements OnChanges {
  private api = inject(ApiService);

  @Input({ required: true }) uploadId!: string;
  @Input({ required: true }) file!: FileAnalyze;
  @Input() highlights: Highlight[] = [];
  @Input() searchQ = '';
  @Input() from = '';
  @Input() to = '';

  @Output() close = new EventEmitter<string>();
  @Output() detach = new EventEmitter<string>(); // open in new window
  // US-0006: пин записи → аннотация (entry-pin). Эмитит file_analyze_id + entry_id + ts.
  @Output() pinEntry = new EventEmitter<{ file_analyze_id: string; entry_id: number | string; ts: string | null }>();

  readonly entries = signal<LogEntry[]>([]);
  readonly total = signal(0);
  readonly offset = signal(0);
  readonly limit = signal(10);
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);
  readonly bucket = signal<Bucket>('hour');
  readonly histogram = signal<HistogramBucketPoint[]>([]);
  readonly showChart = signal(true);

  readonly pageCount = computed(() => Math.max(1, Math.ceil(this.total() / this.limit())));

  // Перезагрузка при смене фильтров (from/to/searchQ) или файла. ngOnChanges
  // вызывается и для начальных значений @Input (до ngOnInit), поэтому отдельный
  // ngOnInit не нужен — первичная загрузка тоже идёт отсюда.
  ngOnChanges(changes: SimpleChanges): void {
    const relevant = ['file', 'from', 'to', 'searchQ'];
    if (relevant.some((k) => changes[k])) {
      this.offset.set(0);
      this.loadEntries();
      this.loadHistogram();
    }
  }

  reload(): void {
    this.loadEntries();
    this.loadHistogram();
  }

  loadEntries(): void {
    this.loading.set(true);
    this.error.set(null);
    this.api
      .getEntries(this.file.id, {
        limit: this.limit(),
        offset: this.offset(),
        from: this.from || undefined,
        to: this.to || undefined,
        q: this.searchQ || undefined,
      })
      .subscribe({
        next: (p) => {
          this.entries.set(p?.items ?? []);
          this.total.set(p?.total ?? 0);
          this.loading.set(false);
        },
        error: (e) => {
          this.error.set(e.message);
          this.loading.set(false);
        },
      });
  }

  loadHistogram(): void {
    this.api
      .histogram(this.uploadId, this.bucket(), { from: this.from || undefined, to: this.to || undefined, files: [this.file.id] })
      .subscribe({
        next: (h) => this.histogram.set(h ?? []),
        error: () => this.histogram.set([]),
      });
  }

  onBucket(b: Bucket): void {
    this.bucket.set(b);
    this.loadHistogram();
  }

  setLimit(n: number): void {
    this.limit.set(n);
    this.offset.set(0);
    this.loadEntries();
  }

  next(): void {
    if (this.offset() + this.limit() < this.total()) {
      this.offset.set(this.offset() + this.limit());
      this.loadEntries();
    }
  }

  prev(): void {
    if (this.offset() > 0) {
      this.offset.set(Math.max(0, this.offset() - this.limit()));
      this.loadEntries();
    }
  }

  rowColor(entry: LogEntry): string | null {
    for (const h of this.highlights) {
      const hay = `${entry.message ?? ''} ${entry.raw_line ?? ''} ${entry.component ?? ''} ${entry.level ?? ''}`;
      if (h.text && hay.toLowerCase().includes(h.text.toLowerCase())) return h.color;
    }
    return null;
  }

  highlightText(text: string | null): string {
    if (!text) return '';
    let out = text;
    for (const h of this.highlights) {
      if (!h.text) continue;
      const safe = h.text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      out = out.replace(new RegExp(safe, 'gi'), (m) => `<mark style="background:${h.color}40">${m}</mark>`);
    }
    return out;
  }

  pageEnd(): number {
    return Math.min(this.offset() + this.limit(), this.total());
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