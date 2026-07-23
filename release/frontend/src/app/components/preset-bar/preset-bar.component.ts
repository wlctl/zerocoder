import { Component, Input, Output, EventEmitter, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';

import { Preset } from '../../models';

// PresetBarComponent (US-0006) — «глупый» бар пресетов.
// Сборка snapshot и восстановление состояния — в родителе (ViewerComponent, где
// живут сигналы). Здесь только UI: выбор пресета, загрузить/удалить, имя + сохранить.
@Component({
  selector: 'app-preset-bar',
  imports: [CommonModule, FormsModule],
  template: `
    <div class="preset-bar">
      <select [ngModel]="selectedId()" (ngModelChange)="selectedId.set($event)">
        <option value="">— пресеты ({{ presets.length }}) —</option>
        @for (p of presets; track p.id) {
          <option [value]="p.id">{{ p.name }}</option>
        }
      </select>
      <button type="button" class="btn btn--sm" [disabled]="!selectedId()" (click)="loadSelected()">загрузить</button>
      <button type="button" class="btn btn--sm btn--danger" [disabled]="!selectedId()" (click)="deleteSelected()">удалить</button>
      <input
        type="text"
        placeholder="имя нового пресета…"
        [ngModel]="name()"
        (ngModelChange)="name.set($event)"
        (keyup.enter)="submitSave()"
      />
      <button type="button" class="btn btn--sm" [disabled]="!name().trim()" (click)="submitSave()">сохранить вид</button>
    </div>
  `,
  styles: [
    `
      :host { display: block; }
      .preset-bar { display: flex; flex-wrap: wrap; gap: 0.4rem; align-items: center; }
    `,
  ],
})
export class PresetBarComponent {
  @Input() presets: Preset[] = [];

  @Output() load = new EventEmitter<Preset>();
  @Output() save = new EventEmitter<string>();
  @Output() delete = new EventEmitter<Preset>();

  readonly selectedId = signal('');
  readonly name = signal('');

  loadSelected(): void {
    const p = this.presets.find((x) => x.id === this.selectedId());
    if (p) this.load.emit(p);
  }

  deleteSelected(): void {
    const p = this.presets.find((x) => x.id === this.selectedId());
    if (p) this.delete.emit(p);
    this.selectedId.set('');
  }

  submitSave(): void {
    const n = this.name().trim();
    if (!n) return;
    this.save.emit(n);
    this.name.set('');
  }
}