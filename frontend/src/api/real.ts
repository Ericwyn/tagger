import type {LibrarySummary, Track} from '@/types';

interface DataEnvelope<T> {
  data: T;
}

interface ErrorEnvelope {
  error?: {
    code?: string;
    message?: string;
  };
}

export interface ScanReport {
  startedAt: string;
  completedAt: string;
  discovered: number;
  parsed: number;
  failed: number;
  warningCount: number;
  warnings?: string[];
}

export interface ScanResult {
  library: LibrarySummary;
  report: ScanReport;
}

export class APIError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'APIError';
    this.status = status;
    this.code = code;
  }
}

export function createRealAPI(fetcher: typeof fetch = fetch) {
  async function request<T>(path: string, init?: RequestInit): Promise<T> {
    const response = await fetcher(path, {
      ...init,
      headers: {
        Accept: 'application/json',
        ...init?.headers,
      },
    });
    const payload = await response.json() as DataEnvelope<T> & ErrorEnvelope;
    if (!response.ok) {
      throw new APIError(
        response.status,
        payload.error?.code ?? 'request_failed',
        payload.error?.message ?? `请求失败（HTTP ${response.status}）`,
      );
    }
    return payload.data;
  }

  return {
    async getLibrary(): Promise<LibrarySummary> {
      const libraries = await request<LibrarySummary[]>('/api/v1/libraries');
      if (!libraries[0]) throw new APIError(404, 'library_not_found', '尚未配置音乐曲库');
      return libraries[0];
    },

    async listTracks(): Promise<Track[]> {
      const result = await request<{tracks: Track[]; total: number}>('/api/v1/tracks');
      return result.tracks;
    },

    async rescanLibrary(libraryId: string): Promise<ScanResult> {
      return request<ScanResult>(`/api/v1/libraries/${encodeURIComponent(libraryId)}/scans`, {method: 'POST'});
    },
  };
}
