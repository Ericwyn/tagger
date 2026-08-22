import {afterEach, describe, expect, it} from 'vitest';
import {deleteLyricsSidecar, listTracks, resetMockState, updateTrack, writeLyricsSidecar} from '@/mock/api';

describe('mock api', () => {
  afterEach(() => resetMockState());

  it('persists a tag patch and advances the revision', async () => {
    const before = (await listTracks())[0];
    const updated = await updateTrack(before.id, {
      title: '新的标题',
      artists: before.artists,
      album: before.album,
      albumArtists: before.albumArtists,
      trackNumber: before.trackNumber,
      trackTotal: before.trackTotal,
      discNumber: before.discNumber,
      discTotal: before.discTotal,
      year: before.year,
      genres: before.genres,
      lyrics: before.lyrics,
    });

    expect(updated.title).toBe('新的标题');
    expect(updated.revision).not.toBe(before.revision);
    expect((await listTracks())[0].title).toBe('新的标题');
  });

  it('persists and removes a lyrics sidecar without changing the audio revision', async () => {
    const before = (await listTracks())[0];
    const withSidecar = await writeLyricsSidecar(before.id, '[00:01.00] sidecar');
    expect(withSidecar.track.lyricsSidecar?.exists).toBe(true);
    expect(withSidecar.track.revision).toBe(before.revision);
    expect((await listTracks())[0].lyricsSidecar?.revision).toBe(withSidecar.track.lyricsSidecar?.revision);

    const removed = await deleteLyricsSidecar(before.id);
    expect(removed.track.lyricsSidecar).toBeUndefined();
    expect((await listTracks())[0].lyricsSidecar).toBeUndefined();
  });
});
