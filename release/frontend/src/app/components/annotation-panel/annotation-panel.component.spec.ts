import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';

import { AnnotationPanelComponent } from './annotation-panel.component';
import { Annotation } from '../../models';

describe('AnnotationPanelComponent', () => {
  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [AnnotationPanelComponent],
      providers: [provideHttpClient(), provideHttpClientTesting()],
    }).compileComponents();
  });

  it('renders entry-pin and time-pin annotations with file name or "вне страницы"', () => {
    const fixture = TestBed.createComponent(AnnotationPanelComponent);
    const anns: Annotation[] = [
      { id: 'a1', upload_id: 'u1', file_analyze_id: 'fA', entry_id: 7, ts: null, note: 'boom', color: '#f00', created_at: 't' },
      { id: 'a2', upload_id: 'u1', file_analyze_id: null, entry_id: null, ts: '2018-05-16T14:26Z', note: 'restart', color: '#0f0', created_at: 't' },
      { id: 'a3', upload_id: 'u1', file_analyze_id: 'fGone', entry_id: 1, ts: null, note: 'dangling', color: '#00f', created_at: 't' },
    ];
    fixture.componentRef.setInput('annotations', anns);
    fixture.componentRef.setInput('fileNames', { fA: 'a.log' });
    fixture.detectChanges();

    const text = (fixture.nativeElement as HTMLElement).textContent!;
    expect(text).toContain('a.log #7');
    expect(text).toContain('2018-05-16T14:26Z');
    expect(text).toContain('вне страницы/удалена'); // fGone нет в fileNames → dangling
  });

  it('emits add with time-pin payload from the form', () => {
    const fixture = TestBed.createComponent(AnnotationPanelComponent);
    const comp = fixture.componentInstance;
    fixture.detectChanges();

    let emitted: any;
    comp.add.subscribe((p) => (emitted = p));
    comp.note.set('restart');
    comp.color.set('#ff0000');
    comp.timePin.set(true);
    comp.ts.set('2018-05-16T14:26');
    comp.submit();

    expect(emitted).toEqual({ note: 'restart', color: '#ff0000', ts: '2018-05-16T14:26' });
  });

  it('does not emit add when note empty or time missing', () => {
    const fixture = TestBed.createComponent(AnnotationPanelComponent);
    const comp = fixture.componentInstance;
    fixture.detectChanges();
    let emitted = false;
    comp.add.subscribe(() => (emitted = true));

    comp.note.set(''); comp.ts.set('2020-01-01T00:00'); comp.submit();
    expect(emitted).toBe(false);

    comp.note.set('x'); comp.ts.set(''); comp.timePin.set(true); comp.submit();
    expect(emitted).toBe(false);
  });

  it('emits remove when × clicked', () => {
    const fixture = TestBed.createComponent(AnnotationPanelComponent);
    const ann: Annotation = { id: 'a1', upload_id: 'u1', file_analyze_id: null, entry_id: null, ts: 't', note: 'n', color: '#f00', created_at: 't' };
    fixture.componentRef.setInput('annotations', [ann]);
    fixture.detectChanges();
    let removed: any;
    fixture.componentInstance.remove.subscribe((r) => (removed = r));
    const btn = (fixture.nativeElement as HTMLElement).querySelector('.ann__x') as HTMLButtonElement;
    btn.click();
    expect(removed).toEqual(ann);
  });
});