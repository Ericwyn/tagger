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
	RawTagsResponse,
	Track,
	TrackPatch,
	WriteSelection,
	BatchEditItem,
	BatchEditOperation,
	BatchEditSelection,
	UpdateProvenance,
	CandidateSearchQuery,
	LyricsSidecarResponse,
	LyricsSidecarWriteResult,
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

	cancelJob(jobId: string): Promise<Job> {
	  return request<Job>(`/api/v1/jobs/${encodeURIComponent(jobId)}/cancel`, {method: 'POST'});
	},

	retryJob(jobId: string): Promise<Job> {
	  return request<Job>(`/api/v1/jobs/${encodeURIComponent(jobId)}/retry`, {method: 'POST'});
	},

	subscribeJobEvents(jobId: string, onJob: (job: Job) => void): () => void {
	  if (typeof EventSource === 'undefined') return () => undefined;
	  const source = new EventSource(`/api/v1/jobs/${encodeURIComponent(jobId)}/events`);
	  const handler = (event: Event) => {
		try {
		  const job = JSON.parse((event as MessageEvent<string>).data) as Job;
		  onJob(job);
		} catch {
		  // A malformed event is ignored; the regular GET refresh remains authoritative.
		}
	  };
	  source.addEventListener('job', handler);
	  return () => {
		source.removeEventListener('job', handler);
		source.close();
	  };
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

	createWriteJob(matchJobId: string, items: WriteSelection[]): Promise<Job> {
	  return request<Job>(`/api/v1/matches/jobs/${encodeURIComponent(matchJobId)}/write`, {
		method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({items}),
	  });
	},

	createBatchEditJob(items: BatchEditSelection[], operations: BatchEditOperation[], sequenceTracks: boolean): Promise<Job> {
	  return request<Job>('/api/v1/tracks/batch-edit', {
		method: 'POST', headers: {'Content-Type': 'application/json'},
		body: JSON.stringify({items, operations, sequenceTracks}),
	  });
	},

	listBatchEditItems(jobId: string): Promise<BatchEditItem[]> {
	  return request<BatchEditItem[]>(`/api/v1/jobs/${encodeURIComponent(jobId)}/batch-edit-items`);
	},

	getRawTags(trackId: string): Promise<RawTagsResponse> {
		return request<RawTagsResponse>(`/api/v1/tracks/${encodeURIComponent(trackId)}/raw-tags`);
	},

	getLyricsSidecar(trackId: string): Promise<LyricsSidecarResponse> {
		return request<LyricsSidecarResponse>(`/api/v1/tracks/${encodeURIComponent(trackId)}/lyrics-sidecar`);
	},

	writeLyricsSidecar(track: Track, content: string): Promise<LyricsSidecarWriteResult> {
		return request<LyricsSidecarWriteResult>(`/api/v1/tracks/${encodeURIComponent(track.id)}/lyrics-sidecar`, {
			method: 'PUT',
			headers: {
				'Content-Type': 'application/json',
				'If-Match': `"${track.revision}"`,
			},
			body: JSON.stringify({
				baseRevision: track.revision,
				baseSidecarRevision: track.lyricsSidecar?.revision ?? '',
				content,
				dryRun: false,
			}),
		});
	},

	deleteLyricsSidecar(track: Track): Promise<LyricsSidecarWriteResult> {
		return request<LyricsSidecarWriteResult>(`/api/v1/tracks/${encodeURIComponent(track.id)}/lyrics-sidecar`, {
			method: 'DELETE',
			headers: {
				'Content-Type': 'application/json',
				'If-Match': `"${track.revision}"`,
			},
			body: JSON.stringify({
				baseRevision: track.revision,
				baseSidecarRevision: track.lyricsSidecar?.revision ?? '',
				dryRun: false,
			}),
		});
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

	async searchCandidates(track: Track, override?: CandidateSearchQuery): Promise<MatchCandidate[]> {
		const query = override ?? {
			title: track.title,
			artists: track.artists,
			album: track.album,
			durationSeconds: track.durationSeconds,
		};
      const result = await request<{candidates: MatchCandidate[]; providers: Record<string, unknown>}>(
        '/api/v1/matches/tracks/search',
        {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            fileId: track.id,
            query,
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
