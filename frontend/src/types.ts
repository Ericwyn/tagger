export type PageID = 'library' | 'review' | 'jobs' | 'history' | 'settings';
export type InspectorTab = 'tags' | 'artwork' | 'lyrics' | 'technical' | 'history';
export type TrackFormat = 'flac' | 'mp3' | 'wav' | 'ogg';
export type CoverTone = 'vermilion' | 'moss' | 'cobalt' | 'sand' | 'charcoal' | 'jade';
export type TrackHealth = 'complete' | 'tag-compatibility' | 'missing-artwork' | 'missing-lyrics' | 'needs-review' | 'parse-error' | 'missing';
export type TagIssue = 'missing-embedded-title' | 'missing-embedded-artist' | 'missing-embedded-album' | 'missing-embedded-album-artist' | 'suspicious-album-artist';

export interface TagHint {
  title?: string;
  artists: string[];
  album?: string;
  albumArtists: string[];
  source: 'filename' | 'directory';
  pattern: 'filename-title' | 'title-artist' | 'artist-title' | 'directory-artist-album';
}
export const historyRetentionOptions = [3, 5, 10, 20] as const;
export type HistoryRetention = typeof historyRetentionOptions[number];
export const defaultBatchTrackLimit = 2000;
export const minBatchTrackLimit = 1;
export const maxBatchTrackLimit = 100000;

export interface TrackProperties {
  container: string;
  codec: string;
  bitrateKbps: number;
  sampleRateHz: number;
  bitDepth: number;
  channels: number;
}

export interface SidecarInfo {
  exists: boolean;
  revision?: string;
  sizeBytes?: number;
  modifiedAt?: string;
}

export interface Track {
  id: string;
  fileName: string;
  relativePath: string;
  folderId: string;
  format: TrackFormat;
  sizeBytes: number;
  durationSeconds: number;
  title: string;
  artists: string[];
  album: string;
  albumArtists: string[];
  trackNumber?: number;
  trackTotal?: number;
  discNumber?: number;
  discTotal?: number;
  year?: number;
  genres: string[];
  lyrics: string;
  comment: string;
  composers: string[];
  conductor: string;
  lyricists: string[];
  copyright: string;
  bpm?: number;
  isrc: string;
  musicbrainzTrackId: string;
  musicbrainzReleaseId: string;
  musicbrainzArtistIds: string[];
  acoustidId: string;
  acoustidFingerprint: string;
  tagHints: TagHint[];
  tagIssues: TagIssue[];
  lyricsSidecar?: SidecarInfo;
  artworkCount: number;
  artworkWidth?: number;
  artworkHeight?: number;
  artworkSizeBytes?: number;
  coverTone: CoverTone;
  health: TrackHealth;
  properties: TrackProperties;
  writable: boolean;
  revision: string;
  modifiedAt: string;
  parseError?: string;
  missing?: boolean;
  missingSince?: string;
  syncState?: 'indexed' | 'draft' | 'error';
}

export interface FolderNode {
  id: string;
  name: string;
  path?: string;
  count: number;
  parentId?: string;
  children?: FolderNode[];
}

export interface LibrarySummary {
  id: string;
  name: string;
  rootLabel: string;
  rootPath?: string;
  active?: boolean;
  trackCount: number;
  folderCount: number;
  writable: boolean;
  lastScanLabel: string;
  folders: FolderNode[];
  watchMode?: 'auto' | 'events' | 'poll';
  watchState?: 'healthy' | 'degraded' | 'polling';
}

export interface LibraryEvent {
  libraryId: string;
  generation: number;
  kind: 'snapshot' | 'inventory' | 'metadata' | 'watcher-state';
  paths?: string[];
  watchMode?: LibrarySummary['watchMode'];
  watchState?: LibrarySummary['watchState'];
}

export interface LibraryReconcileResult {
  generation: number;
  changed: boolean;
  pending?: string[];
  missing: number;
  warnings?: string[];
  job?: Job;
}

export type TrackSort = 'album' | 'title' | 'modified' | 'format';

export interface TrackQuery {
  q?: string;
  folderId?: string;
  folderPath?: string;
  includeSubfolders?: boolean;
  health?: TrackHealth;
  format?: TrackFormat;
  sort?: TrackSort;
}

export interface TrackPage {
  tracks: Track[];
  total: number;
  nextCursor?: string;
  hasMore: boolean;
}

export interface DirectoryProbe {
  path: string;
  name: string;
  readable: boolean;
  writable: boolean;
  audioFiles: number;
  folders: number;
  formats: Record<string, number>;
  warnings?: string[];
}

export type CandidateKind = 'source' | 'smart' | 'ai';

export interface CandidateSourceReference {
  providerId: string;
  providerName: string;
  candidateId: string;
  externalId?: string;
}

export interface CandidateField<T = string | number | string[]> {
  value: T;
  source: string;
  sources?: CandidateSourceReference[];
  confidence?: number;
  derived?: boolean;
}

export interface MatchEvidence {
  identityScore: number;
  releaseScore?: number;
  completenessScore?: number;
  assetQuality?: number;
  margin?: number;
  level: 'exact' | 'high' | 'review' | 'low' | string;
  sourceCount: number;
  conflicts?: string[];
  algorithmVersion?: string;
}

export interface MatchCandidate {
  id: string;
  kind?: CandidateKind;
  providerId: string;
  providerName: string;
  externalId: string;
  memberCandidateIds?: string[];
  contributors?: CandidateSourceReference[];
  artworkRefId?: string;
  artworkSource?: CandidateSourceReference;
  title: CandidateField<string>;
  artists: CandidateField<string[]>;
  album: CandidateField<string>;
  albumArtists: CandidateField<string[]>;
  year: CandidateField<number>;
  trackNumber: CandidateField<number>;
  trackTotal: CandidateField<number>;
  discNumber: CandidateField<number>;
  discTotal: CandidateField<number>;
  durationSeconds: CandidateField<number>;
  genres: CandidateField<string[]>;
  comment?: CandidateField<string>;
  composers?: CandidateField<string[]>;
  conductor?: CandidateField<string>;
  lyricists?: CandidateField<string[]>;
  copyright?: CandidateField<string>;
  bpm?: CandidateField<number>;
  isrc?: CandidateField<string>;
  musicbrainzTrackId?: CandidateField<string>;
  musicbrainzReleaseId?: CandidateField<string>;
  musicbrainzArtistIds?: CandidateField<string[]>;
  acoustidId?: CandidateField<string>;
  acoustidFingerprint?: CandidateField<string>;
  lyrics?: CandidateField<string>;
  hasLyrics: boolean;
  hasArtwork: boolean;
  coverTone: CoverTone;
  score: number;
  scoreLabel: string;
  matchReasons: string[];
  evidence?: MatchEvidence;
  recommended?: boolean;
  autoAccept?: boolean;
}

export interface CandidateSearchQuery {
  title: string;
  artists: string[];
  album: string;
  durationSeconds: number;
}

export interface MatchQueryHistory {
  id: string;
  trackId: string;
  query: CandidateSearchQuery;
  providerIds: string[];
  resultCount: number;
  createdAt: string;
}

export type JobState = 'running' | 'review' | 'waiting' | 'succeeded' | 'partial' | 'failed' | 'cancelled';

export interface Job {
  id: string;
  kind: 'scan' | 'match' | 'write' | 'batch_edit';
  title: string;
  detail: string;
  state: JobState;
  processed: number;
  total: number;
  succeeded: number;
  failed: number;
  startedAt: string;
  error?: string;
}

export type ScanMode = 'quick' | 'full' | 'targeted';

export interface MatchItem {
  id: string;
  jobId: string;
  trackId: string;
  state: 'review' | 'accepted' | 'skipped' | 'no_match' | 'failed' | 'written' | 'write_pending' | 'write_failed' | 'artwork_failed';
  candidates: MatchCandidate[];
  selectedCandidateId?: string;
  reviewFields?: string[] | null;
  reviewArtwork?: boolean;
  reviewArtworkMaxSize?: number;
  error?: string;
  updatedAt?: string;
}

export interface WriteSelection {
  trackId: string;
  candidateId: string;
  baseRevision: string;
  fields: string[];
  artwork?: boolean;
  artworkMaxSize?: number;
}

export type BatchEditMode = 'set' | 'append' | 'delete' | 'replace';

export interface BatchEditOperation {
  field: 'title' | 'artists' | 'album' | 'albumArtists' | 'year' | 'genres' | 'comment' | 'composers' | 'conductor' | 'lyricists' | 'copyright' | 'bpm' | 'isrc';
  mode: BatchEditMode;
  value: string;
  find?: string;
}

export interface BatchEditSelection {
  trackId: string;
  baseRevision: string;
}

export interface BatchArtworkInput {
  action: 'replace' | 'delete';
  file?: File;
  sourceTrack?: Track;
  maxSize?: number;
}

export type BatchEditItemState = 'pending' | 'written' | 'failed';

export interface BatchEditItem {
  id: string;
  jobId: string;
  trackId: string;
  state: BatchEditItemState;
  error?: string;
  diff: RevisionDiff[];
  updatedAt?: string;
}

export interface Revision {
  id: string;
  trackId: string;
  trackTitle: string;
  fileName: string;
  action: string;
  source: string;
  time: string;
  fields: string[];
  coverTone: CoverTone;
  diff?: RevisionDiff[];
  baseRevision?: string;
  resultRevision?: string;
  currentRevision?: string;
  beforeSidecar?: SidecarInfo;
  afterSidecar?: SidecarInfo;
}

export interface RevisionDiff {
  field: string;
  operation: 'keep' | 'set' | 'delete';
  before: unknown;
  after: unknown;
}

export type ProviderHealth = 'ready' | 'degraded' | 'misconfigured' | 'disabled';

export interface ProviderConfigField {
  key: string;
  label: string;
  type: 'text' | 'url' | 'password' | 'number' | 'boolean' | string;
  description?: string;
  placeholder?: string;
  secret?: boolean;
  required?: boolean;
  value?: string;
  configured?: boolean;
}

export interface ProviderConfig {
  id: string;
  name: string;
  shortName: string;
  description: string;
  capabilities: string[];
  health: ProviderHealth;
  enabled: boolean;
  experimental?: boolean;
  accent: string;
  quotaLabel: string;
  config?: ProviderConfigField[];
  configError?: string;
}

export interface ProviderTestResult {
  status: string;
  count: number;
  latencyMs: number;
  retryable?: boolean;
  retryAfterMs?: number;
  hint?: string;
  error?: string;
  cached?: boolean;
}

export interface ProviderTestLog {
  level: 'info' | 'success' | 'warning' | 'error' | string;
  stage: string;
  message: string;
  details?: Record<string, string | number | boolean | string[] | undefined>;
}

export interface ProviderTestResponse {
  provider: ProviderConfig;
  result: ProviderTestResult;
  query?: CandidateSearchQuery;
  candidates?: MatchCandidate[];
  logs?: ProviderTestLog[];
}

export interface TrackPatch {
  title: string;
  artists: string[];
  album: string;
  albumArtists: string[];
  trackNumber?: number;
  trackTotal?: number;
  discNumber?: number;
  discTotal?: number;
  year?: number;
  genres: string[];
  lyrics: string;
  comment: string;
  composers: string[];
  conductor: string;
  lyricists: string[];
  copyright: string;
  bpm?: number;
  isrc: string;
  musicbrainzTrackId: string;
  musicbrainzReleaseId: string;
  musicbrainzArtistIds: string[];
  acoustidId: string;
  acoustidFingerprint: string;
}

export interface RawTagsResponse {
  trackId: string;
  revision: string;
  tags: Record<string, string[]>;
}

export interface LyricsSidecarResponse {
  trackId: string;
  revision: string;
  sidecar?: SidecarInfo;
  content: string;
}

export interface LyricsSidecarWriteResult {
  track: Track;
  sidecar: {
    baseRevision: string;
    currentRevision: string;
    baseSidecarRevision: string;
    currentSidecarRevision?: string;
    dryRun: boolean;
    changed: boolean;
    before?: SidecarInfo;
    after?: SidecarInfo;
  };
}

export interface UpdateProvenance {
  providerId?: string;
  restoreRevisionId?: string;
}

export interface RevisionSnapshot {
  revisionId: string;
  trackId: string;
  target: 'before' | 'after';
  baseRevision: string;
  currentRevision: string;
  hasTagSnapshot: boolean;
  tags: Record<string, string[]>;
  artwork?: ArtworkAsset;
  sidecar?: SidecarInfo;
}

export interface RestoreDraftRequest {
  key: string;
  revisionId: string;
  trackId: string;
  patch?: TrackPatch;
  label: string;
}

export interface RestorePreview {
  revisionId: string;
  trackId: string;
  target: 'before' | 'after';
  preview: {
    baseRevision: string;
    currentRevision: string;
    dryRun: boolean;
    changed: boolean;
    diff: RevisionDiff[];
    warnings: string[];
  };
}

export interface RestoreResult {
  track: Track;
  write: RestorePreview['preview'];
  restoredRevisionId: string;
  target: 'before' | 'after';
}

export interface ArtworkAsset {
  mime: 'image/jpeg' | 'image/png' | 'image/webp';
  format: string;
  width: number;
  height: number;
  size: number;
  hash: string;
}

export interface ArtworkWriteResult {
  track: Track;
  write: {
    baseRevision: string;
    currentRevision: string;
    dryRun: boolean;
    changed: boolean;
    diff: RevisionDiff[];
    warnings: string[];
    before?: ArtworkAsset;
    after?: ArtworkAsset;
  };
}
