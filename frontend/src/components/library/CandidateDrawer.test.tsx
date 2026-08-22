import {render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {expect, it, vi} from 'vitest';
import {CandidateDrawer} from '@/components/library/CandidateDrawer';
import {seedTracks} from '@/mock/data';
import type {MatchCandidate, Track} from '@/types';

const track: Track = {
  ...seedTracks[0],
  trackNumber: 2,
  trackTotal: 10,
  discNumber: 1,
  discTotal: 1,
  year: 1989,
  genres: ['Pop'],
  lyrics: 'old lyrics',
};

const candidate: MatchCandidate = {
  id: 'candidate-1',
  providerId: 'lrclib',
  providerName: 'LRCLIB',
  externalId: '42',
  title: {value: track.title, source: 'LRCLIB'},
  artists: {value: track.artists, source: 'LRCLIB'},
  album: {value: '', source: 'LRCLIB'},
  albumArtists: {value: [], source: 'LRCLIB'},
  year: {value: 0, source: 'LRCLIB'},
  trackNumber: {value: 0, source: 'LRCLIB'},
  trackTotal: {value: 0, source: 'LRCLIB'},
  discNumber: {value: 0, source: 'LRCLIB'},
  durationSeconds: {value: track.durationSeconds, source: 'LRCLIB'},
  genres: {value: [], source: 'LRCLIB'},
  lyrics: {value: '[00:01.00]new lyrics', source: 'LRCLIB'},
  hasLyrics: true,
  hasArtwork: false,
  coverTone: 'moss',
  score: 1,
  scoreLabel: '高度匹配',
  matchReasons: ['标题一致'],
};

it('keeps missing candidate fields and applies explicitly selected lyrics', async () => {
  const user = userEvent.setup();
  const onApply = vi.fn().mockResolvedValue(undefined);
  render(
    <CandidateDrawer
      open
      track={track}
      candidates={[candidate]}
      loading={false}
      onClose={() => undefined}
      onApply={onApply}
    />,
  );

  await user.click(screen.getByRole('checkbox', {name: /歌词/}));
  await user.click(screen.getByRole('button', {name: '采用所选资料'}));
  await waitFor(() => expect(onApply).toHaveBeenCalledOnce());
  const patch = onApply.mock.calls[0][0];
  expect(patch).toEqual(expect.objectContaining({
    album: track.album,
    albumArtists: track.albumArtists,
    trackNumber: 2,
    trackTotal: 10,
    discNumber: 1,
    year: 1989,
    genres: ['Pop'],
    lyrics: '[00:01.00]new lyrics',
  }));
});

it('passes an explicit artwork choice without exposing the remote URL', async () => {
  const user = userEvent.setup();
  const onApply = vi.fn().mockResolvedValue(undefined);
  render(
	<CandidateDrawer
	  open
	  track={track}
	  candidates={[{...candidate, hasArtwork: true}]}
	  loading={false}
	  onClose={() => undefined}
	  onApply={onApply}
	/>,
  );

  await user.click(screen.getByRole('checkbox', {name: /封面/}));
  await user.click(screen.getByRole('button', {name: '采用所选资料'}));
  await waitFor(() => expect(onApply).toHaveBeenCalledOnce());
  expect(onApply.mock.calls[0][2]).toEqual({artwork: true});
});
