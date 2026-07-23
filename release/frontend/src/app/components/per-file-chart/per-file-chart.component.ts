import { Component, Input, Output, EventEmitter, computed, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';

import { Bucket, HistogramBucketPoint } from '../../models';

@Component({
  selector: 'app-per-file-chart',
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
      @if (data().length === 0) {
        <p class="muted small">нет данных гистограммы</p>
      } @else {
        <svg [attr.viewBox]="'0 0 ' + width + ' ' + height" preserveAspectRatio="xMidYMid meet" class="chart__svg">
          <g>
            @for (b of bars(); track b.bucket) {
              <rect
                [attr.x]="b.x" [attr.y]="b.y"
                [attr.width]="b.w" [attr.height]="b.h"
                [attr.fill]="b.color"
                rx="1">
                <title>{{ b.bucket }}: {{ b.count }}</title>
              </rect>
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
        </svg>
      }
    </div>
  `,
  styles: [
    `
      :host { display: block; }
      .chart { border: 1px solid #eee; border-radius: 4px; padding: 0.4rem; background: #fafafa; }
      .chart__controls { display: flex; justify-content: space-between; align-items: center; font-size: 0.8rem; margin-bottom: 0.25rem; }
      .chart__svg { width: 100%; height: 80px; }
      .chart__tick { font-size: 8px; fill: #6b7280; }
      .muted { color: #6b7280; }
      .small { font-size: 0.8rem; }
    `,
  ],
})
export class PerFileChartComponent {
  @Input({ required: true }) set dataInput(v: HistogramBucketPoint[]) {
    this.data.set(v ?? []);
  }
  @Input({ required: true }) set bucketInput(v: Bucket) {
    this.bucket.set(v ?? 'hour');
  }
  @Output() bucketChange = new EventEmitter<Bucket>();

  readonly bucket = signal<Bucket>('hour');

  readonly width = 400;
  readonly height = 90;

  readonly data = signal<HistogramBucketPoint[]>([]);

  readonly total = computed(() => this.data().reduce((s, p) => s + (p.count ?? 0), 0));

  readonly bars = computed(() => {
    const d = this.data();
    if (d.length === 0) return [];
    const max = Math.max(...d.map((p) => p.count ?? 0), 1);
    const n = d.length;
    const pad = 4;
    const innerW = this.width - pad * 2;
    const innerH = this.height - 16;
    const bw = Math.max(1, innerW / n - 1);
    const palette = ['#3b82f6', '#ef4444', '#f59e0b', '#10b981', '#8b5cf6', '#ec4899', '#14b8a6', '#6366f1'];
    return d.map((p, i) => {
      const h = Math.round(((p.count ?? 0) / max) * innerH);
      const x = pad + i * (bw + 1);
      const y = innerH - h + 2;
      const color = palette[i % palette.length];
      const label = n <= 16 ? this.shortLabel(p.bucket) : '';
      return { bucket: p.bucket, count: p.count ?? 0, x, y, w: bw, h, color, label };
    });
  });

  private shortLabel(b: string): string {
    // bucket format from backend unknown; keep last 5 chars or time part.
    if (!b) return '';
    const t = b.indexOf('T') >= 0 ? b.slice(b.indexOf('T') + 1, b.indexOf('T') + 6) : b;
    return t.length > 8 ? t.slice(-5) : t;
  }
}