import {afterEach, describe, expect, it} from 'vitest';
import {deleteLyricsSidecar, listTrackPage, listTracks, resetMockState, resolveTracks, updateArtwork, updateTrack, writeLyricsSidecar} from '@/mock/api';

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
      comment: before.comment,
      composers: before.composers,
      conductor: before.conductor,
      lyricists: before.lyricists,
      copyright: before.copyright,
      bpm: before.bpm,
      isrc: before.isrc,
      musicbrainzTrackId: before.musicbrainzTrackId,
      musicbrainzReleaseId: before.musicbrainzReleaseId,
      musicbrainzArtistIds: before.musicbrainzArtistIds,
      acoustidId: before.acoustidId,
      acoustidFingerprint: before.acoustidFingerprint,
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

  it('updates artwork dimensions in the mock just like the real write response', async () => {
    const before = (await listTracks())[0];
    const file = new File([new Uint8Array(128)], 'cover.png', {type: 'image/png'});
    const resized = await updateArtwork(before.id, file, 500);
    expect(resized.artworkCount).toBe(1);
    expect(resized.artworkWidth).toBe(500);
    expect(resized.artworkHeight).toBe(500);
    expect(resized.artworkSizeBytes).toBe(128);

    const removed = await updateArtwork(before.id, null);
    expect(removed.artworkCount).toBe(0);
    expect(removed.artworkWidth).toBeUndefined();
    expect(removed.artworkHeight).toBeUndefined();
  });

  it('filters and paginates tracks with the same bounded resolve contract', async () => {
    const first = await listTrackPage({format: 'flac', sort: 'title'}, '', 1);
    expect(first.total).toBe(15);
    expect(first.tracks).toHaveLength(1);
    expect(first.hasMore).toBe(true);
    const second = await listTrackPage({format: 'flac', sort: 'title'}, first.nextCursor, 1);
    expect(second.tracks[0].id).not.toBe(first.tracks[0].id);
    await expect(listTrackPage({}, 'not-a-cursor', 1)).rejects.toThrow('invalid_track_cursor');
    await expect(resolveTracks({ids: [first.tracks[0].id]})).resolves.toEqual({tracks: [expect.objectContaining({id: first.tracks[0].id})], total: 1});
  });
});
