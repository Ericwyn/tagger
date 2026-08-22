import {describe, expect, it, vi} from 'vitest';
import {APIError, createRealAPI} from '@/api/real';
import type {LibrarySummary, Track} from '@/types';

const library: LibrarySummary = {
  id: 'lib-1',
  name: 'TestMusic',
  rootLabel: 'TestMusic',
  trackCount: 1,
  folderCount: 1,
  writable: true,
  lastScanLabel: '2026-08-20 10:00',
  folders: [],
};

const track = {id: 'trk-1', title: 'Song'} as Track;

describe('real API client', () => {
  it('unwraps library and track responses', async () => {
    const fetcher = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({data: [library]}), {status: 200}))
      .mockResolvedValueOnce(new Response(JSON.stringify({data: {tracks: [track], total: 1}}), {status: 200}));
    const api = createRealAPI(fetcher);

    await expect(api.getLibrary()).resolves.toEqual(library);
    await expect(api.listTracks()).resolves.toEqual([track]);
    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/libraries', expect.any(Object));
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/tracks', expect.any(Object));
  });

  it('preserves stable backend error codes', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: {code: 'scan_failed', message: '无法读取目录'},
    }), {status: 500}));
    const api = createRealAPI(fetcher);

    await expect(api.listTracks()).rejects.toEqual(new APIError(500, 'scan_failed', '无法读取目录'));
  });

  it('posts a rescan request to the selected library', async () => {
	const result = {id: 'job-1', kind: 'scan', state: 'waiting', title: 'Scan', detail: 'Waiting', processed: 0, total: 1, succeeded: 0, failed: 0, startedAt: 'now'};
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({data: result}), {status: 200}));
    const api = createRealAPI(fetcher);

    await expect(api.rescanLibrary('lib/a')).resolves.toEqual(result);
    expect(fetcher).toHaveBeenCalledWith('/api/v1/libraries/lib%2Fa/scans', expect.objectContaining({method: 'POST'}));
  });

  it('lists and retrieves persistent jobs', async () => {
	const job = {id: 'job-1', kind: 'scan', state: 'running', title: 'Scan', detail: 'Scanning', processed: 0, total: 1, succeeded: 0, failed: 0, startedAt: 'now'};
	const fetcher = vi.fn()
	  .mockResolvedValueOnce(new Response(JSON.stringify({data: [job]}), {status: 200}))
	  .mockResolvedValueOnce(new Response(JSON.stringify({data: job}), {status: 200}));
	const api = createRealAPI(fetcher);
	await expect(api.listJobs()).resolves.toEqual([job]);
	await expect(api.getJob('job/a')).resolves.toEqual(job);
	expect(fetcher.mock.calls[0][0]).toBe('/api/v1/jobs');
	expect(fetcher.mock.calls[1][0]).toBe('/api/v1/jobs/job%2Fa');
  });

  it('cancels and retries persistent jobs through explicit endpoints', async () => {
	const cancelled = {id: 'job-1', kind: 'scan', state: 'cancelled', title: 'Scan', detail: 'Cancelled', processed: 0, total: 1, succeeded: 0, failed: 0, startedAt: 'now'};
	const retried = {...cancelled, state: 'waiting', detail: 'Waiting for retry'};
	const fetcher = vi.fn()
	  .mockResolvedValueOnce(new Response(JSON.stringify({data: cancelled}), {status: 200}))
	  .mockResolvedValueOnce(new Response(JSON.stringify({data: retried}), {status: 202}));
	const api = createRealAPI(fetcher);
	await expect(api.cancelJob('job/a')).resolves.toEqual(cancelled);
	await expect(api.retryJob('job/a')).resolves.toEqual(retried);
	expect(fetcher.mock.calls[0][0]).toBe('/api/v1/jobs/job%2Fa/cancel');
	expect(fetcher.mock.calls[0][1]).toEqual(expect.objectContaining({method: 'POST'}));
	expect(fetcher.mock.calls[1][0]).toBe('/api/v1/jobs/job%2Fa/retry');
  });

  it('subscribes to job SSE snapshots and closes on cleanup', () => {
	class FakeEventSource {
	  static last: FakeEventSource | undefined;
	  readonly listeners = new Map<string, (event: Event) => void>();
	  readonly close = vi.fn();
	  constructor(readonly url: string) { FakeEventSource.last = this; }
	  addEventListener(type: string, listener: EventListenerOrEventListenerObject) {
		if (typeof listener === 'function') this.listeners.set(type, listener);
	  }
	  removeEventListener(type: string) { this.listeners.delete(type); }
	  emit(type: string, data: string) { this.listeners.get(type)?.({data} as MessageEvent<string>); }
	}
	vi.stubGlobal('EventSource', FakeEventSource);
	try {
	  const api = createRealAPI(vi.fn());
	  const onJob = vi.fn();
	  const unsubscribe = api.subscribeJobEvents('job/a', onJob);
	  expect(FakeEventSource.last?.url).toBe('/api/v1/jobs/job%2Fa/events');
	  FakeEventSource.last?.emit('job', JSON.stringify({id: 'job-1', state: 'running'}));
	  expect(onJob).toHaveBeenCalledWith(expect.objectContaining({id: 'job-1', state: 'running'}));
	  unsubscribe();
	  expect(FakeEventSource.last?.close).toHaveBeenCalledOnce();
	} finally {
	  vi.unstubAllGlobals();
	}
  });

  it('creates a write job from reviewed candidate selections', async () => {
	const job = {id: 'job-write', kind: 'write', state: 'waiting', title: 'Write', detail: 'Waiting', processed: 0, total: 1, succeeded: 0, failed: 0, startedAt: 'now'};
	const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({data: job}), {status: 202}));
	const api = createRealAPI(fetcher);
	await expect(api.createWriteJob('job/match', [{trackId: 'trk-1', candidateId: 'cand-1', baseRevision: 'rev-1', fields: ['title'], artwork: true}])).resolves.toEqual(job);
	expect(fetcher.mock.calls[0][0]).toBe('/api/v1/matches/jobs/job%2Fmatch/write');
	expect(JSON.parse(String((fetcher.mock.calls[0][1] as RequestInit).body))).toEqual({items: [{trackId: 'trk-1', candidateId: 'cand-1', baseRevision: 'rev-1', fields: ['title'], artwork: true}]});
  });

  it('creates a persistent batch edit job with revision guards', async () => {
	const job = {id: 'job-edit', kind: 'batch_edit', state: 'waiting', title: 'Edit', detail: 'Waiting', processed: 0, total: 2, succeeded: 0, failed: 0, startedAt: 'now'};
	const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({data: job}), {status: 202}));
	const api = createRealAPI(fetcher);
	await expect(api.createBatchEditJob([{trackId: 'trk-1', baseRevision: 'rev-1'}], [{field: 'genres', mode: 'append', value: 'Live'}], true)).resolves.toEqual(job);
	expect(fetcher.mock.calls[0][0]).toBe('/api/v1/tracks/batch-edit');
	expect(JSON.parse(String((fetcher.mock.calls[0][1] as RequestInit).body))).toEqual({
		items: [{trackId: 'trk-1', baseRevision: 'rev-1'}],
		operations: [{field: 'genres', mode: 'append', value: 'Live'}], sequenceTracks: true,
	});
  });

  it('lists batch edit item snapshots for the job detail view', async () => {
	const items = [{id: 'item-1', jobId: 'job-edit', trackId: 'trk-1', state: 'failed', error: 'revision conflict', diff: [{field: 'genres', operation: 'set', before: ['Pop'], after: ['Live']}]}];
	const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({data: items}), {status: 200}));
	const api = createRealAPI(fetcher);
	await expect(api.listBatchEditItems('job/edit')).resolves.toEqual(items);
	expect(fetcher).toHaveBeenCalledWith('/api/v1/jobs/job%2Fedit/batch-edit-items', expect.any(Object));
  });

  it('reads raw tags for a track with its normalized response envelope', async () => {
    const raw = {trackId: 'trk/1', revision: 'rev-1', tags: {TITLE: ['Song'], ARTIST: ['Artist']}};
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({data: raw}), {status: 200}));
    const api = createRealAPI(fetcher);

    await expect(api.getRawTags('trk/1')).resolves.toEqual(raw);
    expect(fetcher).toHaveBeenCalledWith('/api/v1/tracks/trk%2F1/raw-tags', expect.any(Object));
  });

  it('guards lyrics sidecar writes with both audio and sidecar revisions', async () => {
    const current = {...track, revision: 'rev-1', lyricsSidecar: {exists: true, revision: 'sidecar-old'}} as Track;
    const updated = {...current, revision: 'rev-2', lyricsSidecar: {exists: true, revision: 'sidecar-new'}};
    const result = {track: updated, sidecar: {baseRevision: 'rev-1', currentRevision: 'rev-2', baseSidecarRevision: 'sidecar-old', currentSidecarRevision: 'sidecar-new', dryRun: false, changed: true}};
    const fetcher = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({data: result}), {status: 200}))
      .mockResolvedValueOnce(new Response(JSON.stringify({data: result}), {status: 200}));
    const api = createRealAPI(fetcher);

    await expect(api.writeLyricsSidecar(current, '[00:01.00] hello')).resolves.toEqual(result);
    const putInit = fetcher.mock.calls[0][1] as RequestInit;
    expect(fetcher.mock.calls[0][0]).toBe('/api/v1/tracks/trk-1/lyrics-sidecar');
    expect(putInit.method).toBe('PUT');
    expect(putInit.headers).toEqual(expect.objectContaining({'If-Match': '"rev-1"'}));
    expect(JSON.parse(String(putInit.body))).toEqual({
      baseRevision: 'rev-1', baseSidecarRevision: 'sidecar-old', content: '[00:01.00] hello', dryRun: false,
    });

    await expect(api.deleteLyricsSidecar(updated)).resolves.toEqual(result);
    const deleteInit = fetcher.mock.calls[1][1] as RequestInit;
    expect(deleteInit.method).toBe('DELETE');
    expect(JSON.parse(String(deleteInit.body))).toEqual({baseRevision: 'rev-2', baseSidecarRevision: 'sidecar-new', dryRun: false});
  });

  it('writes an explicit patch guarded by the indexed revision', async () => {
    const fullTrack = {
      ...track,
      title: 'Old title',
      artists: ['Artist'],
      album: 'Album',
      albumArtists: ['Artist'],
      genres: ['Pop'],
      lyrics: 'Lyrics',
      comment: 'old comment',
      composers: ['Old Composer'],
      conductor: '',
      lyricists: [],
      copyright: '',
      bpm: 90,
      isrc: '',
      musicbrainzTrackId: '',
      musicbrainzReleaseId: '',
      musicbrainzArtistIds: [],
      acoustidId: '',
      acoustidFingerprint: '',
      trackNumber: 1,
      revision: 'rev-1',
    } as Track;
    const updated = {...fullTrack, title: 'New title', revision: 'rev-2'};
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      data: {track: updated, write: {baseRevision: 'rev-1', currentRevision: 'rev-2', changed: true, warnings: []}},
    }), {status: 200}));
    const api = createRealAPI(fetcher);

    await expect(api.updateTrack(fullTrack, {
      title: 'New title',
      artists: ['Artist'],
      album: '',
      albumArtists: [],
      genres: ['Pop'],
      lyrics: '',
      comment: 'new comment',
      composers: ['Composer A', 'Composer B'],
      conductor: 'Conductor',
      lyricists: ['Lyricist'],
      copyright: '© 2024 Label',
      bpm: 128,
      isrc: 'US-ABC-24-00001',
      musicbrainzTrackId: 'track-mbid',
      musicbrainzReleaseId: 'release-mbid',
      musicbrainzArtistIds: ['artist-mbid'],
      acoustidId: 'acoustid-id',
      acoustidFingerprint: 'fingerprint',
      trackNumber: 1,
    }, {providerId: 'musicbrainz'})).resolves.toEqual(expect.objectContaining({track: updated}));

    const init = fetcher.mock.calls[0][1] as RequestInit;
    expect(fetcher.mock.calls[0][0]).toBe('/api/v1/tracks/trk-1/tags');
    expect(init.method).toBe('PATCH');
    expect(init.headers).toEqual(expect.objectContaining({'If-Match': '"rev-1"'}));
    expect(JSON.parse(String(init.body))).toEqual(expect.objectContaining({
      baseRevision: 'rev-1',
      patch: expect.objectContaining({
        title: {op: 'set', value: 'New title'},
        album: {op: 'delete'},
        albumArtists: {op: 'delete'},
        lyrics: {op: 'delete'},
        comment: {op: 'set', value: 'new comment'},
        composers: {op: 'set', value: ['Composer A', 'Composer B']},
        conductor: {op: 'set', value: 'Conductor'},
        lyricists: {op: 'set', value: ['Lyricist']},
        copyright: {op: 'set', value: '© 2024 Label'},
        bpm: {op: 'set', value: 128},
        isrc: {op: 'set', value: 'US-ABC-24-00001'},
        musicbrainzTrackId: {op: 'set', value: 'track-mbid'},
        musicbrainzReleaseId: {op: 'set', value: 'release-mbid'},
        musicbrainzArtistIds: {op: 'set', value: ['artist-mbid']},
        acoustidId: {op: 'set', value: 'acoustid-id'},
        acoustidFingerprint: {op: 'set', value: 'fingerprint'},
      }),
	  provenance: {providerId: 'musicbrainz'},
    }));
    const serialized = JSON.parse(String(init.body)).patch;
    expect(serialized).not.toHaveProperty('artists');
    expect(serialized).not.toHaveProperty('trackNumber');
    expect(serialized).not.toHaveProperty('trackTotal');
  });

  it('searches real provider candidates and lists provider health', async () => {
    const candidate = {id: 'cand-1', providerId: 'musicbrainz', title: {value: 'Song', source: 'MusicBrainz'}};
    const provider = {id: 'musicbrainz', name: 'MusicBrainz', health: 'ready', enabled: true};
    const fetcher = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({data: {candidates: [candidate], providers: {musicbrainz: {status: 'ok'}}}}), {status: 200}))
      .mockResolvedValueOnce(new Response(JSON.stringify({data: [provider]}), {status: 200}));
    const api = createRealAPI(fetcher);

    await expect(api.searchCandidates({...track, artists: ['Artist'], album: 'Album', durationSeconds: 180} as Track)).resolves.toEqual([candidate]);
    await expect(api.listProviders()).resolves.toEqual([provider]);
    const searchInit = fetcher.mock.calls[0][1] as RequestInit;
    expect(fetcher.mock.calls[0][0]).toBe('/api/v1/matches/tracks/search');
    expect(JSON.parse(String(searchInit.body))).toEqual(expect.objectContaining({
      fileId: 'trk-1',
      query: {title: 'Song', artists: ['Artist'], album: 'Album', durationSeconds: 180},
      limitPerProvider: 5,
    }));
    expect(fetcher.mock.calls[1][0]).toBe('/api/v1/providers');
  });

  it('sends an edited candidate query while retaining the file identity', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({data: {candidates: [], providers: {}}}), {status: 200}));
    const api = createRealAPI(fetcher);
    const editedQuery = {title: '重新命名', artists: ['甲', '乙'], album: '新专辑', durationSeconds: 201};

    await expect(api.searchCandidates(track as Track, editedQuery)).resolves.toEqual([]);

    const init = fetcher.mock.calls[0][1] as RequestInit;
    expect(JSON.parse(String(init.body))).toEqual({
      fileId: 'trk-1',
      query: editedQuery,
      limitPerProvider: 5,
    });
  });

  it('persists provider enablement and runs connection tests', async () => {
	const provider = {id: 'apple', name: 'Apple', health: 'disabled', enabled: false};
	const testResult = {provider: {...provider, health: 'ready', enabled: true}, result: {status: 'ok', count: 1, latencyMs: 20, cached: true}};
	const fetcher = vi.fn()
	  .mockResolvedValueOnce(new Response(JSON.stringify({data: provider}), {status: 200}))
	  .mockResolvedValueOnce(new Response(JSON.stringify({data: testResult}), {status: 200}));
	const api = createRealAPI(fetcher);
	await expect(api.updateProvider('apple', false)).resolves.toEqual(provider);
	await expect(api.testProvider('apple')).resolves.toEqual(testResult);
	expect(JSON.parse(String((fetcher.mock.calls[0][1] as RequestInit).body))).toEqual({enabled: false});
	expect(fetcher.mock.calls[1][0]).toBe('/api/v1/providers/apple/test');
  });

  it('lists persistent revision history', async () => {
    const revision = {
      id: 'revlog-1', trackId: 'trk-1', trackTitle: 'Song', fileName: 'Song.flac',
      action: '修改标签', source: '手工编辑', time: '2026-08-20 12:00', fields: ['title'],
      coverTone: 'moss', diff: [{field: 'title', operation: 'set', before: 'Old', after: 'Song'}],
    };
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({data: [revision]}), {status: 200}));
    const api = createRealAPI(fetcher);

    await expect(api.listRevisions()).resolves.toEqual([revision]);
    expect(fetcher).toHaveBeenCalledWith('/api/v1/revisions', expect.any(Object));
  });

  it('previews and executes revision restore with the current revision guard', async () => {
	const preview = {
	  revisionId: 'revlog-1', trackId: 'trk-1', target: 'before',
	  preview: {baseRevision: 'current-rev', currentRevision: 'current-rev', dryRun: true, changed: true, diff: [], warnings: []},
	};
	const restored = {track, write: {...preview.preview, dryRun: false}, restoredRevisionId: 'revlog-1', target: 'before'};
	const fetcher = vi.fn()
	  .mockResolvedValueOnce(new Response(JSON.stringify({data: preview}), {status: 200}))
	  .mockResolvedValueOnce(new Response(JSON.stringify({data: restored}), {status: 200}));
	const api = createRealAPI(fetcher);

	await expect(api.previewRevisionRestore('revlog/a', 'current-rev')).resolves.toEqual(preview);
	await expect(api.restoreRevision('revlog/a', 'current-rev')).resolves.toEqual(restored);
	for (const [path, init] of fetcher.mock.calls) {
	  expect(path).toMatch(/^\/api\/v1\/revisions\/revlog%2Fa\/restore/);
	  expect(init).toEqual(expect.objectContaining({method: 'POST'}));
	  expect((init as RequestInit).headers).toEqual(expect.objectContaining({'If-Match': '"current-rev"'}));
	  expect(JSON.parse(String((init as RequestInit).body))).toEqual({baseRevision: 'current-rev', target: 'before'});
	}
  });

  it('uploads and deletes embedded artwork using raw image bytes and revision guards', async () => {
	const fullTrack = {...track, revision: 'art-rev-1'} as Track;
	const uploadedTrack = {...fullTrack, artworkCount: 1, revision: 'art-rev-2'};
	const deletedTrack = {...uploadedTrack, artworkCount: 0, revision: 'art-rev-3'};
	const fetcher = vi.fn()
	  .mockResolvedValueOnce(new Response(JSON.stringify({data: {track: uploadedTrack, write: {changed: true}}}), {status: 200}))
	  .mockResolvedValueOnce(new Response(JSON.stringify({data: {track: deletedTrack, write: {changed: true}}}), {status: 200}));
	const api = createRealAPI(fetcher);
	const file = new File([new Uint8Array([137, 80, 78, 71])], 'cover.png', {type: 'image/png'});

	await expect(api.writeArtwork(fullTrack, file)).resolves.toEqual(expect.objectContaining({track: uploadedTrack}));
	await expect(api.deleteArtwork(uploadedTrack)).resolves.toEqual(expect.objectContaining({track: deletedTrack}));
	const uploadInit = fetcher.mock.calls[0][1] as RequestInit;
	expect(fetcher.mock.calls[0][0]).toBe('/api/v1/tracks/trk-1/artwork/0');
	expect(uploadInit).toEqual(expect.objectContaining({method: 'PUT', body: file}));
	expect(uploadInit.headers).toEqual(expect.objectContaining({'Content-Type': 'image/png', 'If-Match': '"art-rev-1"'}));
	const deleteInit = fetcher.mock.calls[1][1] as RequestInit;
	expect(deleteInit.method).toBe('DELETE');
	expect(deleteInit.headers).toEqual(expect.objectContaining({'If-Match': '"art-rev-2"'}));
  });

  it('applies a server-retained provider artwork candidate', async () => {
	const fullTrack = {...track, revision: 'provider-art-rev'} as Track;
	const updated = {...fullTrack, artworkCount: 1, revision: 'provider-art-next'};
	const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({
	  data: {track: updated, write: {changed: true}},
	}), {status: 200}));
	const api = createRealAPI(fetcher);

	await expect(api.applyCandidateArtwork(fullTrack, 'cand/apple')).resolves.toEqual(expect.objectContaining({track: updated}));
	const [path, init] = fetcher.mock.calls[0] as [string, RequestInit];
	expect(path).toBe('/api/v1/matches/tracks/trk-1/artwork');
	expect(init.headers).toEqual(expect.objectContaining({'If-Match': '"provider-art-rev"'}));
	expect(JSON.parse(String(init.body))).toEqual({
	  candidateId: 'cand/apple', baseRevision: 'provider-art-rev', dryRun: false,
	});
  });
});
