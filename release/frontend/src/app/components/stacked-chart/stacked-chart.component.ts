import { Component, Input, Output, EventEmitter, computed, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';

import { Annotation, Bucket, HistogramByFilePoint } from '../../models';
import { FILE_PALETTE } from '../palette';

interface StackedBar {
  bucket: string;
  x: number;
  w: number;
  segments: { fileId: string; y: number; h: number; color: string; count: number }[];
  label: string;
}

// StackedChartComponent (US-0006) — стекированный по файлам график.
// Агрегирует плоские {bucket, file_analyze_id, count} клиент-сайд в столбцы по
// бакету, сегменты внутри stacked bottom-up; цвет файла — FILE_PALETTE по индексу
// в fileIds (цикл при >10 файлов, стабильно per id). Time-pin-аннотации —
// вертикальные маркеры на x бакета их ts.
@Component({
  selector: 'app-stacked-chart',
  imports: [CommonModule, FormsModule],
  template: `
    <div class="chart">
      <div class="chart__controls">
        <label>группировка:
          <select [ngModel]="bucket()" (ngModelChange)="bucketChange.emit($event)">
            <option value="month">месяц</option>
            <option value="day">день</option>
            <option value="hour">час</option>
            <option value="minute">минута</option>
          </select>
        </label>
        <span class="chart__count">событий: {{ total() }}</span>
      </div>
      @if (bars().length === 0) {
        <p class="muted small">нет данных гистограммы</p>
      } @else {
        <svg [attr.viewBox]="'0 0 ' + width + ' ' + height" preserveAspectRatio="xMidYMid meet" class="chart__svg">
          <g>
            @for (b of bars(); track b.bucket) {
              @for (seg of b.segments; track seg.fileId) {
                <rect
                  [attr.x]="b.x" [attr.y]="seg.y"
                  [attr.width]="b.w" [attr.height]="seg.h"
                  [attr.fill]="seg.color" rx="1">
                  <title>{{ b.bucket }} · {{ fileName(seg.fileId) }}: {{ seg.count }}</title>
                </rect>
              }
            }
          </g>
          <g class="chart__axis">
            @for (b of bars(); track b.bucket) {
              @if (b.label) {
                <text [attr.x]="b.x + b.w / 2" [attr.y]="height - 2" text-anchor="middle" class="chart__tick">
                  {{ b.label }}
                </text>
              }
            }
          </g>
          <g class="chart__markers">
            @for (m of markers(); track m.aid) {
              <line [attr.x1]="m.x" [attr.y1]="2" [attr.x2]="m.x" [attr.y2]="innerH + 2"
                    [attr.stroke]="m.color" stroke-width="1.5" stroke-dasharray="3 2">
                <title>📌 {{ m.note }}</title>
              </line>
            }
          </g>
        </svg>
        @if (legend().length > 0) {
          <ul class="chart__legend">
            @for (lg of legend(); track lg.fileId) {
              <li><span class="chart__swatch" [style.background]="lg.color"></span>{{ fileName(lg.fileId) }}</li>
            }
          </ul>
        }
      }
    </div>
  `,
  styles: [
    `
      :host { display: block; }
      .chart { border: 1px solid #eee; border-radius: 4px; padding: 0.4rem; background: #fafafa; }
      .chart__controls { display: flex; justify-content: space-between; align-items: center; font-size: 0.8rem; margin-bottom: 0.25rem; }
      .chart__svg { width: 100%; height: 110px; }
      .chart__tick { font-size: 8px; fill: #6b7280; }
      .chart__legend { display: flex; flex-wrap: wrap; gap: 0.5rem; margin: 0.25rem 0 0; padding: 0; list-style: none; font-size: 0.72rem; }
      .chart__swatch { display: inline-block; width: 0.7rem; height: 0.7rem; border-radius: 2px; margin-right: 0.2rem; vertical-align: middle; }
      .muted { color: #6b7280; }
      .small { font-size: 0.8rem; }
    `,
  ],
})
export class StackedChartComponent {
  @Input({ required: true }) set dataInput(v: HistogramByFilePoint[]) {
    this.data.set(v ?? []);
  }
  @Input({ required: true }) set bucketInput(v: Bucket) {
    this.bucket.set(v ?? 'hour');
  }
  @Input() fileIds: string[] = [];
  @Input() fileNames: Record<string, string> = {};
  @Input() set annotationsInput(v: Annotation[] | null) {
    this.annotations.set(v ?? []);
  }
  @Output() bucketChange = new EventEmitter<Bucket>();

  readonly bucket = signal<Bucket>('hour');
  readonly data = signal<HistogramByFilePoint[]>([]);
  readonly annotations = signal<Annotation[]>([]);

  readonly width = 420;
  readonly height = 120;
  readonly innerH = this.height - 18;

  readonly total = computed(() => this.data().reduce((s, p) => s + (p.count ?? 0), 0));

  readonly bars = computed<StackedBar[]>(() => {
    const d = this.data();
    if (d.length === 0) return [];
    const ids = this.fileIds;
    // Уникальные бакеты в порядке первого появления (сортировка по строке — стабильна).
    const bucketSet: string[] = [];
    for (const p of d) {
      if (!bucketSet.includes(p.bucket)) bucketSet.push(p.bucket);
    }
    bucketSet.sort();
    // count[bucket][fileId]
    const byBucket = new Map<string, Map<string, number>>();
    for (const p of d) {
      let m = byBucket.get(p.bucket);
      if (!m) { m = new Map(); byBucket.set(p.bucket, m); }
      m.set(p.file_analyze_id, (m.get(p.file_analyze_id) ?? 0) + (p.count ?? 0));
    }
    const maxStack = Math.max(
      ...bucketSet.map((b) => Array.from((byBucket.get(b) ?? new Map()).values()).reduce((s, c) => s + c, 0)),
      1,
    );
    const pad = 4;
    const innerW = this.width - pad * 2;
    const n = bucketSet.length;
    const bw = Math.max(1, innerW / n - 1);
    return bucketSet.map((b, i) => {
      const m = byBucket.get(b) ?? new Map();
      const segments: StackedBar['segments'] = [];
      let acc = 0; // высота снизу
      for (const fid of ids) {
        const c = m.get(fid) ?? 0;
        if (c <= 0) continue;
        const h = Math.round((c / maxStack) * this.innerH);
        const y = this.innerH - acc - h + 2;
        const idx = ids.indexOf(fid);
        segments.push({ fileId: fid, y, h, color: FILE_PALETTE[idx % FILE_PALETTE.length], count: c });
        acc += h;
      }
      return { bucket: b, x: pad + i * (bw + 1), w: bw, segments, label: n <= 16 ? this.shortLabel(b) : '' };
    });
  });

  readonly legend = computed(() => {
    const present = new Set<string>();
    for (const p of this.data()) present.add(p.file_analyze_id);
    return this.fileIds
      .filter((id) => present.has(id))
      .map((id) => ({ fileId: id, color: FILE_PALETTE[this.fileIds.indexOf(id) % FILE_PALETTE.length] }));
  });

  readonly markers = computed(() => {
    const an = this.annotations();
    const bs = this.bars();
    if (!an || an.length === 0 || bs.length === 0) return [];
    const byBucket = new Map<string, StackedBar>();
    for (const b of bs) byBucket.set(b.bucket, b);
    const out: { aid: string; x: number; color: string; note: string }[] = [];
    for (const a of an) {
      if (!a.ts) continue; // только time-pin
      const lbl = bucketOf(a.ts, this.bucket());
      const bar = byBucket.get(lbl);
      if (!bar) continue;
      out.push({ aid: a.id, x: bar.x + bar.w / 2, color: a.color, note: a.note });
    }
    return out;
  });

  fileName(id: string): string {
    return this.fileNames[id] ?? id.slice(0, 8);
  }

  private shortLabel(b: string): string {
    if (!b) return '';
    const t = b.indexOf('T') >= 0 ? b.slice(b.indexOf('T') + 1, b.indexOf('T') + 6) : b;
    return t.length > 8 ? t.slice(-5) : t;
  }
}

// bucketOf вычисляет метку бакета из ts ISO (зеркало backend bucketFmt).
export function bucketOf(ts: string, bucket: Bucket): string {
  if (!ts) return '';
  switch (bucket) {
    case 'month':
      return ts.slice(0, 7); // YYYY-MM
    case 'day':
      return ts.slice(0, 10); // YYYY-MM-DD
    case 'hour':
      return ts.slice(0, 13); // YYYY-MM-DDTHH
    case 'minute':
      return ts.slice(0, 16); // YYYY-MM-DDTHH:MM
    default:
      return ts.slice(0, 10);
  }
}