import * as mock from '@/mock/api';
import {createRealAPI} from '@/api/real';
import type {
  Job,
  LibrarySummary,
  MatchCandidate,
	MatchItem,
  ProviderConfig,
  RestorePreview,
  RestoreResult,
	Revision,
	RawTagsResponse,
	LyricsSidecarResponse,
	Track,
  TrackPatch,
	UpdateProvenance,
	WriteSelection,
	BatchEditItem,
	BatchEditOperation,
	BatchEditSelection,
	CandidateSearchQuery,
	MatchQueryHistory,
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
  throw new Error('等待后台任务超时');
}

export async function createMatchJob(trackIds: string[]): Promise<Job | null> {
  if (apiReadMode === 'mock') return null;
  return real.createMatchJob(trackIds);
}

export async function getJob(jobId: string): Promise<Job | null> {
  if (apiReadMode === 'mock') return null;
  return real.getJob(jobId);
}

export async function listMatchItems(jobId: string): Promise<MatchItem[]> {
  if (apiReadMode === 'mock') return [];
  return real.listMatchItems(jobId);
}

export async function updateMatchItem(jobId: string, trackId: string, state: 'review' | 'accepted' | 'skipped', selectedCandidateId?: string, fields?: string[], artwork?: boolean): Promise<MatchItem | null> {
  if (apiReadMode === 'mock') return null;
  return real.updateMatchItem(jobId, trackId, state, selectedCandidateId, fields, artwork);
}

export async function rematchMatchItem(jobId: string, trackId: string, track: Track, query?: CandidateSearchQuery, providerIds: string[] = []): Promise<MatchItem | null> {
  if (apiReadMode === 'mock') {
    const candidates = await mock.searchCandidates(track);
    return {
      id: `mock-${trackId}`,
      jobId,
      trackId,
      state: candidates.length > 0 ? 'review' : 'no_match',
      candidates,
      error: candidates.length > 0 ? undefined : '重新匹配没有返回候选',
    };
  }
  return real.rematchMatchItem(jobId, trackId, query, providerIds);
}

export async function createWriteJob(matchJobId: string, items: WriteSelection[]): Promise<Job | null> {
  if (apiReadMode === 'mock') return null;
  return real.createWriteJob(matchJobId, items);
}

export async function createBatchEditJob(items: BatchEditSelection[], operations: BatchEditOperation[], sequenceTracks: boolean): Promise<Job | null> {
  if (apiReadMode === 'mock') return null;
  return real.createBatchEditJob(items, operations, sequenceTracks);
}

export function listBatchEditItems(jobId: string): Promise<BatchEditItem[]> {
  return apiReadMode === 'mock' ? Promise.resolve([]) : real.listBatchEditItems(jobId);
}

export function getRawTags(trackId: string): Promise<RawTagsResponse | null> {
  return apiReadMode === 'mock' ? Promise.resolve(null) : real.getRawTags(trackId);
}

export function getLyricsSidecar(trackId: string): Promise<LyricsSidecarResponse | null> {
  return apiReadMode === 'mock' ? Promise.resolve(null) : real.getLyricsSidecar(trackId);
}

export async function updateTrack(trackId: string, patch: TrackPatch, provenance?: UpdateProvenance): Promise<Track> {
  if (apiReadMode === 'mock') return mock.updateTrack(trackId, patch);
  const current = realTrackCache.get(trackId);
  if (!current) throw new Error('track_not_found');
  const result = await real.updateTrack(current, patch, provenance);
  realTrackCache.set(trackId, result.track);
  return result.track;
}

export async function writeLyricsSidecar(trackId: string, content: string): Promise<Track> {
  if (apiReadMode === 'mock') return (await mock.writeLyricsSidecar(trackId, content)).track;
  const current = realTrackCache.get(trackId);
  if (!current) throw new Error('track_not_found');
  const result = await real.writeLyricsSidecar(current, content);
  realTrackCache.set(trackId, result.track);
  return result.track;
}

export async function deleteLyricsSidecar(trackId: string): Promise<Track> {
  if (apiReadMode === 'mock') return (await mock.deleteLyricsSidecar(trackId)).track;
  const current = realTrackCache.get(trackId);
  if (!current) throw new Error('track_not_found');
  const result = await real.deleteLyricsSidecar(current);
  realTrackCache.set(trackId, result.track);
  return result.track;
}

export function searchCandidates(track: Track, query?: CandidateSearchQuery): Promise<MatchCandidate[]> {
  return apiReadMode === 'mock' ? mock.searchCandidates(track) : real.searchCandidates(track, query);
}

export function listQueryHistory(trackId: string): Promise<MatchQueryHistory[]> {
  return apiReadMode === 'mock' ? Promise.resolve([]) : real.listQueryHistory(trackId);
}

export function listProviders(): Promise<ProviderConfig[]> {
  return apiReadMode === 'mock' ? mock.listProviders() : real.listProviders();
}

export function updateProvider(provider: ProviderConfig, enabled: boolean): Promise<ProviderConfig> {
  return apiReadMode === 'mock'
	? Promise.resolve({...provider, enabled, health: enabled ? provider.health === 'disabled' ? 'ready' : provider.health : 'disabled'})
	: real.updateProvider(provider.id, enabled);
}

export async function testProvider(provider: ProviderConfig): Promise<string> {
  if (apiReadMode === 'mock') return `${provider.name} Mock 连接测试完成`;
  const response = await real.testProvider(provider.id);
  return `${provider.name} 连接正常 · ${response.result.cached ? '缓存命中' : `${response.result.latencyMs}ms`}`;
}

export function listJobs(): Promise<Job[]> {
	return apiReadMode === 'mock' ? mock.listJobs() : real.listJobs();
}

export function cancelJob(jobId: string): Promise<Job | null> {
  return apiReadMode === 'mock' ? Promise.resolve(null) : real.cancelJob(jobId);
}

export function retryJob(jobId: string): Promise<Job | null> {
  return apiReadMode === 'mock' ? Promise.resolve(null) : real.retryJob(jobId);
}

export function subscribeJobEvents(jobId: string, onJob: (job: Job) => void): () => void {
  return apiReadMode === 'mock' ? () => undefined : real.subscribeJobEvents(jobId, onJob);
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

export function candidateArtworkURL(candidate: MatchCandidate): string | undefined {
  if (apiReadMode === 'mock' || !candidate.hasArtwork) return undefined;
  return `/api/v1/matches/candidates/${encodeURIComponent(candidate.id)}/artwork`;
}

export function audioURL(track: Track): string | undefined {
  if (apiReadMode === 'mock') return undefined;
  return `/api/v1/tracks/${encodeURIComponent(track.id)}/audio?revision=${encodeURIComponent(track.revision)}`;
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
