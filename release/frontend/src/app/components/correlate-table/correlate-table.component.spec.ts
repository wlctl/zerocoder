import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting, HttpTestingController } from '@angular/common/http/testing';

import { CorrelateTableComponent } from './correlate-table.component';

describe('CorrelateTableComponent', () => {
  let httpMock: HttpTestingController;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [CorrelateTableComponent],
      providers: [provideHttpClient(), provideHttpClientTesting()],
    }).compileComponents();
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => httpMock.verify());

  it('loads correlate + histogram and renders cross-file rows ordered by ts with file color', () => {
    const fixture = TestBed.createComponent(CorrelateTableComponent);
    const comp = fixture.componentInstance;
    fixture.componentRef.setInput('uploadId', 'u1');
    fixture.componentRef.setInput('fileIds', ['fA', 'fB']);
    fixture.detectChanges(); // triggers ngOnChanges → load()

    const corrReq = httpMock.expectOne((r) => r.url === '/api/uploads/u1/correlate');
    expect(corrReq.request.method).toBe('GET');
    expect(corrReq.request.params.getAll('files')).toEqual(['fA', 'fB']);
    corrReq.flush({
      items: [
        { id: 1, file_analyze_id: 'fA', filename: 'a.log', ts: '2018-05-16T14:26:25Z', level: 'info', component: 'Security', message: 'first', raw_line: 'raw-a1' },
        { id: 2, file_analyze_id: 'fB', filename: 'b.log', ts: '2018-05-16T14:26:28Z', level: 'info', component: 'Main', message: 'mid', raw_line: 'raw-b' },
        { id: 3, file_analyze_id: 'fA', filename: 'a.log', ts: '2018-05-16T14:26:31Z', level: 'info', component: 'WLS', message: 'third', raw_line: 'raw-a2' },
      ],
      total: 3,
      limit: 25,
      offset: 0,
    });
    const histReq = httpMock.expectOne((r) => r.url === '/api/uploads/u1/histogram');
    histReq.flush([{ bucket: '2018-05-16T14', count: 3 }]);

    fixture.detectChanges();

    expect(comp.total()).toBe(3);
    expect(comp.items().length).toBe(3);
    // стабильная палитра per file id (fA — индекс 0, fB — индекс 1)
    expect(comp.fileColor('fA')).toBe(comp.fileColor('fA'));
    expect(comp.fileColor('fA')).not.toBe(comp.fileColor('fB'));

    const compiled = fixture.nativeElement as HTMLElement;
    const rows = compiled.querySelectorAll('tbody tr');
    expect(rows.length).toBe(3);
    expect(rows[0].textContent).toContain('a.log');
    expect(rows[1].textContent).toContain('b.log');
  });

  it('handles empty result without throwing', () => {
    const fixture = TestBed.createComponent(CorrelateTableComponent);
    const comp = fixture.componentInstance;
    fixture.componentRef.setInput('uploadId', 'u1');
    fixture.componentRef.setInput('fileIds', []);
    fixture.detectChanges();

    const corrReq = httpMock.expectOne((r) => r.url === '/api/uploads/u1/correlate');
    corrReq.flush({ items: [], total: 0, limit: 25, offset: 0 });
    const histReq = httpMock.expectOne((r) => r.url === '/api/uploads/u1/histogram');
    histReq.flush([]);

    fixture.detectChanges();
    expect(comp.total()).toBe(0);
    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.textContent).toContain('нет записей');
  });

  it('applies from/to and q filters to the correlate request', () => {
    const fixture = TestBed.createComponent(CorrelateTableComponent);
    fixture.componentRef.setInput('uploadId', 'u1');
    fixture.componentRef.setInput('fileIds', ['fA']);
    fixture.componentRef.setInput('from', '2018-05-16T14:26:26Z');
    fixture.componentRef.setInput('to', '2018-05-16T14:26:30Z');
    fixture.componentRef.setInput('searchQ', 'mid');
    fixture.detectChanges();

    const corrReq = httpMock.expectOne((r) => r.url === '/api/uploads/u1/correlate');
    expect(corrReq.request.params.get('from')).toBe('2018-05-16T14:26:26Z');
    expect(corrReq.request.params.get('to')).toBe('2018-05-16T14:26:30Z');
    expect(corrReq.request.params.get('q')).toBe('mid');
    corrReq.flush({ items: [], total: 0, limit: 25, offset: 0 });
    httpMock.expectOne((r) => r.url === '/api/uploads/u1/histogram').flush([]);
  });
});