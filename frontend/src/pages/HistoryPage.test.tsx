import {render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, describe, expect, it, vi} from 'vitest';
import {HistoryPage} from '@/pages/HistoryPage';
import {getRevisionSnapshot, listRevisions} from '@/api';
import type {Revision, RevisionSnapshot} from '@/types';

vi.mock('@/api', () => ({
  getRevisionSnapshot: vi.fn(),
  listRevisions: vi.fn(),
}));

const revision: Revision = {
  id: 'revlog-1', trackId: 'trk-1', trackTitle: 'New title', fileName: 'song.flac',
  action: '修改标签', source: '手工编辑', time: '2026-08-20 12:00', fields: ['title'],
  coverTone: 'moss', currentRevision: 'current-revision',
  diff: [{field: 'title', operation: 'set', before: 'Old title', after: 'New title'}],
};

const oldRevision: Revision = {
  ...revision,
  id: 'revlog-old',
  trackTitle: 'Old song',
  fileName: 'old-song.mp3',
  action: '批量编辑标签',
  source: '批量编辑',
  time: '2025-01-01 12:00',
};

const snapshot: RevisionSnapshot = {
  revisionId: revision.id,
  trackId: revision.trackId,
  target: 'before',
  baseRevision: 'current-revision',
  currentRevision: 'current-revision',
  hasTagSnapshot: true,
  tags: {
    TITLE: ['Old title'], ARTIST: ['Original artist'], ALBUM: ['Original album'],
    TRACKNUMBER: ['2/10'], DATE: ['2020-01-01'], GENRE: ['Pop'], LYRICS: ['[00:01.00] old'],
  },
};

describe('HistoryPage snapshot loading', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(listRevisions).mockResolvedValue([revision, oldRevision]);
    vi.mocked(getRevisionSnapshot).mockResolvedValue(snapshot);
  });

  it('loads a historical tag snapshot into the editor without writing', async () => {
    const user = userEvent.setup();
    const onNotice = vi.fn();
    const onLoadSnapshot = vi.fn();
    render(<HistoryPage onNotice={onNotice} onLoadSnapshot={onLoadSnapshot} />);

    await user.click(await screen.findByRole('button', {name: '加载快照到曲目编辑器'}));
    await waitFor(() => expect(getRevisionSnapshot).toHaveBeenCalledWith(revision));
    expect(onLoadSnapshot).toHaveBeenCalledWith(expect.objectContaining({
      revisionId: revision.id,
      trackId: revision.trackId,
      label: `历史修订 ${revision.id}`,
      patch: expect.objectContaining({title: 'Old title', trackNumber: 2, trackTotal: 10, year: 2020}),
    }));
    expect(onNotice).toHaveBeenCalledWith(expect.stringContaining('已加载到曲目编辑器'));
  });

  it('opens artwork-only revisions without turning an empty tag snapshot into deletes', async () => {
    const user = userEvent.setup();
    const artworkRevision: Revision = {...revision, fields: ['artwork'], diff: [{
      field: 'artwork', operation: 'set',
      before: {format: 'JPEG', width: 1200, height: 1200, size: 500000, hash: 'a'.repeat(64)},
      after: {format: 'JPEG', width: 600, height: 600, size: 90000, hash: 'b'.repeat(64)},
    }]};
    vi.mocked(listRevisions).mockResolvedValue([artworkRevision]);
    vi.mocked(getRevisionSnapshot).mockResolvedValue({
      ...snapshot, revisionId: artworkRevision.id, hasTagSnapshot: false, tags: {}, artwork: {mime: 'image/jpeg', format: 'JPEG', width: 1200, height: 1200, size: 500000, hash: 'a'.repeat(64)},
    });
    const onLoadSnapshot = vi.fn();
    render(<HistoryPage onNotice={vi.fn()} onLoadSnapshot={onLoadSnapshot} />);

    await user.click(await screen.findByRole('button', {name: '加载快照到曲目编辑器'}));
    await waitFor(() => expect(getRevisionSnapshot).toHaveBeenCalledWith(artworkRevision));
    expect(onLoadSnapshot).toHaveBeenCalledWith(expect.objectContaining({patch: undefined}));
  });

  it('keeps legacy revisions with null optional arrays renderable', async () => {
    vi.mocked(listRevisions).mockResolvedValue([{
      ...revision,
      fields: ['artwork'],
      diff: null as unknown as Revision['diff'],
    }]);
    render(<HistoryPage onNotice={vi.fn()} onLoadSnapshot={vi.fn()} />);
    expect(await screen.findByRole('heading', {name: '修改标签'})).toBeInTheDocument();
    expect(screen.queryByText('null')).not.toBeInTheDocument();
  });

  it('filters revisions by source and recent date', async () => {
    const user = userEvent.setup();
    render(<HistoryPage onNotice={vi.fn()} onLoadSnapshot={vi.fn()} />);

    expect(await screen.findByRole('button', {name: /Old song/})).toBeInTheDocument();
    await user.selectOptions(screen.getByRole('combobox', {name: '历史来源筛选'}), '批量编辑');
    expect(screen.getByRole('button', {name: /Old song/})).toBeInTheDocument();
    expect(screen.queryByRole('button', {name: /New title/})).not.toBeInTheDocument();

    await user.selectOptions(screen.getByRole('combobox', {name: '历史时间筛选'}), '30d');
    expect(screen.queryByRole('button', {name: /Old song/ })).not.toBeInTheDocument();
    expect(screen.getByText('没有匹配的修订')).toBeInTheDocument();
  });
});
