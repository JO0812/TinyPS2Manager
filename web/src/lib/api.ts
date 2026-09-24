// Typed client for the oplbm REST + SSE API (mirrors internal/api DTOs).

export interface LibraryItem {
  id: number;
  sourcePath: string;
  contentHash: string;
  platform: 'ps2' | 'ps1';
  discType: string; // '' for PS1
  detectionMethod: string;
  title: string;
  discIndex: number;
  discGroupId: number | null;
  sizeBytes: number;
  status: string;
}

export interface Job {
  id: number;
  libraryItemId: number;
  destinationId: number;
  kind: string;
  order: number;
  status: 'pending' | 'running' | 'paused' | 'error' | 'done';
  phase: string;
  bytesTotal: number;
  bytesDone: number;
  error: string;
  attempts: number;
  createdAt: string;
  updatedAt: string;
}

export interface Destination {
  id: number;
  path: string;
  kind: string;
  filesystem: string;
  fsOverride: string;
  bdmPrefix: string;
  freeBytes: number;
  totalBytes: number;
  updatedAt: string;
}

export interface Settings {
  theme: 'system' | 'light' | 'dark';
  stagingDir: string;
  splitThreshold: number;
  bdmPrefixDefault: string;
  filesystemDefault: string;
}

export interface ProgressEvent {
  jobId: number;
  phase: string;
  bytesDone: number;
  bytesTotal: number;
  etaSec: number;
  message: string;
}

export interface RiptoplPreview {
  tag: string;
  asset: string;
  url: string;
  sizeBytes: number;
  digest: string;
  flavours: string[];
}

export interface PreparePreview {
  dirs: string[];
  files: string[];
  riptopl?: RiptoplPreview;
  warnings: string[];
  checklist: string[];
}

export interface StagedLoader {
  tag: string;
  asset: string;
  digest: string;
  flavour: string;
  elfPath: string;
  elfSize: number;
  stagedAt: string;
}

export interface PrepareResult {
  jobs: Job[];
  riptopl?: StagedLoader;
  dirs: string[];
  files: string[];
  checklist: string[];
}

export interface PrepareRequest {
  mode: 'preview' | 'execute';
  itemIds: number[];
  riptoplTag?: string;
  flavour?: string;
}

export interface FieldError {
  field: string;
  msg: string;
}

export class ApiError extends Error {
  status: number;
  errors: FieldError[];
  constructor(status: number, errors: FieldError[]) {
    super(errors.map((e) => (e.field ? `${e.field}: ${e.msg}` : e.msg)).join('; '));
    this.status = status;
    this.errors = errors;
  }
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : {},
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  const data = text ? (JSON.parse(text) as T) : (undefined as T);
  if (!res.ok) {
    const errs = (data as { errors?: FieldError[] })?.errors ?? [{ field: '', msg: `HTTP ${res.status}` }];
    throw new ApiError(res.status, errs);
  }
  return data;
}

const get = <T>(p: string) => req<T>('GET', p);
const post = <T>(p: string, b?: unknown) => req<T>('POST', p, b);
const patch = <T>(p: string, b: unknown) => req<T>('PATCH', p, b);
const put = <T>(p: string, b: unknown) => req<T>('PUT', p, b);

export const api = {
  library: () => get<LibraryItem[]>('/api/library'),
  importDir: (path: string) => post<LibraryItem[]>('/api/library/import', { path }),
  patchLibrary: (id: number, body: { discType?: string; title?: string; discGroupId?: number | null }) =>
    patch<LibraryItem>(`/api/library/${id}`, body),

  destinations: () => get<Destination[]>('/api/destinations'),
  createDestination: (body: { path: string; kind?: string; filesystemOverride?: string; bdmPrefix?: string }) =>
    post<Destination>('/api/destinations', body),
  patchDestination: (id: number, body: { bdmPrefix?: string; filesystemOverride?: string }) =>
    patch<Destination>(`/api/destinations/${id}`, body),

  queue: () => get<Job[]>('/api/queue'),
  enqueue: (destinationId: number, itemIds: number[]) =>
    post<Job[]>('/api/queue', { destinationId, itemIds }),
  jobAction: (id: number, body: { action?: string; order?: number }) =>
    patch<Job | { deleted: boolean }>(`/api/queue/${id}`, body),
  pauseAll: () => post<{ paused: boolean }>('/api/queue/pause'),
  resumeAll: () => post<{ paused: boolean }>('/api/queue/resume'),

  prepare: (destId: number, body: PrepareRequest) =>
    post<PreparePreview | PrepareResult>(`/api/destinations/${destId}/prepare`, body),

  settings: () => get<Settings>('/api/settings'),
  saveSettings: (s: Settings) => put<Settings>('/api/settings', s),
};

/** Subscribe to live queue progress; returns an unsubscribe function. */
export function subscribeQueueEvents(onEvent: (e: ProgressEvent) => void): () => void {
  const src = new EventSource('/api/queue/events');
  src.onmessage = (m) => {
    try {
      onEvent(JSON.parse(m.data) as ProgressEvent);
    } catch {
      /* partial frame: next tick heals */
    }
  };
  return () => src.close();
}

/** Human bytes: 684 GB, 72 GB, 9408 B. */
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let v = n;
  let u = -1;
  do {
    v /= 1024;
    u++;
  } while (v >= 1024 && u < units.length - 1);
  return `${v >= 100 ? Math.round(v) : v.toFixed(1)} ${units[u]}`;
}
