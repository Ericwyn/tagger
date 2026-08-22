import * as mock from '@/mock/api';
import {createRealAPI, type ScanResult} from '@/api/real';
import type {
  Job,
  LibrarySummary,
  MatchCandidate,
  ProviderConfig,
  RestorePreview,
  RestoreResult,
  Revision,
  Track,
  TrackPatch,
  UpdateProvenance,
} from '@/types';

const configuredMode = import.meta.env.VITE_API_MODE;
export const apiReadMode: 'mock' | 'real' = configuredMode === 'mock' || import.meta.env.MODE === 'test'
  ? 'mock'
  : 'real';

const real = createRealAPI();
let realTrackCache = new Map<string, Track>();

export async function getLibrary(): Promise<LibrarySummary> {
  return apiReadMode === 'mock' ? mock.getLibrary() : real.getLibrary();
}

export async function listTracks(): Promise<Track[]> {
  if (apiReadMode === 'mock') return mock.listTracks();
  const tracks = await real.listTracks();
  realTrackCache = new Map(tracks.map((track) => [track.id, track]));
  return tracks;
}

export async function rescanLibrary(libraryId: string): Promise<ScanResult | null> {
  if (apiReadMode === 'mock') {
    await mock.getLibrary();
    return null;
  }
  return real.rescanLibrary(libraryId);
}

export async function updateTrack(trackId: string, patch: TrackPatch, provenance?: UpdateProvenance): Promise<Track> {
  if (apiReadMode === 'mock') return mock.updateTrack(trackId, patch);
  const current = realTrackCache.get(trackId);
  if (!current) throw new Error('track_not_found');
  const result = await real.updateTrack(current, patch, provenance);
  realTrackCache.set(trackId, result.track);
  return result.track;
}

// The job view remains backed by the mock strategy until its persistent Go
// module is connected. Revision history is served by SQLite in real mode.
export function searchCandidates(track: Track): Promise<MatchCandidate[]> {
  return apiReadMode === 'mock' ? mock.searchCandidates(track) : real.searchCandidates(track);
}

export function listProviders(): Promise<ProviderConfig[]> {
  return apiReadMode === 'mock' ? mock.listProviders() : real.listProviders();
}

export function listJobs(): Promise<Job[]> {
  return mock.listJobs();
}

export function listRevisions(): Promise<Revision[]> {
  return apiReadMode === 'mock' ? mock.listRevisions() : real.listRevisions();
}

export function previewRevisionRestore(revision: Revision): Promise<RestorePreview> {
  if (apiReadMode === 'mock' || !revision.currentRevision) {
    return Promise.reject(new Error('历史恢复仅在真实后端模式可用'));
  }
  return real.previewRevisionRestore(revision.id, revision.currentRevision);
}

export function restoreRevision(revision: Revision, preview: RestorePreview): Promise<RestoreResult> {
  if (apiReadMode === 'mock' || !revision.currentRevision) {
    return Promise.reject(new Error('历史恢复仅在真实后端模式可用'));
  }
  return real.restoreRevision(revision.id, preview.preview.currentRevision);
}

export function artworkURL(track: Track): string | undefined {
  if (apiReadMode === 'mock' || track.artworkCount === 0) return undefined;
  return `/api/v1/tracks/${encodeURIComponent(track.id)}/artwork/0?revision=${encodeURIComponent(track.revision)}`;
}

export async function updateArtwork(trackId: string, file: File | null): Promise<Track> {
  if (apiReadMode === 'mock') return mock.updateArtwork(trackId, file);
  const current = realTrackCache.get(trackId);
  if (!current) throw new Error('track_not_found');
  const result = file ? await real.writeArtwork(current, file) : await real.deleteArtwork(current);
  realTrackCache.set(trackId, result.track);
  return result.track;
}

export async function applyCandidateArtwork(trackId: string, candidateId: string): Promise<Track> {
  if (apiReadMode === 'mock') return mock.updateArtwork(trackId, new File([], 'provider-cover.jpg', {type: 'image/jpeg'}));
  const current = realTrackCache.get(trackId);
  if (!current) throw new Error('track_not_found');
  const result = await real.applyCandidateArtwork(current, candidateId);
  realTrackCache.set(trackId, result.track);
  return result.track;
}
