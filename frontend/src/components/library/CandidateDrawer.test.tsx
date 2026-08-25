import {fireEvent, render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, expect, it, vi} from 'vitest';
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
	discTotal: {value: 0, source: 'LRCLIB'},
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

beforeEach(() => {
  localStorage.clear();
});

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

  expect(screen.getByRole('textbox', {name: '远程歌词内容'})).toHaveValue('[00:01.00]new lyrics');
  fireEvent.change(screen.getByRole('textbox', {name: '远程歌词内容'}), {target: {value: '[00:01.00]edited lyrics'}});
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
    lyrics: '[00:01.00]edited lyrics',
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
  await user.selectOptions(screen.getByRole('combobox', {name: '封面写入尺寸'}), '500');
  await user.click(screen.getByRole('button', {name: '采用所选资料'}));
  await waitFor(() => expect(onApply).toHaveBeenCalledOnce());
  expect(onApply.mock.calls[0][2]).toEqual({artwork: true, artworkMaxSize: 500});
});

it('toggles all available metadata fields off when the select-all control is clicked again', async () => {
  const user = userEvent.setup();
  render(
    <CandidateDrawer
      open
      track={track}
      candidates={[candidate]}
      loading={false}
      onClose={() => undefined}
      onApply={vi.fn().mockResolvedValue(undefined)}
    />,
  );

  const selectAll = screen.getByRole('button', {name: '取消全选字段'});
  expect(screen.getByText(/采用 2 组字段/)).toBeInTheDocument();
  await user.click(selectAll);
  expect(screen.getByText(/采用 0 组字段/)).toBeInTheDocument();
  expect(screen.getByRole('button', {name: '全选可用字段'})).toBeInTheDocument();
  await user.click(screen.getByRole('button', {name: '全选可用字段'}));
  expect(screen.getByText(/采用 2 组字段/)).toBeInTheDocument();
});

it('focuses the lyrics asset when opened from the lyrics inspector tab', () => {
  render(
    <CandidateDrawer
      open
      track={track}
      candidates={[candidate]}
      loading={false}
      focus="lyrics"
      onClose={() => undefined}
      onApply={vi.fn().mockResolvedValue(undefined)}
    />,
  );

  expect(screen.getByRole('checkbox', {name: /歌词/})).toBeChecked();
});

it('allows the source query to be edited before searching again', async () => {
  const user = userEvent.setup();
  const onSearchQuery = vi.fn().mockResolvedValue(undefined);
  render(
    <CandidateDrawer
      open
      track={track}
      candidates={[candidate]}
      loading={false}
      onSearchQuery={onSearchQuery}
      onClose={() => undefined}
      onApply={vi.fn().mockResolvedValue(undefined)}
    />,
  );

  await user.click(screen.getByRole('button', {name: '修改查询'}));
  await user.clear(screen.getByRole('textbox', {name: '查询标题'}));
  await user.type(screen.getByRole('textbox', {name: '查询标题'}), '重新命名');
  await user.clear(screen.getByRole('textbox', {name: '查询艺术家'}));
  await user.type(screen.getByRole('textbox', {name: '查询艺术家'}), '甲 / 乙');
  await user.click(screen.getByRole('button', {name: '重新查询'}));

  await waitFor(() => expect(onSearchQuery).toHaveBeenCalledWith({
    title: '重新命名',
    artists: ['甲', '乙'],
    album: track.album,
    durationSeconds: track.durationSeconds,
  }));
});

it('keeps per-track query history and can run a previous query again', async () => {
  const user = userEvent.setup();
  const onSearchQuery = vi.fn().mockResolvedValue(undefined);
  render(
    <CandidateDrawer
      open
      track={track}
      candidates={[candidate]}
      loading={false}
      onSearchQuery={onSearchQuery}
      onClose={() => undefined}
      onApply={vi.fn().mockResolvedValue(undefined)}
    />,
  );

  await user.click(screen.getByRole('button', {name: '修改查询'}));
  await user.clear(screen.getByRole('textbox', {name: '查询标题'}));
  await user.type(screen.getByRole('textbox', {name: '查询标题'}), '历史查询');
  await user.click(screen.getByRole('button', {name: '重新查询'}));
  const history = await screen.findByRole('combobox', {name: '查询历史'});
  expect(history).toHaveDisplayValue('查询历史');

  await user.selectOptions(history, '0');
  await waitFor(() => expect(onSearchQuery).toHaveBeenCalledTimes(2));
  expect(onSearchQuery).toHaveBeenLastCalledWith({
    title: '历史查询', artists: track.artists, album: track.album, durationSeconds: track.durationSeconds,
  });
});

it('shows album artist in the diff and leaves a suspicious value unselected by default', async () => {
  const user = userEvent.setup();
  const onApply = vi.fn().mockResolvedValue(undefined);
  const suspicious: MatchCandidate = {
    ...candidate,
    album: {value: 'デジモンエンディングベスト', source: 'Test'},
    artists: {value: ['宮崎歩'], source: 'Test'},
    albumArtists: {value: ['デジモンエンディングベスト'], source: 'Test'},
  };
  render(
    <CandidateDrawer
      open
      track={track}
      candidates={[suspicious]}
      loading={false}
      onClose={() => undefined}
      onApply={onApply}
    />,
  );

  expect(screen.getAllByText('专辑艺术家').length).toBeGreaterThanOrEqual(2);
  expect(screen.getByText(/候选专辑艺术家与专辑名相同/)).toBeInTheDocument();
  expect(screen.getByRole('button', {name: '专辑艺术家'})).not.toHaveClass('is-active');

  await user.click(screen.getByRole('button', {name: '采用所选资料'}));
  await waitFor(() => expect(onApply).toHaveBeenCalledOnce());
  expect(onApply.mock.calls[0][0].albumArtists).toEqual(track.albumArtists);

  await user.click(screen.getByRole('button', {name: '专辑艺术家'}));
  await user.click(screen.getByRole('button', {name: '采用所选资料'}));
  await waitFor(() => expect(onApply).toHaveBeenCalledTimes(2));
  expect(onApply.mock.calls[1][0].albumArtists).toEqual(['デジモンエンディングベスト']);
});
