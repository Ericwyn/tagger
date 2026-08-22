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
    vi.mocked(listRevisions).mockResolvedValue([revision]);
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
});
