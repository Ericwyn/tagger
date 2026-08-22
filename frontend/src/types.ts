export type PageID = 'library' | 'review' | 'jobs' | 'history' | 'settings';
export type InspectorTab = 'tags' | 'artwork' | 'lyrics' | 'technical' | 'history';
export type TrackFormat = 'flac' | 'mp3' | 'wav';
export type CoverTone = 'vermilion' | 'moss' | 'cobalt' | 'sand' | 'charcoal' | 'jade';
export type TrackHealth = 'complete' | 'missing-artwork' | 'missing-lyrics' | 'needs-review' | 'parse-error';

export interface TrackProperties {
  container: string;
  codec: string;
  bitrateKbps: number;
  sampleRateHz: number;
  bitDepth: number;
  channels: number;
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
  artworkCount: number;
  coverTone: CoverTone;
  health: TrackHealth;
  properties: TrackProperties;
  writable: boolean;
  revision: string;
  modifiedAt: string;
  parseError?: string;
  syncState?: 'indexed' | 'draft';
}

export interface FolderNode {
  id: string;
  name: string;
  count: number;
  parentId?: string;
  children?: FolderNode[];
}

export interface LibrarySummary {
  id: string;
  name: string;
  rootLabel: string;
  trackCount: number;
  folderCount: number;
  writable: boolean;
  lastScanLabel: string;
  folders: FolderNode[];
}

export interface CandidateField<T = string | number | string[]> {
  value: T;
  source: string;
}

export interface MatchCandidate {
  id: string;
  providerId: string;
  providerName: string;
  externalId: string;
  title: CandidateField<string>;
  artists: CandidateField<string[]>;
  album: CandidateField<string>;
  albumArtists: CandidateField<string[]>;
  year: CandidateField<number>;
  trackNumber: CandidateField<number>;
  trackTotal: CandidateField<number>;
  discNumber: CandidateField<number>;
  durationSeconds: CandidateField<number>;
  genres: CandidateField<string[]>;
  hasLyrics: boolean;
  hasArtwork: boolean;
  coverTone: CoverTone;
  score: number;
  scoreLabel: string;
  matchReasons: string[];
}

export type JobState = 'running' | 'review' | 'waiting' | 'succeeded' | 'partial';

export interface Job {
  id: string;
  kind: 'scan' | 'match' | 'write';
  title: string;
  detail: string;
  state: JobState;
  processed: number;
  total: number;
  succeeded: number;
  failed: number;
  startedAt: string;
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
}

export type ProviderHealth = 'ready' | 'degraded' | 'misconfigured' | 'disabled';

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
}
