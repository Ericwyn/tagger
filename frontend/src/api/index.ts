import * as mock from '@/mock/api';
import {createRealAPI} from '@/api/real';
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

export async function rescanLibrary(libraryId: string): Promise<Job | null> {
  if (apiReadMode === 'mock') {
    await mock.getLibrary();
    return null;
  }
  return real.rescanLibrary(libraryId);
}

export async function waitForJob(jobId: string, timeoutMs = 5 * 60_000): Promise<Job> {
  if (apiReadMode === 'mock') throw new Error('Mock 模式没有持久化任务');
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
	const job = await real.getJob(jobId);
	if (job.state === 'succeeded' || job.state === 'partial' || job.state === 'failed' || job.state === 'review') return job;
	await new Promise((resolve) => window.setTimeout(resolve, 350));
  }
  throw new Error('等待扫描任务超时');
}

export async function updateTrack(trackId: string, patch: TrackPatch, provenance?: UpdateProvenance): Promise<Track> {
  if (apiReadMode === 'mock') return mock.updateTrack(trackId, patch);
  const current = realTrackCache.get(trackId);
  if (!current) throw new Error('track_not_found');
  const result = await real.updateTrack(current, patch, provenance);
  realTrackCache.set(trackId, result.track);
  return result.track;
}

export function searchCandidates(track: Track): Promise<MatchCandidate[]> {
  return apiReadMode === 'mock' ? mock.searchCandidates(track) : real.searchCandidates(track);
}

export function listProviders(): Promise<ProviderConfig[]> {
  return apiReadMode === 'mock' ? mock.listProviders() : real.listProviders();
}

export function listJobs(): Promise<Job[]> {
	return apiReadMode === 'mock' ? mock.listJobs() : real.listJobs();
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
