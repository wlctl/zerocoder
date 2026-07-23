import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';

import { PresetBarComponent } from './preset-bar.component';
import { Preset } from '../../models';

describe('PresetBarComponent', () => {
  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [PresetBarComponent],
      providers: [provideHttpClient(), provideHttpClientTesting()],
    }).compileComponents();
  });

  const presets: Preset[] = [
    { id: 'p1', upload_id: 'u1', name: '-errors', snapshot: {} as any, created_at: 't' },
    { id: 'p2', upload_id: 'u1', name: 'noon', snapshot: {} as any, created_at: 't' },
  ];

  it('renders preset options and disables load/delete until one is selected', () => {
    const fixture = TestBed.createComponent(PresetBarComponent);
    fixture.componentRef.setInput('presets', presets);
    fixture.detectChanges();
    const el = fixture.nativeElement as HTMLElement;
    expect(el.querySelector('select')!.textContent).toContain('errors');
    expect(el.querySelector('select')!.textContent).toContain('noon');
    const loadBtn = el.querySelectorAll('button')[0] as HTMLButtonElement;
    expect(loadBtn.disabled).toBe(true);
  });

  it('emits load for the selected preset', () => {
    const fixture = TestBed.createComponent(PresetBarComponent);
    const comp = fixture.componentInstance;
    fixture.componentRef.setInput('presets', presets);
    fixture.detectChanges();
    let loaded: any;
    comp.load.subscribe((p) => (loaded = p));
    comp.selectedId.set('p2');
    comp.loadSelected();
    expect(loaded).toEqual(presets[1]);
  });

  it('emits delete for the selected preset and clears selection', () => {
    const fixture = TestBed.createComponent(PresetBarComponent);
    const comp = fixture.componentInstance;
    fixture.componentRef.setInput('presets', presets);
    fixture.detectChanges();
    let deleted: any;
    comp.delete.subscribe((p) => (deleted = p));
    comp.selectedId.set('p1');
    comp.deleteSelected();
    expect(deleted).toEqual(presets[0]);
    expect(comp.selectedId()).toBe('');
  });

  it('emits save with trimmed name and clears the input', () => {
    const fixture = TestBed.createComponent(PresetBarComponent);
    const comp = fixture.componentInstance;
    fixture.componentRef.setInput('presets', presets);
    fixture.detectChanges();
    let saved = '';
    comp.save.subscribe((n) => (saved = n));
    comp.name.set('  my view  ');
    comp.submitSave();
    expect(saved).toBe('my view');
    expect(comp.name()).toBe('');
  });

  it('does not emit save when name is blank', () => {
    const fixture = TestBed.createComponent(PresetBarComponent);
    const comp = fixture.componentInstance;
    fixture.detectChanges();
    let saved = false;
    comp.save.subscribe(() => (saved = true));
    comp.name.set('   ');
    comp.submitSave();
    expect(saved).toBe(false);
  });
});