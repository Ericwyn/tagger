import {describe, expect, it} from 'vitest';
import {candidatesFor, library, providerConfigs, seedTracks} from '@/mock/data';

describe('mock music archive', () => {
  it('mirrors the TestMusic sample directory', () => {
    expect(library.name).toBe('TestMusic');
    expect(seedTracks).toHaveLength(24);
    expect(seedTracks.some((track) => track.fileName === '再回首-姜育恒.flac')).toBe(true);
    expect(seedTracks.filter((track) => track.album === '安泊猜想')).toHaveLength(9);
    expect(seedTracks.filter((track) => track.album === '青年晚报')).toHaveLength(9);
  });

  it('keeps provider candidates ordered by confidence', () => {
    const candidates = candidatesFor(seedTracks[0]);
    expect(candidates).toHaveLength(3);
    expect(candidates[0].providerId).toBe('musicbrainz');
    expect(candidates[0].score).toBeGreaterThan(candidates[1].score);
    expect(candidates[2].scoreLabel).toContain('版本');
  });

  it('marks unofficial providers as experimental', () => {
    expect(providerConfigs.find((provider) => provider.id === 'netease')?.experimental).toBe(true);
    expect(providerConfigs.find((provider) => provider.id === 'kuwo')?.enabled).toBe(false);
    expect(providerConfigs.find((provider) => provider.id === 'lrcapi')?.experimental).toBe(true);
  });
});
