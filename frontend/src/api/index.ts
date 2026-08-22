import * as mock from '@/mock/api';
import {createRealAPI, type ScanResult} from '@/api/real';
import type {Job, LibrarySummary, MatchCandidate, ProviderConfig, Revision, Track, TrackPatch} from '@/types';

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

export async function rescanLibrary(libraryId: string): Promise<ScanResult | null> {
  if (apiReadMode === 'mock') {
    await mock.getLibrary();
    return null;
  }
  return real.rescanLibrary(libraryId);
}

export async function updateTrack(trackId: string, patch: TrackPatch): Promise<Track> {
  if (apiReadMode === 'mock') return mock.updateTrack(trackId, patch);
  const current = realTrackCache.get(trackId);
  if (!current) throw new Error('track_not_found');
  const updated: Track = {
    ...current,
    ...patch,
    revision: `draft-${trackId}-${Date.now()}`,
    modifiedAt: '浏览器草稿 · 刚刚',
    syncState: 'draft',
  };
  realTrackCache.set(trackId, updated);
  return structuredClone(updated);
}

// Provider, job and revision views remain intentionally backed by the mock
// strategy until their corresponding Go modules are connected.
export function searchCandidates(track: Track): Promise<MatchCandidate[]> {
  return mock.searchCandidates(track);
}

export function listProviders(): Promise<ProviderConfig[]> {
  return mock.listProviders();
}

export function listJobs(): Promise<Job[]> {
  return mock.listJobs();
}

export function listRevisions(): Promise<Revision[]> {
  return mock.listRevisions();
}
