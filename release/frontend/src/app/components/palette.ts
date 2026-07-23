// Стабильная палитра цветов файлов (US-0005/US-0006): детерминированная по индексу.
// Используется correlate-table (цвет файла) и stacked-chart (сегменты по файлам).
// Per-file-chart имеет свою 8-цветную палитру (single-series, другой интент) — не объединяется.
export const FILE_PALETTE = [
  '#3b82f6', '#ef4444', '#f59e0b', '#10b981', '#8b5cf6',
  '#ec4899', '#14b8a6', '#6366f1', '#f97316', '#0ea5e9',
];

// fileColor возвращает стабильный цвет по индексу id в ids (цикл при >10 файлов).
export function fileColor(ids: string[], id: string): string {
  const i = ids.indexOf(id);
  if (i < 0) return '#9ca3af';
  return FILE_PALETTE[i % FILE_PALETTE.length];
}