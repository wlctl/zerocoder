import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';

import { StackedChartComponent, bucketOf } from './stacked-chart.component';
import { Annotation, HistogramByFilePoint } from '../../models';

describe('bucketOf', () => {
  const ts = '2018-05-16T14:26:28Z';
  it('month/day/hour/minute slice the ISO prefix', () => {
    expect(bucketOf(ts, 'month')).toBe('2018-05');
    expect(bucketOf(ts, 'day')).toBe('2018-05-16');
    expect(bucketOf(ts, 'hour')).toBe('2018-05-16T14');
    expect(bucketOf(ts, 'minute')).toBe('2018-05-16T14:26');
  });
  it('empty ts → empty', () => expect(bucketOf('', 'hour')).toBe(''));
});

describe('StackedChartComponent', () => {
  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [StackedChartComponent],
      providers: [provideHttpClient(), provideHttpClientTesting()],
    }).compileComponents();
  });

  it('aggregates flat points into stacked bars per bucket with per-file segments', () => {
    const fixture = TestBed.createComponent(StackedChartComponent);
    const comp = fixture.componentInstance;
    const data: HistogramByFilePoint[] = [
      { bucket: '2018-05-16T14', file_analyze_id: 'fA', count: 2 },
      { bucket: '2018-05-16T14', file_analyze_id: 'fB', count: 1 },
      { bucket: '2018-05-16T15', file_analyze_id: 'fA', count: 3 },
    ];
    fixture.componentRef.setInput('dataInput', data);
    fixture.componentRef.setInput('bucketInput', 'hour');
    fixture.componentRef.setInput('fileIds', ['fA', 'fB']);
    fixture.componentRef.setInput('fileNames', { fA: 'a.log', fB: 'b.log' });
    fixture.detectChanges();

    const bars = comp.bars();
    expect(bars.length).toBe(2); // два бакета
    const b14 = bars.find((b) => b.bucket === '2018-05-16T14')!;
    expect(b14.segments.length).toBe(2); // fA + fB
    expect(b14.segments.map((s) => s.count).reduce((a, b) => a + b, 0)).toBe(3);
    expect(comp.total()).toBe(6);

    // цвет стабилен per file id по индексу в fileIds
    const colA = b14.segments.find((s) => s.fileId === 'fA')!.color;
    const colB = b14.segments.find((s) => s.fileId === 'fB')!.color;
    expect(colA).not.toBe(colB);

    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.querySelectorAll('rect').length).toBe(3); // 2 + 1 сегмент
    expect(compiled.querySelector('.chart__legend')?.textContent).toContain('a.log');
  });

  it('renders time-pin annotation markers at their bucket x', () => {
    const fixture = TestBed.createComponent(StackedChartComponent);
    const comp = fixture.componentInstance;
    fixture.componentRef.setInput('dataInput', [
      { bucket: '2018-05-16T14', file_analyze_id: 'fA', count: 2 },
    ] as HistogramByFilePoint[]);
    fixture.componentRef.setInput('bucketInput', 'hour');
    fixture.componentRef.setInput('fileIds', ['fA']);
    const ann: Annotation[] = [
      { id: 'an1', upload_id: 'u1', file_analyze_id: null, entry_id: null, ts: '2018-05-16T14:26:28Z', note: 'restart', color: '#ff0000', created_at: 't' },
      { id: 'an2', upload_id: 'u1', file_analyze_id: 'fA', entry_id: 5, ts: null, note: 'entry', color: '#00ff00', created_at: 't' },
    ];
    fixture.componentRef.setInput('annotationsInput', ann);
    fixture.detectChanges();

    // только time-pin (an1) попадает в маркеры; entry-pin (an2) без ts — нет
    expect(comp.markers().length).toBe(1);
    expect(comp.markers()[0].aid).toBe('an1');
    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.querySelectorAll('.chart__markers line').length).toBe(1);
  });

  it('empty data shows placeholder and no bars', () => {
    const fixture = TestBed.createComponent(StackedChartComponent);
    fixture.componentRef.setInput('dataInput', []);
    fixture.componentRef.setInput('bucketInput', 'hour');
    fixture.componentRef.setInput('fileIds', []);
    fixture.detectChanges();
    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.textContent).toContain('нет данных');
    expect(compiled.querySelectorAll('rect').length).toBe(0);
  });

  it('emits bucketChange when grouping select changes', () => {
    const fixture = TestBed.createComponent(StackedChartComponent);
    fixture.componentRef.setInput('dataInput', []);
    fixture.componentRef.setInput('bucketInput', 'hour');
    fixture.componentRef.setInput('fileIds', []);
    fixture.detectChanges();
    const comp = fixture.componentInstance;
    let emitted = '';
    comp.bucketChange.subscribe((b) => (emitted = b));
    const select = (fixture.nativeElement as HTMLElement).querySelector('select')!;
    select.value = 'day';
    select.dispatchEvent(new Event('change'));
    expect(emitted).toBe('day');
  });
});