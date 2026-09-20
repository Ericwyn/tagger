import type {
	LibrarySummary,
	LibraryEvent,
	LibraryReconcileResult,
	DirectoryProbe,
	Job,
  MatchCandidate,
	CandidateField,
	MatchItem,
  ArtworkWriteResult,
  ProviderConfig,
  ProviderTestResponse,
  RestorePreview,
	RestoreResult,
	RevisionSnapshot,
	Revision,
	RevisionDiff,
	RawTagsResponse,
	Track,
	TagHint,
	TagIssue,
	TrackPatch,
	WriteSelection,
	BatchEditItem,
	BatchEditOperation,
	BatchEditSelection,
	OrganizePreviewItem,
	UpdateProvenance,
	CandidateSearchQuery,
	MatchQueryHistory,
	LyricsSidecarResponse,
	LyricsSidecarWriteResult,
	ScanMode,
	TrackPage,
	TrackQuery,
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
	writeHistory?: boolean;
	batchTrackLimit?: number;
  storage?: SystemStorageInfo;
}

export interface SystemStorageInfo {
  dataDir?: string;
  databaseBytes: number;
  artworkCacheBytes: number;
  providerCacheEntries: number;
  artworkReferenceEntries: number;
  totalBytes: number;
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

function numberValue(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

function candidateFieldRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' ? value as Record<string, unknown> : {};
}

function normalizeCandidateSources(value: unknown): NonNullable<CandidateField['sources']> {
  if (!Array.isArray(value)) return [];
  return value.flatMap((item) => {
    if (!item || typeof item !== 'object') return [];
    const raw = item as Record<string, unknown>;
    const providerId = stringValue(raw.providerId);
    const candidateId = stringValue(raw.candidateId);
    if (!providerId || !candidateId) return [];
    return [{
      providerId,
      providerName: stringValue(raw.providerName),
      candidateId,
      ...(stringValue(raw.externalId) ? {externalId: stringValue(raw.externalId)} : {}),
    }];
  });
}

function normalizeStringCandidateField(value: unknown): CandidateField<string> {
  const raw = candidateFieldRecord(value);
  const sources = normalizeCandidateSources(raw.sources);
  return {
    value: stringValue(raw.value),
    source: stringValue(raw.source),
    ...(sources.length > 0 ? {sources} : {}),
    ...(typeof raw.confidence === 'number' ? {confidence: raw.confidence} : {}),
    ...(raw.derived === true ? {derived: true} : {}),
  };
}

function normalizeNumberCandidateField(value: unknown): CandidateField<number> {
  const raw = candidateFieldRecord(value);
  const sources = normalizeCandidateSources(raw.sources);
  return {
    value: numberValue(raw.value),
    source: stringValue(raw.source),
    ...(sources.length > 0 ? {sources} : {}),
    ...(typeof raw.confidence === 'number' ? {confidence: raw.confidence} : {}),
    ...(raw.derived === true ? {derived: true} : {}),
  };
}

function normalizeStringsCandidateField(value: unknown): CandidateField<string[]> {
  const raw = candidateFieldRecord(value);
  const sources = normalizeCandidateSources(raw.sources);
  return {
    value: stringArray(raw.value),
    source: stringValue(raw.source),
    ...(sources.length > 0 ? {sources} : {}),
    ...(typeof raw.confidence === 'number' ? {confidence: raw.confidence} : {}),
    ...(raw.derived === true ? {derived: true} : {}),
  };
}

// Candidate payloads live in SQLite as JSON and may predate the current Go
// shape. Normalize nullable slice zero-values at the API boundary so one old or
// sparse candidate can never blank the React tree by exposing `null.length`.
export function normalizeMatchCandidate(candidate: MatchCandidate): MatchCandidate {
  const raw = candidate as MatchCandidate & Record<string, unknown>;
  const contributors = normalizeCandidateSources(raw.contributors);
  const artworkSources = normalizeCandidateSources(raw.artworkSource ? [raw.artworkSource] : []);
	const evidence = raw.evidence && typeof raw.evidence === 'object' ? raw.evidence as unknown as Record<string, unknown> : undefined;
  return {
    ...candidate,
    memberCandidateIds: stringArray(raw.memberCandidateIds),
    contributors,
    ...(artworkSources[0] ? {artworkSource: artworkSources[0]} : {}),
    title: normalizeStringCandidateField(raw.title),
    artists: normalizeStringsCandidateField(raw.artists),
    album: normalizeStringCandidateField(raw.album),
    albumArtists: normalizeStringsCandidateField(raw.albumArtists),
    year: normalizeNumberCandidateField(raw.year),
    trackNumber: normalizeNumberCandidateField(raw.trackNumber),
    trackTotal: normalizeNumberCandidateField(raw.trackTotal),
    discNumber: normalizeNumberCandidateField(raw.discNumber),
    discTotal: normalizeNumberCandidateField(raw.discTotal),
    durationSeconds: normalizeNumberCandidateField(raw.durationSeconds),
    genres: normalizeStringsCandidateField(raw.genres),
    comment: normalizeStringCandidateField(raw.comment),
    composers: normalizeStringsCandidateField(raw.composers),
    conductor: normalizeStringCandidateField(raw.conductor),
    lyricists: normalizeStringsCandidateField(raw.lyricists),
    copyright: normalizeStringCandidateField(raw.copyright),
    bpm: normalizeNumberCandidateField(raw.bpm),
    isrc: normalizeStringCandidateField(raw.isrc),
    musicbrainzTrackId: normalizeStringCandidateField(raw.musicbrainzTrackId),
    musicbrainzReleaseId: normalizeStringCandidateField(raw.musicbrainzReleaseId),
    musicbrainzArtistIds: normalizeStringsCandidateField(raw.musicbrainzArtistIds),
    acoustidId: normalizeStringCandidateField(raw.acoustidId),
    acoustidFingerprint: normalizeStringCandidateField(raw.acoustidFingerprint),
    ...(raw.lyrics && typeof raw.lyrics === 'object' ? {lyrics: normalizeStringCandidateField(raw.lyrics)} : {lyrics: undefined}),
    matchReasons: stringArray(raw.matchReasons),
    ...(evidence ? {evidence: {
      identityScore: numberValue(evidence.identityScore),
      releaseScore: numberValue(evidence.releaseScore),
      completenessScore: numberValue(evidence.completenessScore),
      assetQuality: numberValue(evidence.assetQuality),
      margin: numberValue(evidence.margin),
      level: stringValue(evidence.level),
      sourceCount: numberValue(evidence.sourceCount),
      conflicts: stringArray(evidence.conflicts),
      algorithmVersion: stringValue(evidence.algorithmVersion),
    }} : {}),
  };
}

function normalizeMatchItem(item: MatchItem): MatchItem {
	const raw = item as MatchItem & Record<string, unknown>;
	const candidates = Array.isArray(raw.candidates)
	  ? raw.candidates
		.filter((candidate): candidate is MatchCandidate => Boolean(candidate && typeof candidate === 'object'))
		.map(normalizeMatchCandidate)
	  : [];
	return {
	  ...item,
	  candidates,
	  ...(raw.reviewFields == null ? {reviewFields: raw.reviewFields as null | undefined} : {reviewFields: stringArray(raw.reviewFields)}),
	};
}

const tagIssueValues: TagIssue[] = [
  'missing-embedded-title', 'missing-embedded-artist', 'missing-embedded-album',
  'missing-embedded-album-artist', 'suspicious-album-artist',
];

function normalizeTagIssues(value: unknown): TagIssue[] {
  return stringArray(value).filter((item): item is TagIssue => tagIssueValues.includes(item as TagIssue));
}

function normalizeTagHints(value: unknown): TagHint[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((item) => {
    if (!item || typeof item !== 'object') return [];
    const raw = item as Record<string, unknown>;
    const source = raw.source === 'directory' ? 'directory' : raw.source === 'filename' ? 'filename' : undefined;
    const pattern = raw.pattern === 'filename-title' || raw.pattern === 'title-artist' || raw.pattern === 'artist-title' || raw.pattern === 'directory-artist-album'
      ? raw.pattern
      : undefined;
    if (!source || !pattern) return [];
    return [{
      ...(stringValue(raw.title) ? {title: stringValue(raw.title)} : {}),
      artists: stringArray(raw.artists),
      ...(stringValue(raw.album) ? {album: stringValue(raw.album)} : {}),
      albumArtists: stringArray(raw.albumArtists),
      source,
      pattern,
    } satisfies TagHint];
  });
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
    tagHints: normalizeTagHints(raw.tagHints),
    tagIssues: normalizeTagIssues(raw.tagIssues),
    ...(raw.syncState === 'indexed' || raw.syncState === 'draft' || raw.syncState === 'error' ? {syncState: raw.syncState} : {}),
  };
}

export function normalizeLibrary(library: LibrarySummary): LibrarySummary {
  const raw = library as LibrarySummary & Record<string, unknown>;
  const folders = Array.isArray(raw.folders)
    ? raw.folders.filter((item): item is LibrarySummary['folders'][number] => Boolean(item && typeof item === 'object'))
    : [];
  return {
    ...library,
    name: stringValue(raw.name) || stringValue(raw.rootLabel) || '未命名曲库',
    rootLabel: stringValue(raw.rootLabel),
    ...(stringValue(raw.rootPath) ? {rootPath: stringValue(raw.rootPath)} : {}),
    trackCount: typeof raw.trackCount === 'number' ? raw.trackCount : 0,
    folderCount: typeof raw.folderCount === 'number' ? raw.folderCount : folders.length,
    folders,
    ...(raw.watchMode === 'auto' || raw.watchMode === 'events' || raw.watchMode === 'poll' ? {watchMode: raw.watchMode} : {}),
    ...(raw.watchState === 'healthy' || raw.watchState === 'degraded' || raw.watchState === 'polling' ? {watchState: raw.watchState} : {}),
  };
}

function normalizeRevisionDiff(value: unknown): RevisionDiff[] {
  if (!Array.isArray(value)) return [];
  return value
    .filter((item): item is Record<string, unknown> => Boolean(item && typeof item === 'object'))
    .map((item) => ({
      field: stringValue(item.field),
      operation: item.operation === 'delete' || item.operation === 'keep' ? item.operation : 'set',
      before: item.before,
      after: item.after,
    }));
}

export function normalizeRevision(revision: Revision): Revision {
  const raw = revision as Revision & Record<string, unknown>;
  return {
    ...revision,
    fields: stringArray(raw.fields),
    diff: normalizeRevisionDiff(raw.diff),
  };
}

function normalizeRestorePreview(value: RestorePreview): RestorePreview {
  const raw = value as RestorePreview & {preview?: Record<string, unknown> | null};
  const preview = raw.preview ?? {};
  return {
    ...value,
    preview: {
      ...(value.preview ?? {}),
      diff: normalizeRevisionDiff(preview.diff),
      warnings: stringArray(preview.warnings),
    },
  };
}

function normalizeRevisionSnapshot(value: RevisionSnapshot): RevisionSnapshot {
  const raw = value as RevisionSnapshot & Record<string, unknown>;
  const tags = raw.tags && typeof raw.tags === 'object' ? raw.tags as Record<string, unknown> : {};
  return {
    ...value,
    hasTagSnapshot: Boolean(raw.hasTagSnapshot),
    tags: Object.fromEntries(Object.entries(tags).map(([key, values]) => [key, stringArray(values)])),
  };
}

function normalizeTrackResult<T extends {track: Track}>(result: T): T {
  return {...result, track: normalizeTrack(result.track)};
}

function trackQueryParams(query: TrackQuery, cursor = '', limit = 100): string {
  const params = new URLSearchParams();
  if (limit > 0) params.set('limit', String(limit));
  if (cursor) params.set('cursor', cursor);
  if (query.q?.trim()) params.set('q', query.q.trim());
  if (query.folderId) params.set('folder_id', query.folderId);
  if (query.folderPath) params.set('folder_path', query.folderPath);
  if (query.includeSubfolders) params.set('include_subfolders', 'true');
  if (query.health) params.set('health', query.health);
  if (query.format) params.set('format', query.format);
  if (query.sort) params.set('sort', query.sort);
  const encoded = params.toString();
  return encoded ? `?${encoded}` : '';
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

    clearRuntimeCache(): Promise<SystemStorageInfo> {
      return request<{storage: SystemStorageInfo}>('/api/v1/system/cache/clear', {method: 'POST'}).then((result) => result.storage);
    },

    updateSystemSettings(settings: {historyRetention?: number; writeHistory?: boolean; batchTrackLimit?: number}): Promise<{historyRetention: number; writeHistory: boolean; batchTrackLimit: number}> {
	  return request<{historyRetention: number; writeHistory: boolean; batchTrackLimit: number}>('/api/v1/system/settings', {
        method: 'PATCH',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(settings),
      });
    },

	    async getLibrary(): Promise<LibrarySummary> {
	      const libraries = await request<LibrarySummary[]>('/api/v1/libraries');
	      const current = libraries.find((library) => library.active) ?? libraries[0];
	      if (!current) throw new APIError(404, 'library_not_found', '尚未配置音乐曲库');
      return normalizeLibrary(current);
    },

    listLibraries(): Promise<LibrarySummary[]> {
      return request<LibrarySummary[]>('/api/v1/libraries').then((items) => items.map(normalizeLibrary));
	    },

	    registerLibrary(path: string): Promise<Job> {
	      return request<Job>('/api/v1/libraries', {
	        method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({path}),
	      });
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

	async listTrackPage(query: TrackQuery = {}, cursor = '', limit = 100, signal?: AbortSignal): Promise<TrackPage> {
	  const result = await request<TrackPage>(`/api/v1/tracks${trackQueryParams(query, cursor, limit)}`, {signal});
	  return {...result, tracks: (result.tracks ?? []).map(normalizeTrack)};
	},

	async listTracks(): Promise<Track[]> {
	  return (await this.listTrackPage({}, '', 100)).tracks;
	},

	async resolveTracks(resolveRequest: {ids?: string[]; query?: TrackQuery}): Promise<{tracks: Track[]; total: number}> {
	  const result = await request<{tracks: Track[]; total: number}>('/api/v1/tracks/resolve', {
	    method: 'POST',
	    headers: {'Content-Type': 'application/json'},
	    body: JSON.stringify(resolveRequest),
	  });
	  return {...result, tracks: (result.tracks ?? []).map(normalizeTrack)};
	},

	rescanTrack(trackId: string): Promise<Track> {
	  return request<Track>(`/api/v1/tracks/${encodeURIComponent(trackId)}/scan`, {method: 'POST'}).then(normalizeTrack);
	},

		async rescanLibrary(libraryId: string, mode: ScanMode = 'quick', targets: string[] = []): Promise<Job> {
	  return request<Job>(`/api/v1/libraries/${encodeURIComponent(libraryId)}/scans`, {
	    method: 'POST', headers: {'Content-Type': 'application/json'},
	    body: JSON.stringify({mode, targets}),
	  });
		},

		reconcileLibrary(libraryId: string, folderPath = ''): Promise<LibraryReconcileResult> {
		  return request<LibraryReconcileResult>(`/api/v1/libraries/${encodeURIComponent(libraryId)}/reconcile`, {
			method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({folderPath}),
		  });
		},

		subscribeLibraryEvents(libraryId: string, onEvent: (event: LibraryEvent) => void): () => void {
		  if (typeof EventSource === 'undefined') return () => undefined;
		  const source = new EventSource(`/api/v1/libraries/${encodeURIComponent(libraryId)}/events`);
		  const handler = (event: Event) => {
			try {
			  onEvent(JSON.parse((event as MessageEvent<string>).data) as LibraryEvent);
			} catch {
			  // REST snapshots remain authoritative after malformed or lost events.
			}
		  };
		  source.addEventListener('library', handler);
		  return () => {
			source.removeEventListener('library', handler);
			source.close();
		  };
		},

	async deleteLibrary(libraryId: string): Promise<{id: string; deleted: boolean}> {
	  return request<{id: string; deleted: boolean}>(`/api/v1/libraries/${encodeURIComponent(libraryId)}`, {method: 'DELETE'});
	},

	async purgeMissing(libraryId: string): Promise<{removed: number}> {
	  return request<{removed: number}>(`/api/v1/libraries/${encodeURIComponent(libraryId)}/missing/purge`, {method: 'POST'});
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
		body: JSON.stringify({trackIds, providerIds, limit: 2}),
	  });
	},

	listMatchItems(jobId: string): Promise<MatchItem[]> {
	  return request<MatchItem[]>(`/api/v1/jobs/${encodeURIComponent(jobId)}/matches`).then((items) => Array.isArray(items) ? items.map(normalizeMatchItem) : []);
	},

	updateMatchItem(jobId: string, trackId: string, state: 'review' | 'accepted' | 'skipped', selectedCandidateId?: string, fields?: string[], artwork?: boolean, artworkMaxSize?: number): Promise<MatchItem> {
	  return request<MatchItem>(`/api/v1/matches/jobs/${encodeURIComponent(jobId)}/items/${encodeURIComponent(trackId)}`, {
		method: 'PATCH',
		headers: {'Content-Type': 'application/json'},
		body: JSON.stringify({state, selectedCandidateId, fields, artwork, artworkMaxSize}),
	  }).then(normalizeMatchItem);
	},

	rematchMatchItem(jobId: string, trackId: string, query?: CandidateSearchQuery, providerIds: string[] = []): Promise<MatchItem> {
	  return request<{item: MatchItem; providers: Record<string, unknown>}>(`/api/v1/matches/jobs/${encodeURIComponent(jobId)}/items/${encodeURIComponent(trackId)}/rematch`, {
		method: 'POST',
		headers: {'Content-Type': 'application/json'},
		body: JSON.stringify({query, providerIds, limitPerProvider: 5}),
	  }).then((result) => normalizeMatchItem(result.item));
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

	previewOrganize(items: BatchEditSelection[]): Promise<OrganizePreviewItem[]> {
		return request<OrganizePreviewItem[]>('/api/v1/tracks/organize-preview', {
			method: 'POST', headers: {'Content-Type': 'application/json'},
			body: JSON.stringify({items, moveLyricsSidecar: true}),
		});
	},

	createOrganizeJob(items: BatchEditSelection[]): Promise<Job> {
		return request<Job>('/api/v1/tracks/organize', {
			method: 'POST', headers: {'Content-Type': 'application/json'},
			body: JSON.stringify({items, moveLyricsSidecar: true}),
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
	      return (result.candidates ?? []).map(normalizeMatchCandidate);
    },

	listQueryHistory(trackId: string): Promise<MatchQueryHistory[]> {
	  return request<MatchQueryHistory[]>(`/api/v1/matches/tracks/${encodeURIComponent(trackId)}/query-history`);
	},

	listProviders(): Promise<ProviderConfig[]> {
      return request<ProviderConfig[]>('/api/v1/providers');
    },

    listRevisions(limit = 100): Promise<Revision[]> {
      const query = limit > 0 && limit < 100 ? `?limit=${limit}` : '';
      return request<Revision[]>(`/api/v1/revisions${query}`).then((items) => (items ?? []).map(normalizeRevision));
    },

    previewRevisionRestore(revisionId: string, baseRevision: string): Promise<RestorePreview> {
      return request<RestorePreview>(`/api/v1/revisions/${encodeURIComponent(revisionId)}/restore-preview`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json', 'If-Match': `"${baseRevision}"`},
        body: JSON.stringify({baseRevision, target: 'before'}),
      }).then(normalizeRestorePreview);
    },

	    restoreRevision(revisionId: string, baseRevision: string): Promise<RestoreResult> {
      return request<RestoreResult>(`/api/v1/revisions/${encodeURIComponent(revisionId)}/restore`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json', 'If-Match': `"${baseRevision}"`},
        body: JSON.stringify({baseRevision, target: 'before'}),
      }).then(normalizeTrackResult);
	    },

    getRevisionSnapshot(revisionId: string, baseRevision: string): Promise<RevisionSnapshot> {
      return request<RevisionSnapshot>(`/api/v1/revisions/${encodeURIComponent(revisionId)}/snapshot`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json', 'If-Match': `"${baseRevision}"`},
        body: JSON.stringify({baseRevision, target: 'before'}),
      }).then(normalizeRevisionSnapshot);
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

	resetProvider(providerId: string): Promise<ProviderConfig> {
		return request<ProviderConfig>(`/api/v1/providers/${encodeURIComponent(providerId)}/reset`, {
			method: 'POST',
		});
	},

	testProvider(providerId: string, query?: CandidateSearchQuery): Promise<ProviderTestResponse> {
	  const init: RequestInit = {method: 'POST'};
	  if (query) {
		init.headers = {'Content-Type': 'application/json'};
		init.body = JSON.stringify({query, limit: 5, probeArtwork: true});
	  }
	  return request<ProviderTestResponse>(`/api/v1/providers/${encodeURIComponent(providerId)}/test`, init).then((result) => ({
		...result,
		...(Array.isArray(result.candidates) ? {candidates: result.candidates.map(normalizeMatchCandidate)} : {}),
	  }));
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
