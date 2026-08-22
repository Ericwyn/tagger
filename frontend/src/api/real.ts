import type {
	LibrarySummary,
	DirectoryProbe,
	Job,
  MatchCandidate,
	MatchItem,
  ArtworkWriteResult,
	ProviderConfig,
	ProviderTestResponse,
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
	MatchQueryHistory,
	LyricsSidecarResponse,
	LyricsSidecarWriteResult,
} from '@/types';

export interface BatchArtworkPayload {
  action: 'replace' | 'delete';
  data?: string;
  mime?: string;
  maxSize?: number;
}

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

export interface SystemInfo {
  version: string;
  tag_engine: string;
  listen?: string;
  historyRetention?: number;
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

const authTokenKey = 'tagger-auth-token';

function storedAuthToken(): string {
  if (typeof localStorage === 'undefined') return '';
  return localStorage.getItem(authTokenKey)?.trim() ?? '';
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : [];
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

// Keep older SQLite payloads/API responses readable after new normalized fields
// are added. A rescan will populate the fields, but opening the library must
// never crash just because an existing track predates the schema.
export function normalizeTrack(track: Track): Track {
  const raw = track as Track & Record<string, unknown>;
  const bpm = typeof raw.bpm === 'number' && Number.isFinite(raw.bpm) ? raw.bpm : undefined;
  return {
    ...track,
    title: stringValue(raw.title),
    artists: stringArray(raw.artists),
    album: stringValue(raw.album),
    albumArtists: stringArray(raw.albumArtists),
    genres: stringArray(raw.genres),
    lyrics: stringValue(raw.lyrics),
    comment: stringValue(raw.comment),
    composers: stringArray(raw.composers),
    conductor: stringValue(raw.conductor),
    lyricists: stringArray(raw.lyricists),
    copyright: stringValue(raw.copyright),
    bpm,
    isrc: stringValue(raw.isrc),
    musicbrainzTrackId: stringValue(raw.musicbrainzTrackId),
    musicbrainzReleaseId: stringValue(raw.musicbrainzReleaseId),
    musicbrainzArtistIds: stringArray(raw.musicbrainzArtistIds),
    acoustidId: stringValue(raw.acoustidId),
    acoustidFingerprint: stringValue(raw.acoustidFingerprint),
  };
}

function normalizeTrackResult<T extends {track: Track}>(result: T): T {
  return {...result, track: normalizeTrack(result.track)};
}

export function createRealAPI(fetcher: typeof fetch = fetch) {
  async function request<T>(path: string, init?: RequestInit): Promise<T> {
    const token = storedAuthToken();
    const response = await fetcher(path, {
      ...init,
      headers: {
        Accept: 'application/json',
        ...(token ? {Authorization: `Bearer ${token}`} : {}),
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
    getSystem(): Promise<SystemInfo> {
      return request<SystemInfo>('/api/v1/system');
    },

    updateSystemSettings(historyRetention: number): Promise<{historyRetention: number}> {
      return request<{historyRetention: number}>('/api/v1/system/settings', {
        method: 'PATCH',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({historyRetention}),
      });
    },

    async getLibrary(): Promise<LibrarySummary> {
      const libraries = await request<LibrarySummary[]>('/api/v1/libraries');
      if (!libraries[0]) throw new APIError(404, 'library_not_found', '尚未配置音乐曲库');
      return libraries[0];
    },

    probeLibrary(path: string): Promise<DirectoryProbe> {
      return request<DirectoryProbe>('/api/v1/libraries/probe', {
        method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({path}),
      });
    },

    switchLibrary(libraryId: string, path: string): Promise<Job> {
      return request<Job>(`/api/v1/libraries/${encodeURIComponent(libraryId)}/switch`, {
        method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({path}),
      });
    },

	async listTracks(): Promise<Track[]> {
	  const result = await request<{tracks: Track[]; total: number}>('/api/v1/tracks');
	  return (result.tracks ?? []).map(normalizeTrack);
	},

	rescanTrack(trackId: string): Promise<Track> {
	  return request<Track>(`/api/v1/tracks/${encodeURIComponent(trackId)}/scan`, {method: 'POST'}).then(normalizeTrack);
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
	  // The backend exchanges the header token for an HttpOnly same-origin
	  // cookie, so native EventSource can authenticate without custom headers.
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

	updateMatchItem(jobId: string, trackId: string, state: 'review' | 'accepted' | 'skipped', selectedCandidateId?: string, fields?: string[], artwork?: boolean, artworkMaxSize?: number): Promise<MatchItem> {
	  return request<MatchItem>(`/api/v1/matches/jobs/${encodeURIComponent(jobId)}/items/${encodeURIComponent(trackId)}`, {
		method: 'PATCH',
		headers: {'Content-Type': 'application/json'},
		body: JSON.stringify({state, selectedCandidateId, fields, artwork, artworkMaxSize}),
	  });
	},

	rematchMatchItem(jobId: string, trackId: string, query?: CandidateSearchQuery, providerIds: string[] = []): Promise<MatchItem> {
	  return request<{item: MatchItem; providers: Record<string, unknown>}>(`/api/v1/matches/jobs/${encodeURIComponent(jobId)}/items/${encodeURIComponent(trackId)}/rematch`, {
		method: 'POST',
		headers: {'Content-Type': 'application/json'},
		body: JSON.stringify({query, providerIds, limitPerProvider: 5}),
	  }).then((result) => result.item);
	},

	createWriteJob(matchJobId: string, items: WriteSelection[]): Promise<Job> {
	  return request<Job>(`/api/v1/matches/jobs/${encodeURIComponent(matchJobId)}/write`, {
		method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({items}),
	  });
	},

	createBatchEditJob(items: BatchEditSelection[], operations: BatchEditOperation[], sequenceTracks: boolean, artwork?: BatchArtworkPayload): Promise<Job> {
	  return request<Job>('/api/v1/tracks/batch-edit', {
		method: 'POST', headers: {'Content-Type': 'application/json'},
		body: JSON.stringify({items, operations, sequenceTracks, ...(artwork ? {artwork} : {})}),
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
		}).then(normalizeTrackResult);
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
		}).then(normalizeTrackResult);
	},

    async updateTrack(track: Track, patch: TrackPatch, provenance?: UpdateProvenance): Promise<WriteResult> {
      const result = await request<WriteResult>(`/api/v1/tracks/${encodeURIComponent(track.id)}/tags`, {
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
	      return normalizeTrackResult(result);
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

	listQueryHistory(trackId: string): Promise<MatchQueryHistory[]> {
	  return request<MatchQueryHistory[]>(`/api/v1/matches/tracks/${encodeURIComponent(trackId)}/query-history`);
	},

	listProviders(): Promise<ProviderConfig[]> {
      return request<ProviderConfig[]>('/api/v1/providers');
    },

    listRevisions(limit = 100): Promise<Revision[]> {
      const query = limit > 0 && limit < 100 ? `?limit=${limit}` : '';
      return request<Revision[]>(`/api/v1/revisions${query}`);
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
      }).then(normalizeTrackResult);
    },

	writeArtwork(track: Track, file: File, maxSize = 0): Promise<ArtworkWriteResult> {
	  const query = maxSize > 0 ? `?max_size=${maxSize}` : '';
	  return request<ArtworkWriteResult>(`/api/v1/tracks/${encodeURIComponent(track.id)}/artwork/0${query}`, {
		method: 'PUT',
		headers: {'Content-Type': file.type || 'application/octet-stream', 'If-Match': `"${track.revision}"`},
		body: file,
	  }).then(normalizeTrackResult);
	},

	async readArtwork(track: Track, index = 0): Promise<File> {
	  const token = storedAuthToken();
	  const response = await fetcher(`/api/v1/tracks/${encodeURIComponent(track.id)}/artwork/${index}?revision=${encodeURIComponent(track.revision)}`, {
	    headers: {Accept: 'image/*', ...(token ? {Authorization: `Bearer ${token}`} : {})},
	  });
	  if (!response.ok) throw new APIError(response.status, 'artwork_read_failed', `读取封面失败（HTTP ${response.status}）`);
	  const blob = await response.blob();
	  return new File([blob], `${track.fileName}.cover`, {type: blob.type || 'application/octet-stream'});
	},

	updateProvider(providerId: string, enabled?: boolean, config?: Record<string, string>): Promise<ProviderConfig> {
	  const body: Record<string, unknown> = {};
	  if (enabled !== undefined) body.enabled = enabled;
	  if (config !== undefined) body.config = config;
	  return request<ProviderConfig>(`/api/v1/providers/${encodeURIComponent(providerId)}`, {
		method: 'PATCH', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body),
	  });
	},

	testProvider(providerId: string, query?: CandidateSearchQuery): Promise<ProviderTestResponse> {
	  const init: RequestInit = {method: 'POST'};
	  if (query) {
		init.headers = {'Content-Type': 'application/json'};
		init.body = JSON.stringify({query, limit: 5, probeArtwork: true});
	  }
	  return request<ProviderTestResponse>(`/api/v1/providers/${encodeURIComponent(providerId)}/test`, init);
	},

	deleteArtwork(track: Track): Promise<ArtworkWriteResult> {
	  return request<ArtworkWriteResult>(`/api/v1/tracks/${encodeURIComponent(track.id)}/artwork/0`, {
		method: 'DELETE',
		headers: {'If-Match': `"${track.revision}"`},
	  }).then(normalizeTrackResult);
	},

	applyCandidateArtwork(track: Track, candidateId: string, maxSize = 0): Promise<ArtworkWriteResult> {
	  return request<ArtworkWriteResult>(`/api/v1/matches/tracks/${encodeURIComponent(track.id)}/artwork`, {
		method: 'POST',
		headers: {'Content-Type': 'application/json', 'If-Match': `"${track.revision}"`},
		body: JSON.stringify({candidateId, baseRevision: track.revision, maxSize, dryRun: false}),
	  }).then(normalizeTrackResult);
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
  if (patch.comment !== track.comment) result.comment = stringOperation(patch.comment);
  if (!arraysEqual(patch.composers, track.composers)) result.composers = stringsOperation(patch.composers);
  if (patch.conductor !== track.conductor) result.conductor = stringOperation(patch.conductor);
  if (!arraysEqual(patch.lyricists, track.lyricists)) result.lyricists = stringsOperation(patch.lyricists);
  if (patch.copyright !== track.copyright) result.copyright = stringOperation(patch.copyright);
  if (patch.bpm !== track.bpm) result.bpm = numberOperation(patch.bpm);
  if (patch.isrc !== track.isrc) result.isrc = stringOperation(patch.isrc);
  if (patch.musicbrainzTrackId !== track.musicbrainzTrackId) result.musicbrainzTrackId = stringOperation(patch.musicbrainzTrackId);
  if (patch.musicbrainzReleaseId !== track.musicbrainzReleaseId) result.musicbrainzReleaseId = stringOperation(patch.musicbrainzReleaseId);
  if (!arraysEqual(patch.musicbrainzArtistIds, track.musicbrainzArtistIds)) result.musicbrainzArtistIds = stringsOperation(patch.musicbrainzArtistIds);
  if (patch.acoustidId !== track.acoustidId) result.acoustidId = stringOperation(patch.acoustidId);
  if (patch.acoustidFingerprint !== track.acoustidFingerprint) result.acoustidFingerprint = stringOperation(patch.acoustidFingerprint);
  return result;
}

function stringOperation(value = ''): FieldOperation<string> {
  const normalized = value.trim();
  return normalized ? {op: 'set', value: normalized} : {op: 'delete'};
}

function stringsOperation(value: string[] = []): FieldOperation<string[]> {
  const normalized = value.map((item) => item.trim()).filter(Boolean);
  return normalized.length > 0 ? {op: 'set', value: normalized} : {op: 'delete'};
}

function numberOperation(value?: number): FieldOperation<number> {
  return value === undefined ? {op: 'delete'} : {op: 'set', value};
}

function arraysEqual(left: string[] = [], right: string[] = []): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}
