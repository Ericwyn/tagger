import {beforeEach, describe, expect, it} from 'vitest';
import {clearLibraryViewSnapshots, getLibraryViewSnapshot, setLibraryViewSnapshot} from '@/pages/libraryViewCache';
import type {Track} from '@/types';

const track = {id: 'track-1', title: '测试曲目'} as Track;

describe('LibraryViewCache', () => {
  beforeEach(() => clearLibraryViewSnapshots());

  it('keeps a session snapshot isolated by library and clones mutable collections', () => {
    const selectedIds = ['track-1'];
    const loadedTracks = [track];
    setLibraryViewSnapshot({
      libraryId: 'library-a',
      query: {q: '测试', sort: 'title'},
      loadedTracks,
      nextCursor: 'cursor',
      hasMore: true,
      total: 2,
      selectedIds,
      activeTrackId: 'track-1',
      virtuosoState: {ranges: [{startIndex: 0, endIndex: 0, size: 60}], scrollTop: 120},
      updatedAt: Date.now(),
    });

    selectedIds.push('mutated');
    loadedTracks.length = 0;
    const snapshot = getLibraryViewSnapshot('library-a');
    expect(snapshot?.selectedIds).toEqual(['track-1']);
    expect(snapshot?.loadedTracks).toHaveLength(1);
    expect(getLibraryViewSnapshot('library-b')).toBeUndefined();
  });

  it('can clear one library without affecting another', () => {
    setLibraryViewSnapshot({libraryId: 'library-a', query: {}, loadedTracks: [], nextCursor: undefined, hasMore: false, total: 0, selectedIds: [], updatedAt: Date.now()});
    setLibraryViewSnapshot({libraryId: 'library-b', query: {}, loadedTracks: [], nextCursor: undefined, hasMore: false, total: 0, selectedIds: [], updatedAt: Date.now()});
    clearLibraryViewSnapshots();
    expect(getLibraryViewSnapshot('library-a')).toBeUndefined();
    expect(getLibraryViewSnapshot('library-b')).toBeUndefined();
  });
});
