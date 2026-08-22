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
    const result = {library, report: {discovered: 1, parsed: 1, failed: 0, warningCount: 0}};
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({data: result}), {status: 200}));
    const api = createRealAPI(fetcher);

    await expect(api.rescanLibrary('lib/a')).resolves.toEqual(result);
    expect(fetcher).toHaveBeenCalledWith('/api/v1/libraries/lib%2Fa/scans', expect.objectContaining({method: 'POST'}));
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
});
