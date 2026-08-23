import {render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, describe, expect, it, vi} from 'vitest';
import {HistoryPage} from '@/pages/HistoryPage';
import {listRevisions, previewRevisionRestore, restoreRevision} from '@/api';
import type {RestorePreview, Revision, Track} from '@/types';

vi.mock('@/api', () => ({
  listRevisions: vi.fn(),
  previewRevisionRestore: vi.fn(),
  restoreRevision: vi.fn(),
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

const preview: RestorePreview = {
  revisionId: revision.id, trackId: revision.trackId, target: 'before',
  preview: {
    baseRevision: 'current-revision', currentRevision: 'current-revision', dryRun: true, changed: true,
    diff: [{field: 'title', operation: 'set', before: 'New title', after: 'Old title'}],
    warnings: ['仅恢复标准字段'],
  },
};

describe('HistoryPage restore flow', () => {
  beforeEach(() => {
	vi.clearAllMocks();
    vi.mocked(listRevisions).mockResolvedValue([revision, oldRevision]);
    vi.mocked(previewRevisionRestore).mockResolvedValue(preview);
    vi.mocked(restoreRevision).mockResolvedValue({
      track: {id: revision.trackId, title: 'Old title'} as Track,
      write: {...preview.preview, dryRun: false},
      restoredRevisionId: revision.id,
      target: 'before',
    });
  });

  it('requires a preview before confirming the restore', async () => {
    const user = userEvent.setup();
    const onNotice = vi.fn();
    render(<HistoryPage onNotice={onNotice} />);

    await user.click(await screen.findByRole('button', {name: '恢复到修改前…'}));
    await waitFor(() => expect(previewRevisionRestore).toHaveBeenCalledWith(revision));
    expect(screen.getByText('将当前文件恢复到这次修改之前')).toBeInTheDocument();
    expect(screen.getByText('仅恢复标准字段')).toBeInTheDocument();
    expect(screen.getByRole('insertion')).toHaveTextContent('Old title');

    await user.click(screen.getByRole('button', {name: '确认恢复'}));
    await waitFor(() => expect(restoreRevision).toHaveBeenCalledWith(revision, preview));
    expect(onNotice).toHaveBeenCalledWith(expect.stringContaining('生成新的审计记录'));
  });

  it('previews artwork-only revisions now that binary blobs are persisted', async () => {
	const artworkRevision: Revision = {
	  ...revision,
	  action: '替换封面',
	  fields: ['artwork'],
	  diff: [{
		field: 'artwork', operation: 'set',
		before: {format: 'JPEG', width: 1200, height: 1200, size: 500000, hash: 'a'.repeat(64)},
		after: {format: 'JPEG', width: 600, height: 600, size: 90000, hash: 'b'.repeat(64)},
	  }],
	};
	const artworkPreview = {...preview, revisionId: artworkRevision.id, trackId: artworkRevision.trackId, preview: {...preview.preview, diff: [{field: 'artwork', operation: 'set' as const, before: artworkRevision.diff![0].after, after: artworkRevision.diff![0].before}]}};
	vi.mocked(listRevisions).mockResolvedValue([artworkRevision]);
	vi.mocked(previewRevisionRestore).mockResolvedValue(artworkPreview);
	render(<HistoryPage onNotice={vi.fn()} />);

	const restoreButton = await screen.findByRole('button', {name: '恢复到修改前…'});
	expect(restoreButton).toBeEnabled();
	await userEvent.setup().click(restoreButton);
	await waitFor(() => expect(previewRevisionRestore).toHaveBeenCalledWith(artworkRevision));
	expect(screen.getByRole('deletion')).toHaveTextContent('JPEG · 600×600');
    expect(screen.getByRole('insertion')).toHaveTextContent('JPEG · 1200×1200');
  });

  it('keeps legacy restore previews with null optional arrays renderable', async () => {
    const legacyPreview = {
      ...preview,
      preview: {
        ...preview.preview,
        diff: null as unknown as RestorePreview['preview']['diff'],
        warnings: null as unknown as RestorePreview['preview']['warnings'],
      },
    };
    vi.mocked(listRevisions).mockResolvedValue([{
      ...revision,
      fields: ['artwork'],
      diff: null as unknown as Revision['diff'],
    }]);
    vi.mocked(previewRevisionRestore).mockResolvedValue(legacyPreview);
    const user = userEvent.setup();
    render(<HistoryPage onNotice={vi.fn()} />);

    await user.click(await screen.findByRole('button', {name: '恢复到修改前…'}));
    await waitFor(() => expect(previewRevisionRestore).toHaveBeenCalledWith(expect.objectContaining({id: revision.id})));
    expect(screen.getByText('将当前文件恢复到这次修改之前')).toBeInTheDocument();
    expect(screen.queryByText('null')).not.toBeInTheDocument();
  });

  it('filters revisions by source and recent date', async () => {
    const user = userEvent.setup();
    render(<HistoryPage onNotice={vi.fn()} />);

    expect(await screen.findByRole('button', {name: /Old song/})).toBeInTheDocument();
    await user.selectOptions(screen.getByRole('combobox', {name: '历史来源筛选'}), '批量编辑');
    expect(screen.getByRole('button', {name: /Old song/})).toBeInTheDocument();
    expect(screen.queryByRole('button', {name: /New title/})).not.toBeInTheDocument();

    await user.selectOptions(screen.getByRole('combobox', {name: '历史时间筛选'}), '30d');
    expect(screen.queryByRole('button', {name: /Old song/})).not.toBeInTheDocument();
    expect(screen.getByText('没有匹配的修订')).toBeInTheDocument();
  });
});
