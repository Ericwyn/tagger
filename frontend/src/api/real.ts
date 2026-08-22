import type {
  LibrarySummary,
	Job,
  MatchCandidate,
	MatchItem,
  ArtworkWriteResult,
  ProviderConfig,
  RestorePreview,
  RestoreResult,
  Revision,
  Track,
  TrackPatch,
  UpdateProvenance,
} from '@/types';

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

interface FieldOperation<T> {
  op: 'set' | 'delete';
  value?: T;
}

export interface WriteResult {
  track: Track;
  write: {
    baseRevision: string;
    currentRevision: string;
    changed: boolean;
    warnings: string[];
  };
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

	async rescanLibrary(libraryId: string): Promise<Job> {
	  return request<Job>(`/api/v1/libraries/${encodeURIComponent(libraryId)}/scans`, {method: 'POST'});
    },

	listJobs(): Promise<Job[]> {
	  return request<Job[]>('/api/v1/jobs');
	},

	getJob(jobId: string): Promise<Job> {
	  return request<Job>(`/api/v1/jobs/${encodeURIComponent(jobId)}`);
	},

	createMatchJob(trackIds: string[], providerIds: string[] = []): Promise<Job> {
	  return request<Job>('/api/v1/matches/tracks/batch', {
		method: 'POST', headers: {'Content-Type': 'application/json'},
		body: JSON.stringify({trackIds, providerIds, limit: 5}),
	  });
	},

	listMatchItems(jobId: string): Promise<MatchItem[]> {
	  return request<MatchItem[]>(`/api/v1/jobs/${encodeURIComponent(jobId)}/matches`);
	},

    async updateTrack(track: Track, patch: TrackPatch, provenance?: UpdateProvenance): Promise<WriteResult> {
      return request<WriteResult>(`/api/v1/tracks/${encodeURIComponent(track.id)}/tags`, {
        method: 'PATCH',
        headers: {
          'Content-Type': 'application/json',
          'If-Match': `"${track.revision}"`,
        },
        body: JSON.stringify({
          baseRevision: track.revision,
          patch: serializePatch(track, patch),
          dryRun: false,
          provenance,
        }),
      });
    },

    async searchCandidates(track: Track): Promise<MatchCandidate[]> {
      const result = await request<{candidates: MatchCandidate[]; providers: Record<string, unknown>}>(
        '/api/v1/matches/tracks/search',
        {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            fileId: track.id,
            query: {
              title: track.title,
              artists: track.artists,
              album: track.album,
              durationSeconds: track.durationSeconds,
            },
            limitPerProvider: 5,
          }),
        },
      );
      return result.candidates;
    },

	listProviders(): Promise<ProviderConfig[]> {
      return request<ProviderConfig[]>('/api/v1/providers');
    },

    listRevisions(): Promise<Revision[]> {
      return request<Revision[]>('/api/v1/revisions');
    },

    previewRevisionRestore(revisionId: string, baseRevision: string): Promise<RestorePreview> {
      return request<RestorePreview>(`/api/v1/revisions/${encodeURIComponent(revisionId)}/restore-preview`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json', 'If-Match': `"${baseRevision}"`},
        body: JSON.stringify({baseRevision, target: 'before'}),
      });
    },

    restoreRevision(revisionId: string, baseRevision: string): Promise<RestoreResult> {
      return request<RestoreResult>(`/api/v1/revisions/${encodeURIComponent(revisionId)}/restore`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json', 'If-Match': `"${baseRevision}"`},
        body: JSON.stringify({baseRevision, target: 'before'}),
      });
    },

    writeArtwork(track: Track, file: File): Promise<ArtworkWriteResult> {
	  return request<ArtworkWriteResult>(`/api/v1/tracks/${encodeURIComponent(track.id)}/artwork/0`, {
		method: 'PUT',
		headers: {'Content-Type': file.type || 'application/octet-stream', 'If-Match': `"${track.revision}"`},
		body: file,
	  });
	},

	updateProvider(providerId: string, enabled: boolean): Promise<ProviderConfig> {
	  return request<ProviderConfig>(`/api/v1/providers/${encodeURIComponent(providerId)}`, {
		method: 'PATCH', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({enabled}),
	  });
	},

	testProvider(providerId: string): Promise<{provider: ProviderConfig; result: {status: string; count: number; latencyMs: number; cached?: boolean}}> {
	  return request(`/api/v1/providers/${encodeURIComponent(providerId)}/test`, {method: 'POST'});
	},

	deleteArtwork(track: Track): Promise<ArtworkWriteResult> {
	  return request<ArtworkWriteResult>(`/api/v1/tracks/${encodeURIComponent(track.id)}/artwork/0`, {
		method: 'DELETE',
		headers: {'If-Match': `"${track.revision}"`},
	  });
	},

	applyCandidateArtwork(track: Track, candidateId: string): Promise<ArtworkWriteResult> {
	  return request<ArtworkWriteResult>(`/api/v1/matches/tracks/${encodeURIComponent(track.id)}/artwork`, {
		method: 'POST',
		headers: {'Content-Type': 'application/json', 'If-Match': `"${track.revision}"`},
		body: JSON.stringify({candidateId, baseRevision: track.revision, dryRun: false}),
	  });
	},
  };
}

function serializePatch(track: Track, patch: TrackPatch) {
  const result: Record<string, FieldOperation<string | string[] | number>> = {};
  if (patch.title !== track.title) result.title = stringOperation(patch.title);
  if (!arraysEqual(patch.artists, track.artists)) result.artists = stringsOperation(patch.artists);
  if (patch.album !== track.album) result.album = stringOperation(patch.album);
  if (!arraysEqual(patch.albumArtists, track.albumArtists)) result.albumArtists = stringsOperation(patch.albumArtists);
  if (patch.trackNumber !== track.trackNumber) result.trackNumber = numberOperation(patch.trackNumber);
  if (patch.trackTotal !== track.trackTotal) result.trackTotal = numberOperation(patch.trackTotal);
  if (patch.discNumber !== track.discNumber) result.discNumber = numberOperation(patch.discNumber);
  if (patch.discTotal !== track.discTotal) result.discTotal = numberOperation(patch.discTotal);
  if (patch.year !== track.year) result.year = numberOperation(patch.year);
  if (!arraysEqual(patch.genres, track.genres)) result.genres = stringsOperation(patch.genres);
  if (patch.lyrics !== track.lyrics) result.lyrics = stringOperation(patch.lyrics);
  return result;
}

function stringOperation(value: string): FieldOperation<string> {
  const normalized = value.trim();
  return normalized ? {op: 'set', value: normalized} : {op: 'delete'};
}

function stringsOperation(value: string[]): FieldOperation<string[]> {
  const normalized = value.map((item) => item.trim()).filter(Boolean);
  return normalized.length > 0 ? {op: 'set', value: normalized} : {op: 'delete'};
}

function numberOperation(value?: number): FieldOperation<number> {
  return value === undefined ? {op: 'delete'} : {op: 'set', value};
}

function arraysEqual(left: string[], right: string[]): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}
