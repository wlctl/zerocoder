import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting, HttpTestingController } from '@angular/common/http/testing';

import { FileWindowComponent } from './file-window.component';
import { FileAnalyze, Highlight } from '../../models';

describe('FileWindowComponent', () => {
  let httpMock: HttpTestingController;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [FileWindowComponent],
      providers: [provideHttpClient(), provideHttpClientTesting()],
    }).compileComponents();
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => httpMock.verify());

  it('loads file detail then highlights and renders app-file-table', () => {
    const fixture = TestBed.createComponent(FileWindowComponent);
    const comp = fixture.componentInstance;
    comp.fileId = 'f1';
    fixture.detectChanges(); // triggers ngOnInit

    const fileReq = httpMock.expectOne('/api/files/f1');
    expect(fileReq.request.method).toBe('GET');
    const file: FileAnalyze = {
      id: 'f1',
      upload_id: 'u1',
      filename: 'AdminServer.log',
      size: 1234,
      record_count: 5,
    };
    fileReq.flush(file);

    const hlReq = httpMock.expectOne('/api/uploads/u1/highlights');
    const hls: Highlight[] = [
      { id: 'h1', upload_id: 'u1', text: 'err', color: '#ff0', lexeme: 0, created_at: 't' },
    ];
    hlReq.flush(hls);

    fixture.detectChanges();

    // Вложенный app-file-table при инициализации грузит entries + histogram.
    const entriesReq = httpMock.expectOne((r) => r.url === '/api/files/f1/entries');
    entriesReq.flush({ items: [], total: 0, limit: 10, offset: 0 });
    const histReq = httpMock.expectOne((r) => r.url === '/api/uploads/u1/histogram');
    histReq.flush([]);

    expect(comp.file()?.filename).toBe('AdminServer.log');
    expect(comp.highlights().length).toBe(1);
    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.querySelector('app-file-table')).toBeTruthy();
  });

  it('sets error when fileId missing', () => {
    const fixture = TestBed.createComponent(FileWindowComponent);
    const comp = fixture.componentInstance;
    comp.fileId = '';
    fixture.detectChanges();
    expect(comp.error()).toContain('fileId');
    expect(comp.file()).toBeNull();
  });

  it('closeWindow() does not throw', () => {
    const fixture = TestBed.createComponent(FileWindowComponent);
    const comp = fixture.componentInstance;
    expect(() => comp.closeWindow()).not.toThrow();
  });
});