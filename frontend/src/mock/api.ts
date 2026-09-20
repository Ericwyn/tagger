import {candidatesFor, jobs, library, providerConfigs, revisions, seedTracks} from '@/mock/data';
import {defaultBatchTrackLimit, type BatchEditSelection, type CandidateSearchQuery, type DirectoryProbe, type Job, type LibrarySummary, type LyricsSidecarWriteResult, type MatchCandidate, type OrganizeMode, type OrganizePreviewItem, type ProviderConfig, type ProviderTestResponse, type Revision, type SidecarInfo, type Track, type TrackPatch, type TrackPage, type TrackQuery, type TrackSort} from '@/types';

let tracks = structuredClone(seedTracks);

function wait(ms = 90): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

export async function getLibrary(): Promise<LibrarySummary> {
  await wait();
  return structuredClone(library);
}

export async function listLibraries(): Promise<LibrarySummary[]> {
  await wait();
  return [structuredClone({...library, active: true})];
}

export async function registerLibrary(path: string): Promise<Job> {
  await wait(140);
  return {id: `mock-register-${Date.now()}`, kind: 'scan', title: `添加曲库 · ${path.split('/').filter(Boolean).pop() || path}`, detail: 'Mock 曲库添加完成', state: 'succeeded', processed: library.trackCount, total: library.trackCount, succeeded: library.trackCount, failed: 0, startedAt: '刚刚'};
}

export async function probeLibrary(path: string): Promise<DirectoryProbe> {
  await wait();
  const normalized = path.trim() || library.rootPath || library.rootLabel;
  return {
    path: normalized,
    name: normalized.split('/').filter(Boolean).pop() || 'TestMusic',
    readable: true,
    writable: true,
    audioFiles: library.trackCount,
    folders: library.folderCount,
    formats: {
      mp3: seedTracks.filter((track) => track.format === 'mp3').length,
      flac: seedTracks.filter((track) => track.format === 'flac').length,
      wav: seedTracks.filter((track) => track.format === 'wav').length,
      ogg: seedTracks.filter((track) => track.format === 'ogg').length,
    },
    warnings: [],
  };
}

export async function switchLibrary(libraryId: string, path: string): Promise<Job> {
  await wait(140);
  return {id: `mock-switch-${Date.now()}`, kind: 'scan', title: `切换曲库 · ${path.split('/').filter(Boolean).pop() || path}`, detail: 'Mock 曲库切换完成', state: 'succeeded', processed: library.trackCount, total: library.trackCount, succeeded: library.trackCount, failed: 0, startedAt: '刚刚'};
}

export async function listTracks(): Promise<Track[]> {
  await wait();
  return structuredClone(tracks);
}

function matchesTrackQuery(track: Track, query: TrackQuery): boolean {
  if (query.folderId && track.folderId !== query.folderId) return false;
  if (query.folderPath) {
    const directory = track.relativePath.split('/').slice(0, -1).join('/');
    const folderPath = query.folderPath.replaceAll(' · ', '/');
    if (directory !== folderPath && (!query.includeSubfolders || !directory.startsWith(`${folderPath}/`))) return false;
  }
  if (query.health && track.health !== query.health) return false;
  if (query.format && track.format !== query.format) return false;
  const needle = query.q?.trim().toLocaleLowerCase() ?? '';
  if (!needle) return true;
  return [track.title, track.fileName, track.relativePath, track.album, ...track.artists, ...track.albumArtists, ...track.genres]
    .some((value) => value.toLocaleLowerCase().includes(needle));
}

function compareMockTracks(left: Track, right: Track, sort: TrackSort = 'album'): number {
  const text = (value: string) => value.trim().toLocaleLowerCase();
  if (sort === 'title') return text(left.title || left.fileName).localeCompare(text(right.title || right.fileName), 'zh-CN') || left.relativePath.localeCompare(right.relativePath);
  if (sort === 'modified') return right.modifiedAt.localeCompare(left.modifiedAt) || left.relativePath.localeCompare(right.relativePath);
  if (sort === 'format') return left.format.localeCompare(right.format) || text(left.title).localeCompare(text(right.title), 'zh-CN') || left.relativePath.localeCompare(right.relativePath);
  return text(left.album).localeCompare(text(right.album), 'zh-CN')
    || (left.discNumber ?? 0) - (right.discNumber ?? 0)
    || (left.trackNumber ?? 0) - (right.trackNumber ?? 0)
    || text(left.title || left.fileName).localeCompare(text(right.title || right.fileName), 'zh-CN')
    || left.relativePath.localeCompare(right.relativePath);
}

function filteredMockTracks(query: TrackQuery): Track[] {
  return tracks.filter((track) => matchesTrackQuery(track, query)).sort((left, right) => compareMockTracks(left, right, query.sort));
}

export async function listTrackPage(query: TrackQuery = {}, cursor = '', limit = 100): Promise<TrackPage> {
  await wait();
  if (limit < 0 || limit > 200) throw new Error('invalid_track_query');
  if (query.sort && !(['album', 'title', 'modified', 'format'] as TrackSort[]).includes(query.sort)) throw new Error('invalid_track_query');
  if (query.health && !(['complete', 'tag-compatibility', 'missing-artwork', 'missing-lyrics', 'needs-review', 'parse-error', 'missing'] as Track['health'][]).includes(query.health)) throw new Error('invalid_track_query');
  if (query.format && !(['mp3', 'flac', 'wav'] as Track['format'][]).includes(query.format)) throw new Error('invalid_track_query');
  const pageSize = Math.max(1, Math.min(limit || 100, 200));
  if (cursor && !/^\d+$/.test(cursor)) throw new Error('invalid_track_cursor');
  const offset = cursor ? Number.parseInt(cursor, 10) : 0;
  const filtered = filteredMockTracks(query);
  if (offset > filtered.length) throw new Error('invalid_track_cursor');
  const page = filtered.slice(offset, offset + pageSize);
  const nextOffset = offset + page.length;
  return {
    tracks: structuredClone(page),
    total: filtered.length,
    nextCursor: nextOffset < filtered.length ? String(nextOffset) : undefined,
    hasMore: nextOffset < filtered.length,
  };
}

export async function resolveTracks(request: {ids?: string[]; query?: TrackQuery}, limit = defaultBatchTrackLimit): Promise<{tracks: Track[]; total: number}> {
  await wait();
  const hasIDs = Boolean(request.ids?.length);
  const hasQuery = request.query != null;
  if (hasIDs === hasQuery) throw new Error('invalid_track_resolve');
  if (hasIDs) {
    const ids = [...new Set((request.ids ?? []).filter(Boolean))];
    if (ids.length > limit) throw new Error(`当前结果超过 ${limit} 首，请缩小搜索、目录或筛选范围后再操作`);
    const byID = new Map(tracks.map((track) => [track.id, track]));
    const resolved = ids.map((id) => byID.get(id));
    if (resolved.some((track) => !track)) throw new Error('track_not_found');
    return {tracks: structuredClone(resolved as Track[]), total: resolved.length};
  }
  const result = filteredMockTracks(request.query ?? {});
  if (result.length > limit) throw new Error(`当前结果超过 ${limit} 首，请缩小搜索、目录或筛选范围后再操作`);
  return {tracks: structuredClone(result), total: result.length};
}

function mockOrganizePlan(track: Track, mode: OrganizeMode): OrganizePreviewItem {
  const artist = (track.artists[0] || '未知歌手').trim() || '未知歌手';
  const album = track.album.trim() || '未知专辑';
  const target = mode === 'artist' ? `${artist}/${track.fileName}` : `${artist}/${album}/${track.fileName}`;
  return {
    trackId: track.id,
    source: track.relativePath,
    target,
    primaryArtist: artist,
    album,
    sidecarExists: Boolean(track.lyricsSidecar?.exists),
    state: track.relativePath === target ? 'noop' : 'ready',
    warnings: [],
  };
}

export async function previewOrganize(items: BatchEditSelection[], mode: OrganizeMode = 'artist_album'): Promise<OrganizePreviewItem[]> {
  await wait(120);
  const byID = new Map(tracks.map((track) => [track.id, track]));
  return items.map((item) => {
    const track = byID.get(item.trackId);
    if (!track) throw new Error('track_not_found');
    return mockOrganizePlan(track, mode);
  });
}

export async function createOrganizeJob(items: BatchEditSelection[], mode: OrganizeMode = 'artist_album'): Promise<Job> {
  const plans = await previewOrganize(items, mode);
  plans.forEach((plan) => {
    const index = tracks.findIndex((track) => track.id === plan.trackId);
    if (index < 0 || plan.state === 'noop') return;
    tracks[index] = {...tracks[index], relativePath: plan.target, folderId: mode === 'artist' ? `folder-${plan.primaryArtist}` : `folder-${plan.primaryArtist}-${plan.album}`};
  });
  return {id: `mock-organize-${Date.now()}`, kind: 'organize', title: '整理文件位置', detail: 'Mock 文件整理完成', state: 'succeeded', processed: plans.length, total: plans.length, succeeded: plans.length, failed: 0, startedAt: '刚刚'};
}

function mockTagIssues(track: Track): Track['tagIssues'] {
  const issues: Track['tagIssues'] = [];
  if (!track.title.trim()) issues.push('missing-embedded-title');
  if (track.artists.length === 0) issues.push('missing-embedded-artist');
  const hintedAlbum = track.tagHints.some((hint) => Boolean(hint.album?.trim()));
  if (!track.album.trim() && hintedAlbum) issues.push('missing-embedded-album');
  if ((track.album.trim() || hintedAlbum) && track.albumArtists.length === 0) issues.push('missing-embedded-album-artist');
  if (track.albumArtists.length === 1 && track.albumArtists[0].trim().toLocaleLowerCase() === track.album.trim().toLocaleLowerCase()
    && !track.artists.some((artist) => artist.trim().toLocaleLowerCase() === track.album.trim().toLocaleLowerCase())) {
    issues.push('suspicious-album-artist');
  }
  return issues;
}

export async function updateTrack(trackId: string, patch: TrackPatch): Promise<Track> {
  await wait(220);
  const index = tracks.findIndex((item) => item.id === trackId);
  if (index < 0) throw new Error('track_not_found');
  const next: Track = {
    ...tracks[index],
    ...patch,
    revision: `rev-${trackId}-${Date.now()}`,
    modifiedAt: '刚刚',
  };
  next.tagIssues = mockTagIssues(next);
  next.health = next.tagIssues.length > 0 ? 'tag-compatibility' : next.artworkCount === 0 ? 'missing-artwork' : next.lyrics ? 'complete' : 'missing-lyrics';
  tracks[index] = next;
  return structuredClone(tracks[index]);
}

export async function updateArtwork(trackId: string, image: File | null, maxSize = 0): Promise<Track> {
  await wait(180);
  const index = tracks.findIndex((item) => item.id === trackId);
  if (index < 0) throw new Error('track_not_found');
  const hasArtwork = Boolean(image);
  tracks[index] = {
    ...tracks[index],
    artworkCount: hasArtwork ? 1 : 0,
    artworkWidth: hasArtwork ? (maxSize || 1000) : undefined,
    artworkHeight: hasArtwork ? (maxSize || 1000) : undefined,
    artworkSizeBytes: hasArtwork ? (image?.size || 240_000) : undefined,
    health: hasArtwork ? (tracks[index].lyrics ? 'complete' : 'missing-lyrics') : 'missing-artwork',
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

export async function resetProvider(provider: ProviderConfig): Promise<ProviderConfig> {
  await wait(120);
  const defaults = providerConfigs.find((item) => item.id === provider.id) ?? provider;
  return structuredClone(defaults);
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
