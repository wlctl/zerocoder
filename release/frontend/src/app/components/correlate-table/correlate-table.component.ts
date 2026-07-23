import { Component, Input, OnChanges, SimpleChanges, Output, EventEmitter, inject, signal, computed } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';

import { ApiService } from '../../services/api.service';
import { Bucket, CorrelatedEntry, Highlight, HistogramBucketPoint } from '../../models';
import { PerFileChartComponent } from '../per-file-chart/per-file-chart.component';
import { FILE_PALETTE } from '../palette';

@Component({
  selector: 'app-correlate-table',
  imports: [CommonModule, FormsModule, PerFileChartComponent],
  templateUrl: './correlate-table.component.html',
  styleUrl: './correlate-table.component.scss',
})
export class CorrelateTableComponent implements OnChanges {
  private api = inject(ApiService);

  @Input({ required: true }) uploadId!: string;
  /** Выбранные file_analyze_id (порядок задаёт цветовую палитру). */
  @Input({ required: true }) fileIds: string[] = [];
  @Input() highlights: Highlight[] = [];
  @Input() searchQ = '';
  @Input() searchMode: 'text' | 'regex' = 'text'; // US-0006
  @Input() searchAttrs = ''; // US-0006: "k1:v1,k2:v2"
  @Input() from = '';
  @Input() to = '';

  // US-0006: пин записи → аннотация (entry-pin) в режиме корреляции.
  @Output() pinEntry = new EventEmitter<{ file_analyze_id: string; entry_id: number | string; ts: string | null }>();

  readonly items = signal<CorrelatedEntry[]>([]);
  readonly total = signal(0);
  readonly offset = signal(0);
  readonly limit = signal(25);
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);
  readonly bucket = signal<Bucket>('hour');
  readonly histogram = signal<HistogramBucketPoint[]>([]);
  readonly showChart = signal(true);

  // file_analyze_id -> стабильный цвет (по индексу в fileIds).
  private colorMap = signal<Map<string, string>>(new Map());

  readonly pageCount = computed(() => Math.max(1, Math.ceil(this.total() / this.limit())));

  ngOnChanges(changes: SimpleChanges): void {
    const relevant = ['fileIds', 'from', 'to', 'searchQ', 'searchMode', 'searchAttrs', 'uploadId'];
    if (relevant.some((k) => changes[k])) {
      this.colorMap.set(this.buildColorMap(this.fileIds));
      this.offset.set(0);
      this.load();
    }
  }

  private buildColorMap(ids: string[]): Map<string, string> {
    const m = new Map<string, string>();
    ids.forEach((id, i) => m.set(id, FILE_PALETTE[i % FILE_PALETTE.length]));
    return m;
  }

  load(): void {
    this.loading.set(true);
    this.error.set(null);
    this.api
      .correlate(this.uploadId, {
        files: this.fileIds,
        from: this.from || undefined,
        to: this.to || undefined,
        q: this.searchQ || undefined,
        mode: this.searchMode || undefined,
        attrs: this.searchAttrs || undefined,
        limit: this.limit(),
        offset: this.offset(),
      })
      .subscribe({
        next: (p) => {
          this.items.set(p?.items ?? []);
          this.total.set(p?.total ?? 0);
          this.loading.set(false);
        },
        error: (e) => {
          this.error.set(e.message);
          this.loading.set(false);
        },
      });
    this.loadHistogram();
  }

  loadHistogram(): void {
    this.api
      .histogram(this.uploadId, this.bucket(), {
        from: this.from || undefined,
        to: this.to || undefined,
        files: this.fileIds,
      })
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
    this.load();
  }

  next(): void {
    if (this.offset() + this.limit() < this.total()) {
      this.offset.set(this.offset() + this.limit());
      this.load();
    }
  }

  prev(): void {
    if (this.offset() > 0) {
      this.offset.set(Math.max(0, this.offset() - this.limit()));
      this.load();
    }
  }

  fileColor(id: string): string {
    return this.colorMap().get(id) ?? '#9ca3af';
  }

  rowColor(entry: CorrelatedEntry): string | null {
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
}