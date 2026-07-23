import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpParams, HttpErrorResponse } from '@angular/common/http';
import { Observable, throwError } from 'rxjs';
import { catchError, map } from 'rxjs/operators';

import {
  Annotation,
  Bucket,
  CorrelatedPage,
  EntriesPage,
  FileAnalyze,
  Highlight,
  HistogramBucketPoint,
  HistogramByFilePoint,
  Lexeme,
  LogEntry,
  ParserInfo,
  Preset,
  PresetSnapshot,
  Stats,
  TimelineBounds,
  Upload,
  UploadDetail,
  UploadResultItem,
  ViewFilter,
} from '../models';

// Base URL is relative: backend serves both API and static frontend from one origin.
const API_BASE = '/api';

function toParams(obj: Record<string, unknown>): HttpParams {
  let params = new HttpParams();
  for (const [k, v] of Object.entries(obj)) {
    if (v === undefined || v === null || v === '') continue;
    if (Array.isArray(v)) {
      for (const item of v) params = params.append(k, String(item));
    } else {
      params = params.set(k, String(v));
    }
  }
  return params;
}

@Injectable({ providedIn: 'root' })
export class ApiService {
  private http = inject(HttpClient);

  // ---- uploads ----
  listUploads(sort?: string, filter?: string): Observable<Upload[]> {
    return this.http
      .get<Upload[]>(`${API_BASE}/uploads`, { params: toParams({ sort, filter }) })
      .pipe(catchError(this.handleError));
  }

  getUpload(id: string): Observable<UploadDetail> {
    return this.http.get<UploadDetail>(`${API_BASE}/uploads/${id}`).pipe(catchError(this.handleError));
  }

  deleteUpload(id: string): Observable<void> {
    return this.http.delete<void>(`${API_BASE}/uploads/${id}`).pipe(catchError(this.handleError));
  }

  // Загрузка одного или нескольких файлов одним multipart-запросом.
  // Каждый файл обрабатывается независимо; ответ содержит per-file результаты.
  uploadFiles(files: File[]): Observable<UploadResultItem[]> {
    const fd = new FormData();
    for (const f of files) {
      fd.append('file', f, f.name);
    }
    return this.http
      .post<{ results: UploadResultItem[] }>(`${API_BASE}/uploads`, fd)
      .pipe(
        map((r) => r?.results ?? []),
        catchError(this.handleError),
      );
  }

  // ---- stats ----
  getStats(): Observable<Stats> {
    return this.http.get<Stats>(`${API_BASE}/stats`).pipe(catchError(this.handleError));
  }

  // ---- files ----
  listFiles(uploadId: string): Observable<FileAnalyze[]> {
    return this.http
      .get<FileAnalyze[]>(`${API_BASE}/files`, { params: toParams({ upload_id: uploadId }) })
      .pipe(catchError(this.handleError));
  }

  getFile(id: string): Observable<FileAnalyze> {
    return this.http.get<FileAnalyze>(`${API_BASE}/files/${id}`).pipe(catchError(this.handleError));
  }

  getEntries(
    fileId: string,
    opts: { limit?: number; offset?: number; level?: string; from?: string; to?: string; q?: string } = {},
  ): Observable<EntriesPage> {
    return this.http
      .get<EntriesPage>(`${API_BASE}/files/${fileId}/entries`, { params: toParams(opts) })
      .pipe(catchError(this.handleError));
  }

  // ---- parsers ----
  listParsers(): Observable<ParserInfo[]> {
    return this.http.get<ParserInfo[]>(`${API_BASE}/parsers`).pipe(catchError(this.handleError));
  }

  // ---- viewer: search / lexemes / histogram / timeline ----
  search(
    uploadId: string,
    opts: {
      q: string;
      files?: string[];
      fields?: 'all' | 'raw';
      mode?: 'text' | 'regex';
      attrs?: string;
      limit?: number;
      offset?: number;
    },
  ): Observable<LogEntry[]> {
    return this.http
      .get<LogEntry[]>(`${API_BASE}/uploads/${uploadId}/search`, { params: toParams(opts) })
      .pipe(catchError(this.handleError));
  }

  lexemes(uploadId: string, files?: string[], limit?: number): Observable<Lexeme[]> {
    return this.http
      .get<Lexeme[]>(`${API_BASE}/uploads/${uploadId}/lexemes`, { params: toParams({ files, limit }) })
      .pipe(catchError(this.handleError));
  }

  histogram(
    uploadId: string,
    bucket: Bucket,
    opts: { from?: string; to?: string; files?: string[] } = {},
  ): Observable<HistogramBucketPoint[]> {
    return this.http
      .get<HistogramBucketPoint[]>(`${API_BASE}/uploads/${uploadId}/histogram`, {
        params: toParams({ bucket, ...opts }),
      })
      .pipe(catchError(this.handleError));
  }

  timeline(uploadId: string): Observable<TimelineBounds> {
    return this.http
      .get<TimelineBounds>(`${API_BASE}/uploads/${uploadId}/timeline`)
      .pipe(catchError(this.handleError));
  }

  // ---- viewer: correlate (US-0005) ----
  // Объединённый кросс-файл поток записей, отсортированный по ts.
  correlate(
    uploadId: string,
    opts: {
      files?: string[];
      from?: string;
      to?: string;
      q?: string;
      mode?: 'text' | 'regex';
      attrs?: string;
      level?: string;
      limit?: number;
      offset?: number;
    } = {},
  ): Observable<CorrelatedPage> {
    return this.http
      .get<CorrelatedPage>(`${API_BASE}/uploads/${uploadId}/correlate`, { params: toParams(opts) })
      .pipe(catchError(this.handleError));
  }

  // ---- viewer: histogram-by-file (US-0006, стекированный по файлам) ----
  histogramByFile(
    uploadId: string,
    bucket: Bucket,
    opts: { from?: string; to?: string; files?: string[] } = {},
  ): Observable<HistogramByFilePoint[]> {
    return this.http
      .get<HistogramByFilePoint[]>(`${API_BASE}/uploads/${uploadId}/histogram-by-file`, {
        params: toParams({ bucket, ...opts }),
      })
      .pipe(catchError(this.handleError));
  }

  // ---- persistence: presets (US-0006) ----
  listPresets(uploadId: string): Observable<Preset[]> {
    return this.http
      .get<Preset[]>(`${API_BASE}/uploads/${uploadId}/presets`)
      .pipe(catchError(this.handleError));
  }

  createPreset(uploadId: string, body: { name: string; snapshot: PresetSnapshot }): Observable<Preset> {
    return this.http
      .post<Preset>(`${API_BASE}/uploads/${uploadId}/presets`, body)
      .pipe(catchError(this.handleError));
  }

  deletePreset(uploadId: string, pid: string): Observable<void> {
    return this.http
      .delete<void>(`${API_BASE}/uploads/${uploadId}/presets/${pid}`)
      .pipe(catchError(this.handleError));
  }

  // ---- persistence: annotations (US-0006) ----
  listAnnotations(uploadId: string): Observable<Annotation[]> {
    return this.http
      .get<Annotation[]>(`${API_BASE}/uploads/${uploadId}/annotations`)
      .pipe(catchError(this.handleError));
  }

  createAnnotation(
    uploadId: string,
    body: {
      file_analyze_id?: string;
      entry_id?: number;
      ts?: string;
      note: string;
      color: string;
    },
  ): Observable<Annotation> {
    return this.http
      .post<Annotation>(`${API_BASE}/uploads/${uploadId}/annotations`, body)
      .pipe(catchError(this.handleError));
  }

  deleteAnnotation(uploadId: string, aid: string): Observable<void> {
    return this.http
      .delete<void>(`${API_BASE}/uploads/${uploadId}/annotations/${aid}`)
      .pipe(catchError(this.handleError));
  }

  // ---- persistence: filters ----
  listFilters(uploadId: string): Observable<ViewFilter[]> {
    return this.http
      .get<ViewFilter[]>(`${API_BASE}/uploads/${uploadId}/filters`)
      .pipe(catchError(this.handleError));
  }

  createFilter(uploadId: string, body: { kind: 'search' | 'timeline'; rule: unknown }): Observable<ViewFilter> {
    return this.http
      .post<ViewFilter>(`${API_BASE}/uploads/${uploadId}/filters`, body)
      .pipe(catchError(this.handleError));
  }

  deleteFilter(uploadId: string, fid: string): Observable<void> {
    return this.http
      .delete<void>(`${API_BASE}/uploads/${uploadId}/filters/${fid}`)
      .pipe(catchError(this.handleError));
  }

  // ---- persistence: highlights ----
  listHighlights(uploadId: string): Observable<Highlight[]> {
    return this.http
      .get<Highlight[]>(`${API_BASE}/uploads/${uploadId}/highlights`)
      .pipe(catchError(this.handleError));
  }

  createHighlight(
    uploadId: string,
    body: { text: string; color: string; lexeme: number },
  ): Observable<Highlight> {
    return this.http
      .post<Highlight>(`${API_BASE}/uploads/${uploadId}/highlights`, body)
      .pipe(catchError(this.handleError));
  }

  deleteHighlight(uploadId: string, hid: string): Observable<void> {
    return this.http
      .delete<void>(`${API_BASE}/uploads/${uploadId}/highlights/${hid}`)
      .pipe(catchError(this.handleError));
  }

  // ---- clear all view state ----
  clearViewState(uploadId: string): Observable<void> {
    return this.http
      .delete<void>(`${API_BASE}/uploads/${uploadId}/view-state`)
      .pipe(catchError(this.handleError));
  }

  private handleError(err: HttpErrorResponse) {
    const msg =
      err.status === 0
        ? 'Network error / backend недоступен'
        : `API ${err.status}: ${err.error instanceof Blob ? '' : (err.error as { message?: string })?.message ?? err.message}`;
    return throwError(() => new Error(msg));
  }
}