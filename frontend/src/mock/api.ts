import {candidatesFor, jobs, library, providerConfigs, revisions, seedTracks} from '@/mock/data';
import type {CandidateSearchQuery, Job, LibrarySummary, LyricsSidecarWriteResult, MatchCandidate, ProviderConfig, ProviderTestResponse, Revision, SidecarInfo, Track, TrackPatch} from '@/types';

let tracks = structuredClone(seedTracks);

function wait(ms = 90): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

export async function getLibrary(): Promise<LibrarySummary> {
  await wait();
  return structuredClone(library);
}

export async function listTracks(): Promise<Track[]> {
  await wait();
  return structuredClone(tracks);
}

export async function updateTrack(trackId: string, patch: TrackPatch): Promise<Track> {
  await wait(220);
  const index = tracks.findIndex((item) => item.id === trackId);
  if (index < 0) throw new Error('track_not_found');
  tracks[index] = {
    ...tracks[index],
    ...patch,
    health: patch.lyrics && tracks[index].artworkCount > 0 ? 'complete' : tracks[index].health,
    revision: `rev-${trackId}-${Date.now()}`,
    modifiedAt: '刚刚',
  };
  return structuredClone(tracks[index]);
}

export async function updateArtwork(trackId: string, image: File | null): Promise<Track> {
  await wait(180);
  const index = tracks.findIndex((item) => item.id === trackId);
  if (index < 0) throw new Error('track_not_found');
  tracks[index] = {
    ...tracks[index],
    artworkCount: image ? 1 : 0,
    revision: `rev-${trackId}-${Date.now()}`,
    modifiedAt: '刚刚',
  };
  return structuredClone(tracks[index]);
}

function mockSidecarRevision(content: string): string {
  let hash = 2166136261;
  for (const character of content) {
    hash ^= character.charCodeAt(0);
    hash = Math.imul(hash, 16777619);
  }
  return `sidecar-${(hash >>> 0).toString(16).padStart(8, '0')}`;
}

function sidecarInfo(content: string): SidecarInfo {
  return {
    exists: true,
    revision: mockSidecarRevision(content),
    sizeBytes: new TextEncoder().encode(content).byteLength,
    modifiedAt: '刚刚',
  };
}

export async function writeLyricsSidecar(trackId: string, content: string): Promise<LyricsSidecarWriteResult> {
  await wait(120);
  const index = tracks.findIndex((item) => item.id === trackId);
  if (index < 0) throw new Error('track_not_found');
  const before = tracks[index].lyricsSidecar;
  const after = sidecarInfo(content);
  tracks[index] = {...tracks[index], lyricsSidecar: after, modifiedAt: '刚刚'};
  return {
    track: structuredClone(tracks[index]),
    sidecar: {
      baseRevision: tracks[index].revision,
      currentRevision: tracks[index].revision,
      baseSidecarRevision: before?.revision ?? '',
      currentSidecarRevision: after.revision,
      dryRun: false,
      changed: before?.revision !== after.revision,
      before,
      after,
    },
  };
}

export async function deleteLyricsSidecar(trackId: string): Promise<LyricsSidecarWriteResult> {
  await wait(120);
  const index = tracks.findIndex((item) => item.id === trackId);
  if (index < 0) throw new Error('track_not_found');
  const before = tracks[index].lyricsSidecar;
  tracks[index] = {...tracks[index], lyricsSidecar: undefined, modifiedAt: '刚刚'};
  return {
    track: structuredClone(tracks[index]),
    sidecar: {
      baseRevision: tracks[index].revision,
      currentRevision: tracks[index].revision,
      baseSidecarRevision: before?.revision ?? '',
      dryRun: false,
      changed: Boolean(before),
      before,
    },
  };
}

export async function searchCandidates(track: Track): Promise<MatchCandidate[]> {
	await wait(520);
	return structuredClone(candidatesFor(track));
}

export async function testProvider(provider: ProviderConfig, query?: CandidateSearchQuery): Promise<ProviderTestResponse> {
	await wait(260);
	const demo = {...seedTracks[0], ...(query ? {
		title: query.title,
		artists: query.artists,
		album: query.album,
		durationSeconds: query.durationSeconds || seedTracks[0].durationSeconds,
	} : {})};
	const candidates = candidatesFor(demo).filter((candidate) => candidate.providerId === provider.id);
	const logs = [
		{level: 'info' as const, stage: 'request', message: 'Mock 已接收测试查询', details: {title: demo.title, provider: provider.id}},
		{level: 'success' as const, stage: 'search', message: `Mock 搜索完成，返回 ${candidates.length} 个候选`, details: {count: candidates.length, latencyMs: 260, cached: false}},
		...candidates.map((candidate) => ({
			level: 'success' as const,
			stage: 'candidate',
			message: `收到候选：${candidate.title.value}`,
			details: {candidateId: candidate.id, hasArtwork: candidate.hasArtwork, hasLyrics: candidate.hasLyrics},
		})),
	];
	return {
		provider: structuredClone(provider),
		result: {status: 'ok', count: candidates.length, latencyMs: 260},
		query: query ? structuredClone(query) : undefined,
		candidates: structuredClone(candidates),
		logs,
	};
}

export async function listProviders(): Promise<ProviderConfig[]> {
  await wait();
  return structuredClone(providerConfigs);
}

export async function listJobs(): Promise<Job[]> {
  await wait();
  return structuredClone(jobs);
}

export async function listRevisions(limit = 100): Promise<Revision[]> {
  await wait();
  return structuredClone(revisions.slice(0, Math.max(0, limit)));
}

export function resetMockState(): void {
  tracks = structuredClone(seedTracks);
}
