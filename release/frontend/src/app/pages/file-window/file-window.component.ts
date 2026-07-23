import { Component, Input, OnInit, inject, signal } from '@angular/core';

import { ApiService } from '../../services/api.service';
import { FileAnalyze, Highlight } from '../../models';
import { FileTableComponent } from '../../components/file-table/file-table.component';

// FileWindowComponent — отдельное окно с таблицей одного файла (US-0003 ФР-9:
// «открытие файла в новом окно но с управлением от основной страницы»). Окно
// открывается ViewerComponent.openInWindow через SPA-роут /window/:fileId
// (window.open) — это полноценный Angular-рендер, а не статический плейсхолдер.
// Грузит детали файла (GET /api/files/{id} → upload_id) и подсветку загрузки
// (GET /api/uploads/{id}/highlights), рендерит app-file-table. Закрытие окна
// возвращает таблицу на основную страницу (ViewerComponent.detached).
@Component({
  selector: 'app-file-window',
  imports: [FileTableComponent],
  template: `
    <div class="fw">
      <header class="fw__top">
        <span class="fw__title">{{ file()?.filename ?? 'файл' }}</span>
        <button type="button" class="btn btn--ghost" (click)="closeWindow()">закрыть окно</button>
      </header>
      @if (error()) {
        <div class="alert alert--error">{{ error() }}</div>
      }
      @if (file(); as f) {
        <app-file-table
          [uploadId]="f.upload_id"
          [file]="f"
          [highlights]="highlights()"
          (close)="closeWindow()"
        ></app-file-table>
      } @else if (!error()) {
        <p class="muted">загрузка файла…</p>
      }
    </div>
  `,
  styles: [
    `
      :host { display: block; padding: 1rem 1.25rem; }
      .fw__top { display: flex; align-items: center; gap: 1rem; margin-bottom: 0.5rem; }
      .fw__title { font-weight: 600; flex: 1; word-break: break-all; }
      .muted { color: #6b7280; }
      .alert { padding: 0.5rem 0.7rem; border-radius: 4px; margin-bottom: 0.7rem; }
      .alert--error { background: #fee2e2; color: #991b1b; }
      .btn { padding: 0.35rem 0.7rem; border: 1px solid #d1d5db; background: #fff; border-radius: 4px; cursor: pointer; }
      .btn--ghost { background: transparent; }
    `,
  ],
})
export class FileWindowComponent implements OnInit {
  private api = inject(ApiService);

  @Input() fileId = '';

  readonly file = signal<FileAnalyze | null>(null);
  readonly highlights = signal<Highlight[]>([]);
  readonly error = signal<string | null>(null);

  ngOnInit(): void {
    if (!this.fileId) {
      this.error.set('не указан fileId');
      return;
    }
    this.api.getFile(this.fileId).subscribe({
      next: (f) => {
        this.file.set(f);
        if (f?.upload_id) this.loadHighlights(f.upload_id);
      },
      error: (e) => this.error.set(e.message),
    });
  }

  private loadHighlights(uploadId: string): void {
    this.api.listHighlights(uploadId).subscribe({
      next: (h) => this.highlights.set(h ?? []),
      error: () => {},
    });
  }

  closeWindow(): void {
    // window.close() работает для окон, открытых через window.open() (наш случай).
    try {
      window.close();
    } catch {
      /* ignore */
    }
  }
}