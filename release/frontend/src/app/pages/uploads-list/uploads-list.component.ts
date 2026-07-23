import { Component, OnInit, inject, signal, computed } from '@angular/core';
import { CommonModule } from '@angular/common';
import { Router } from '@angular/router';

import { ApiService } from '../../services/api.service';
import { Stats, Upload, UploadResultItem } from '../../models';

type SortKey = 'filename' | 'size' | 'uploaded_at' | 'status';

@Component({
  selector: 'app-uploads-list',
  imports: [CommonModule],
  templateUrl: './uploads-list.component.html',
  styleUrl: './uploads-list.component.scss',
})
export class UploadsListComponent implements OnInit {
  private api = inject(ApiService);
  private router = inject(Router);

  readonly uploads = signal<Upload[]>([]);
  readonly stats = signal<Stats | null>(null);
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);
  readonly sortKey = signal<SortKey>('uploaded_at');
  readonly sortDir = signal<'asc' | 'desc'>('desc');
  readonly filterText = signal('');

  // ---- загрузка файлов (multi-file, drag&drop) ----
  readonly selectedFiles = signal<File[]>([]);
  readonly uploading = signal(false);
  readonly uploadError = signal<string | null>(null);
  readonly uploadResults = signal<UploadResultItem[] | null>(null);
  readonly dragOver = signal(false);

  onFilesPicked(files: FileList | null): void {
    if (!files || files.length === 0) return;
    this.uploadError.set(null);
    this.uploadResults.set(null);
    const incoming = Array.from(files);
    this.selectedFiles.update((cur) => {
      const seen = new Set(cur.map((f) => f.name));
      return [...cur, ...incoming.filter((f) => !seen.has(f.name))];
    });
  }

  onDrop(ev: DragEvent): void {
    ev.preventDefault();
    this.dragOver.set(false);
    if (ev.dataTransfer?.files?.length) {
      this.onFilesPicked(ev.dataTransfer.files);
    }
  }

  onDragOver(ev: DragEvent): void {
    ev.preventDefault();
    this.dragOver.set(true);
  }

  onDragLeave(ev: DragEvent): void {
    ev.preventDefault();
    this.dragOver.set(false);
  }

  removeFile(i: number): void {
    this.selectedFiles.update((cur) => cur.filter((_, idx) => idx !== i));
  }

  clearSelection(): void {
    this.selectedFiles.set([]);
    this.uploadResults.set(null);
    this.uploadError.set(null);
  }

  doUpload(): void {
    const files = this.selectedFiles();
    if (files.length === 0) return;
    this.uploading.set(true);
    this.uploadError.set(null);
    this.uploadResults.set(null);
    this.api.uploadFiles(files).subscribe({
      next: (results) => {
        this.uploading.set(false);
        this.uploadResults.set(results);
        this.selectedFiles.set([]);
        this.reload();
      },
      error: (e) => {
        this.uploading.set(false);
        this.uploadError.set(e.message);
      },
    });
  }

  resultClass(r: UploadResultItem): string {
    switch (r.status) {
      case 'parsed':
        return 'upload-res upload-res--ok';
      case 'duplicate':
        return 'upload-res upload-res--dup';
      default:
        return 'upload-res upload-res--err';
    }
  }

  resultText(r: UploadResultItem): string {
    if (r.status === 'duplicate') {
      return `уже загружен ранее${r.existing_upload_id ? ' (id: ' + r.existing_upload_id.slice(0, 8) + '…)' : ''}`;
    }
    if (r.status === 'failed') {
      return r.error ?? 'ошибка обработки';
    }
    const n = r.files?.length ?? 0;
    return n ? `ok, файлов: ${n}` : 'ok';
  }

  readonly filtered = computed(() => {
    const list = this.uploads();
    const f = this.filterText().trim().toLowerCase();
    let out = f ? list.filter((u) => u.filename.toLowerCase().includes(f)) : [...list];
    const k = this.sortKey();
    const dir = this.sortDir() === 'asc' ? 1 : -1;
    out.sort((a, b) => {
      let av: string | number = '';
      let bv: string | number = '';
      if (k === 'size') {
        av = a.size;
        bv = b.size;
      } else if (k === 'uploaded_at') {
        av = a.uploaded_at;
        bv = b.uploaded_at;
      } else if (k === 'status') {
        av = a.status;
        bv = b.status;
      } else {
        av = a.filename;
        bv = b.filename;
      }
      if (av < bv) return -1 * dir;
      if (av > bv) return 1 * dir;
      return 0;
    });
    return out;
  });

  ngOnInit(): void {
    this.reload();
  }

  reload(): void {
    this.loading.set(true);
    this.error.set(null);
    this.api.listUploads().subscribe({
      next: (list) => this.uploads.set(list ?? []),
      error: (e) => this.error.set(e.message),
    });
    this.api.getStats().subscribe({
      next: (s) => this.stats.set(s),
      error: () => this.stats.set(null),
    });
    this.loading.set(false);
  }

  setSort(k: SortKey): void {
    if (this.sortKey() === k) {
      this.sortDir.set(this.sortDir() === 'asc' ? 'desc' : 'asc');
    } else {
      this.sortKey.set(k);
      this.sortDir.set('desc');
    }
  }

  summaryText(u: Upload): string {
    if (u.kind === 'zip') return `архив: ${u.file_count ?? 0} файл(ов)`;
    if (!u.summary) return '—';
    const lc = u.summary.level_counts as Record<string, number> | undefined;
    if (lc) {
      return Object.entries(lc)
        .map(([k2, v]) => `${k2}:${v}`)
        .join(' ');
    }
    return 'сводка';
  }

  openViewer(u: Upload): void {
    this.router.navigate(['/viewer', u.id]);
  }

  deleteUpload(u: Upload, ev: Event): void {
    ev.stopPropagation();
    if (!confirm(`Удалить загрузку «${u.filename}» и все результаты обработки?`)) return;
    this.api.deleteUpload(u.id).subscribe({
      next: () => this.reload(),
      error: (e) => this.error.set(e.message),
    });
  }

  fmtSize(n: number): string {
    if (!n) return '0 B';
    const u = ['B', 'KB', 'MB', 'GB'];
    let i = 0;
    let v = n;
    while (v >= 1024 && i < u.length - 1) {
      v /= 1024;
      i++;
    }
    return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${u[i]}`;
  }

  fmtNum(n: number | undefined): string {
    return (n ?? 0).toLocaleString('ru-RU');
  }
}