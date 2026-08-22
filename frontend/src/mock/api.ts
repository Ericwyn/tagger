import {candidatesFor, jobs, library, providerConfigs, revisions, seedTracks} from '@/mock/data';
import type {Job, LibrarySummary, MatchCandidate, ProviderConfig, Revision, Track, TrackPatch} from '@/types';

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

export async function searchCandidates(track: Track): Promise<MatchCandidate[]> {
  await wait(520);
  return structuredClone(candidatesFor(track));
}

export async function listProviders(): Promise<ProviderConfig[]> {
  await wait();
  return structuredClone(providerConfigs);
}

export async function listJobs(): Promise<Job[]> {
  await wait();
  return structuredClone(jobs);
}

export async function listRevisions(): Promise<Revision[]> {
  await wait();
  return structuredClone(revisions);
}

export function resetMockState(): void {
  tracks = structuredClone(seedTracks);
}
