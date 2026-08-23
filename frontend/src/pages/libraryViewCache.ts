import type {StateSnapshot} from 'react-virtuoso';
import type {Track, TrackQuery} from '@/types';

export interface LibraryViewSnapshot {
  libraryId: string;
  query: TrackQuery;
  loadedTracks: Track[];
  nextCursor?: string;
  hasMore: boolean;
  total: number;
  selectedIds: string[];
  activeTrackId?: string;
  virtuosoState?: StateSnapshot;
  updatedAt: number;
}

const snapshots = new Map<string, LibraryViewSnapshot>();

function cloneState(state?: StateSnapshot): StateSnapshot | undefined {
  if (!state) return undefined;
  return {
    scrollTop: state.scrollTop,
    ranges: state.ranges.map((range) => ({...range})),
  };
}

export function getLibraryViewSnapshot(libraryId: string): LibraryViewSnapshot | undefined {
  const snapshot = snapshots.get(libraryId);
  if (!snapshot) return undefined;
  return {
    ...snapshot,
    query: {...snapshot.query},
    loadedTracks: [...snapshot.loadedTracks],
    selectedIds: [...snapshot.selectedIds],
    virtuosoState: cloneState(snapshot.virtuosoState),
  };
}

export function setLibraryViewSnapshot(snapshot: LibraryViewSnapshot): void {
  snapshots.set(snapshot.libraryId, {
    ...snapshot,
    query: {...snapshot.query},
    loadedTracks: [...snapshot.loadedTracks],
    selectedIds: [...snapshot.selectedIds],
    virtuosoState: cloneState(snapshot.virtuosoState),
  });
}

export function clearLibraryViewSnapshot(libraryId: string): void {
  snapshots.delete(libraryId);
}

export function clearLibraryViewSnapshots(): void {
  snapshots.clear();
}
