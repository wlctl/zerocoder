// Domain models for LogAnalyzer viewer (contract from architect/specs/viewer.spec.md).
// Field names follow the backend spec; response envelopes are best-effort and may need
// alignment with the backend agent (see open questions).

export type UploadKind = 'file' | 'zip';

export interface UploadSummary {
  // Single-log summary from t_files_analyze.summary (free JSON: level_counts, sessions, ...)
  // For archives: file_count instead.
  level_counts?: Record<string, number>;
  sessions?: number;
  [k: string]: unknown;
}

export interface Upload {
  id: string;
  filename: string;
  kind: UploadKind;
  size: number;
  uploaded_at: string;
  status: string;
  file_count?: number;
  summary?: UploadSummary | null;
  first_ts?: string | null;
  last_ts?: string | null;
}

export interface UploadDetail extends Upload {
  files?: FileAnalyze[];
}

export interface FileAnalyze {
  id: string;
  upload_id: string;
  filename: string;
  size: number;
  record_count?: number;
  encoding?: string;
  first_ts?: string | null;
  last_ts?: string | null;
  duration_sec?: number | null;
  summary?: UploadSummary | null;
}

export interface LogEntry {
  id: string | number;
  file_analyze_id: string;
  ts: string | null;
  ts_raw?: string | null;
  level: string | null;
  component: string | null;
  message: string | null;
  raw_line: string | null;
}

export interface EntriesPage {
  items: LogEntry[];
  total: number;
  limit: number;
  offset: number;
}

// Корреляция по времени (US-0005): объединённый кросс-файл поток записей,
// отсортированный по ts, с пометкой файла (filename + file_analyze_id).
export interface CorrelatedEntry extends LogEntry {
  filename: string;
  format?: string;
  seq?: number;
}

export interface CorrelatedPage {
  items: CorrelatedEntry[];
  total: number;
  limit: number;
  offset: number;
}

export interface Stats {
  storage_size: number;
  upload_count: number;
  file_count: number;
  record_count: number;
}

export interface TimelineBounds {
  min_ts: string | null;
  max_ts: string | null;
}

export type Bucket = 'month' | 'day' | 'hour' | 'minute';

export interface HistogramBucketPoint {
  bucket: string;
  count: number;
}

// US-0006: стекированный по файлам график — точка с разбивкой по file_analyze_id.
export interface HistogramByFilePoint {
  bucket: string;
  file_analyze_id: string;
  count: number;
}

// US-0006: пресет — снимок состояния просмотра (сохраняется как JSON-blob).
export interface PresetSnapshot {
  searchFilters: { q: string; fields: 'all' | 'raw'; mode?: 'text' | 'regex'; attrs?: string }[];
  timeline: { from: string; to: string };
  highlights: { text: string; color: string; lexeme: number }[];
  selectedFileIds: string[];
  correlateMode: boolean;
  pageSize: number;
}

export interface Preset {
  id: string;
  upload_id: string;
  name: string;
  snapshot: PresetSnapshot;
  created_at: string;
}

// US-0006: аннотация-пин. entry-pin (file_analyze_id+entry_id) или time-pin (ts);
// nullable-поля — null в JSON, когда пин другого типа. entry_id без FK — может dangling.
export interface Annotation {
  id: string;
  upload_id: string;
  file_analyze_id: string | null;
  entry_id: number | null;
  ts: string | null;
  note: string;
  color: string;
  created_at: string;
}

export interface Lexeme {
  term: string;
  count: number;
}

export type FilterKind = 'search' | 'timeline';

export interface ViewFilter {
  id: string;
  upload_id: string;
  kind: FilterKind;
  rule: SearchRule | TimelineRule;
  created_at: string;
}

export interface SearchRule {
  q: string;
  files?: string[];
  fields?: 'all' | 'raw';
  mode?: 'text' | 'regex'; // US-0006: regex — серверный REGEXP
  attrs?: string; // US-0006: "k1:v1,k2:v2" → json_extract
}

export interface TimelineRule {
  from?: string;
  to?: string;
  files?: string[];
}

export interface Highlight {
  id: string;
  upload_id: string;
  text: string;
  color: string;
  lexeme: number;
  created_at: string;
}

export interface ParserInfo {
  id: string;
  name: string;
  description?: string;
}

// ---- upload (multi-file) ----

export interface UploadFileResult {
  file_analyze_id?: string;
  filename: string;
  path_in_archive?: string;
  format?: string;
  status: string;
  record_count?: number;
  summary?: UploadSummary | null;
  error?: string;
}

export type UploadResultStatus = 'parsed' | 'failed' | 'duplicate';

export interface UploadResultItem {
  upload_id?: string;
  filename: string;
  kind?: UploadKind;
  md5?: string;
  size_bytes?: number;
  status: UploadResultStatus;
  error?: string;
  duplicate?: boolean;
  existing_upload_id?: string;
  files?: UploadFileResult[];
}

export interface UploadResponse {
  results: UploadResultItem[];
}