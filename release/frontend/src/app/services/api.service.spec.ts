import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting, HttpTestingController } from '@angular/common/http/testing';

import { ApiService } from './api.service';
import { Stats, Upload } from '../models';

describe('ApiService', () => {
  let service: ApiService;
  let httpMock: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [ApiService, provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(ApiService);
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => httpMock.verify());

  it('listUploads() calls GET /api/uploads', () => {
    const payload: Upload[] = [
      { id: 'u1', filename: 'a.log', kind: 'file', size: 10, uploaded_at: 't', status: 'ok' },
    ];
    let result: Upload[] | undefined;
    service.listUploads().subscribe((r) => (result = r));

    const req = httpMock.expectOne((r) => r.url === '/api/uploads' && r.method === 'GET');
    expect(req.request.method).toBe('GET');
    req.flush(payload);
    expect(result).toEqual(payload);
  });

  it('getStats() calls GET /api/stats', () => {
    const payload: Stats = { storage_size: 100, upload_count: 2, file_count: 3, record_count: 42 };
    let result: Stats | undefined;
    service.getStats().subscribe((r) => (result = r));

    const req = httpMock.expectOne('/api/stats');
    req.flush(payload);
    expect(result).toEqual(payload);
  });

  it('deleteUpload() calls DELETE /api/uploads/{id}', () => {
    service.deleteUpload('u1').subscribe();
    const req = httpMock.expectOne('/api/uploads/u1');
    expect(req.request.method).toBe('DELETE');
    req.flush(null);
  });

  it('getEntries() forwards query params', () => {
    service.getEntries('f1', { limit: 10, offset: 20, level: 'ERROR' }).subscribe();
    const req = httpMock.expectOne((r) => r.url === '/api/files/f1/entries');
    expect(req.request.params.get('limit')).toBe('10');
    expect(req.request.params.get('offset')).toBe('20');
    expect(req.request.params.get('level')).toBe('ERROR');
    req.flush({ items: [], total: 0, limit: 10, offset: 20 });
  });

  it('search() forwards q, files, fields', () => {
    service.search('u1', { q: 'boom', files: ['f1', 'f2'], fields: 'raw' }).subscribe();
    const req = httpMock.expectOne((r) => r.url === '/api/uploads/u1/search');
    expect(req.request.params.get('q')).toBe('boom');
    expect(req.request.params.getAll('files')).toEqual(['f1', 'f2']);
    expect(req.request.params.get('fields')).toBe('raw');
    req.flush([]);
  });

  it('histogram() forwards bucket', () => {
    service.histogram('u1', 'hour', { files: ['f1'] }).subscribe();
    const req = httpMock.expectOne((r) => r.url === '/api/uploads/u1/histogram');
    expect(req.request.params.get('bucket')).toBe('hour');
    expect(req.request.params.getAll('files')).toEqual(['f1']);
    req.flush([]);
  });

  it('createHighlight() POSTs body', () => {
    service.createHighlight('u1', { text: 'err', color: '#ff0', lexeme: 0 }).subscribe();
    const req = httpMock.expectOne('/api/uploads/u1/highlights');
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toEqual({ text: 'err', color: '#ff0', lexeme: 0 });
    req.flush({ id: 'h1', upload_id: 'u1', text: 'err', color: '#ff0', lexeme: 0, created_at: 't' });
  });

  it('search() forwards mode and attrs (US-0006)', () => {
    service.search('u1', { q: 'boom', mode: 'regex', attrs: 'user:alice' }).subscribe();
    const req = httpMock.expectOne((r) => r.url === '/api/uploads/u1/search');
    expect(req.request.params.get('mode')).toBe('regex');
    expect(req.request.params.get('attrs')).toBe('user:alice');
    req.flush([]);
  });

  it('correlate() forwards mode and attrs (US-0006)', () => {
    service.correlate('u1', { q: 'err', mode: 'regex', attrs: 'k:v' }).subscribe();
    const req = httpMock.expectOne((r) => r.url === '/api/uploads/u1/correlate');
    expect(req.request.params.get('mode')).toBe('regex');
    expect(req.request.params.get('attrs')).toBe('k:v');
    req.flush({ items: [], total: 0, limit: 25, offset: 0 });
  });

  it('histogramByFile() forwards bucket and files', () => {
    service.histogramByFile('u1', 'day', { files: ['f1', 'f2'] }).subscribe();
    const req = httpMock.expectOne((r) => r.url === '/api/uploads/u1/histogram-by-file');
    expect(req.request.params.get('bucket')).toBe('day');
    expect(req.request.params.getAll('files')).toEqual(['f1', 'f2']);
    req.flush([]);
  });

  it('createPreset() POSTs body with snapshot', () => {
    const snap = { searchFilters: [], timeline: { from: '', to: '' }, highlights: [], selectedFileIds: [], correlateMode: false, pageSize: 25 };
    service.createPreset('u1', { name: 'v1', snapshot: snap }).subscribe();
    const req = httpMock.expectOne('/api/uploads/u1/presets');
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toEqual({ name: 'v1', snapshot: snap });
    req.flush({ id: 'p1', upload_id: 'u1', name: 'v1', snapshot: snap, created_at: 't' });
  });

  it('deletePreset() DELETEs /api/uploads/{id}/presets/{pid}', () => {
    service.deletePreset('u1', 'p1').subscribe();
    const req = httpMock.expectOne('/api/uploads/u1/presets/p1');
    expect(req.request.method).toBe('DELETE');
    req.flush(null);
  });

  it('listAnnotations() GETs annotations', () => {
    service.listAnnotations('u1').subscribe();
    const req = httpMock.expectOne('/api/uploads/u1/annotations');
    expect(req.request.method).toBe('GET');
    req.flush([]);
  });

  it('createAnnotation() POSTs entry-pin body', () => {
    service.createAnnotation('u1', { file_analyze_id: 'f1', entry_id: 3, note: 'n', color: '#f00' }).subscribe();
    const req = httpMock.expectOne('/api/uploads/u1/annotations');
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toEqual({ file_analyze_id: 'f1', entry_id: 3, note: 'n', color: '#f00' });
    req.flush({ id: 'a1', upload_id: 'u1', file_analyze_id: 'f1', entry_id: 3, ts: null, note: 'n', color: '#f00', created_at: 't' });
  });

  it('deleteAnnotation() DELETEs /api/uploads/{id}/annotations/{aid}', () => {
    service.deleteAnnotation('u1', 'a1').subscribe();
    const req = httpMock.expectOne('/api/uploads/u1/annotations/a1');
    expect(req.request.method).toBe('DELETE');
    req.flush(null);
  });

  it('clearViewState() DELETEs /api/uploads/{id}/view-state', () => {
    service.clearViewState('u1').subscribe();
    const req = httpMock.expectOne('/api/uploads/u1/view-state');
    expect(req.request.method).toBe('DELETE');
    req.flush(null);
  });

  it('uploadFiles() POSTs multipart with all files', () => {
    const f1 = new File(['a'], 'a.log');
    const f2 = new File(['b'], 'b.log');
    let result: any;
    service.uploadFiles([f1, f2]).subscribe((r) => (result = r));
    const req = httpMock.expectOne((r) => r.url === '/api/uploads' && r.method === 'POST');
    expect(req.request.body instanceof FormData).toBe(true);
    req.flush({ results: [{ filename: 'a.log', status: 'parsed', upload_id: 'u1' }, { filename: 'b.log', status: 'parsed', upload_id: 'u2' }] });
    expect(result.length).toBe(2);
    expect(result[0].upload_id).toBe('u1');
  });
});