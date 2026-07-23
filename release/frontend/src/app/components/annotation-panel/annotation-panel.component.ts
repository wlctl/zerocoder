import { Component, Input, Output, EventEmitter, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';

import { Annotation } from '../../models';

// AnnotationPanelComponent (US-0006) — панель аннотаций-пинов.
// Presentational: список + форма добавления time-pin; entry-pin инициируется
// кнопкой 📌 в строке таблицы (родитель ViewerComponent вызывает createAnnotation).
// @Input annotations/fileNames из родителя (единый источник истины — тот же сигнал
// кормит маркеры stacked-chart); @Output add/remove — родитель персистит через ApiService.
@Component({
  selector: 'app-annotation-panel',
  imports: [CommonModule, FormsModule],
  template: `
    <section class="ann">
      <h3>Аннотации / пин-точки</h3>
      <ul class="ann__list">
        @if (annotations.length === 0) {
          <li class="muted small">нет аннотаций. Time-pin — формой ниже, entry-pin — кнопкой 📌 в строке таблицы.</li>
        }
        @for (a of annotations; track a.id) {
          <li class="ann__item">
            <span class="ann__swatch" [style.background]="a.color"></span>
            <span class="ann__type">
              @if (a.ts) { ⏱ time } @else { 📌 запись }
            </span>
            <span class="ann__target muted small">
              @if (a.ts) { {{ a.ts }} }
              @else if (fileName(a.file_analyze_id) !== '?') { {{ fileName(a.file_analyze_id) }} #{{ a.entry_id }} }
              @else { вне страницы/удалена }
            </span>
            <span class="ann__note">{{ a.note }}</span>
            <button type="button" class="ann__x" (click)="remove.emit(a)" title="удалить">×</button>
          </li>
        }
      </ul>

      <div class="ann__form">
        <input
          type="text"
          placeholder="заметка…"
          [ngModel]="note()"
          (ngModelChange)="note.set($event)"
        />
        <input type="color" [ngModel]="color()" (ngModelChange)="color.set($event)" />
        <label class="ann__toggle">
          <input type="checkbox" [ngModel]="timePin()" (ngModelChange)="timePin.set($event)" />
          <span>time-pin</span>
        </label>
        @if (timePin()) {
          <input
            type="datetime-local"
            [ngModel]="ts()"
            (ngModelChange)="ts.set($event)"
            title="момент time-pin"
          />
        } @else {
          <span class="muted small">entry-pin — через 📌 в таблице</span>
        }
        <button type="button" class="btn btn--sm" (click)="submit()">добавить</button>
      </div>
    </section>
  `,
  styles: [
    `
      :host { display: block; }
      .ann h3 { margin: 0 0 0.4rem; font-size: 0.95rem; }
      .ann__list { list-style: none; margin: 0 0 0.5rem; padding: 0; }
      .ann__item { display: flex; align-items: center; gap: 0.4rem; padding: 0.2rem 0; font-size: 0.82rem; border-bottom: 1px solid #f0f0f0; }
      .ann__swatch { width: 0.7rem; height: 0.7rem; border-radius: 2px; flex: none; }
      .ann__type { font-size: 0.72rem; color: #374151; }
      .ann__target { min-width: 9rem; }
      .ann__note { flex: 1; }
      .ann__x { background: none; border: none; cursor: pointer; color: #b91c1c; font-size: 1rem; }
      .ann__form { display: flex; flex-wrap: wrap; gap: 0.4rem; align-items: center; }
      .ann__toggle { display: inline-flex; align-items: center; gap: 0.25rem; font-size: 0.78rem; }
    `,
  ],
})
export class AnnotationPanelComponent {
  @Input() annotations: Annotation[] = [];
  @Input() fileNames: Record<string, string> = {};

  @Output() add = new EventEmitter<{
    note: string;
    color: string;
    ts?: string;
    file_analyze_id?: string;
    entry_id?: number;
  }>();
  @Output() remove = new EventEmitter<Annotation>();

  readonly note = signal('');
  readonly color = signal('#ef4444');
  readonly timePin = signal(true);
  readonly ts = signal('');

  submit(): void {
    const n = this.note().trim();
    if (!n) return;
    if (this.timePin()) {
      if (!this.ts()) return;
      this.add.emit({ note: n, color: this.color(), ts: this.ts() });
    } else {
      // entry-pin из формы не поддерживается (нужна запись из таблицы)
      return;
    }
    this.note.set('');
    this.ts.set('');
  }

  fileName(id: string | null): string {
    if (!id) return '?';
    return this.fileNames[id] ?? '?';
  }
}