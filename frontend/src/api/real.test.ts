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
});
