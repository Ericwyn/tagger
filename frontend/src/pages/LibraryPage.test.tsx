import {render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, describe, expect, it, vi} from 'vitest';
import {seedTracks} from '@/mock/data';
import type {LibrarySummary, Track} from '@/types';

const api = vi.hoisted(() => ({
  listLibraries: vi.fn(),
  listTracks: vi.fn(),
  switchLibrary: vi.fn(),
}));

vi.mock('@/api', async () => ({
  ...(await vi.importActual<typeof import('@/api')>('@/api')),
  ...api,
}));

import {LibraryPage} from '@/pages/LibraryPage';

const firstLibrary: LibrarySummary = {
  id: 'lib-one', name: 'TestMusic', rootLabel: 'TestMusic', rootPath: '/music/one', active: true,
  trackCount: 1, folderCount: 0, writable: true, lastScanLabel: '刚刚', folders: [],
};

const secondLibrary: LibrarySummary = {
  ...firstLibrary, id: 'lib-two', name: 'Archive', rootLabel: 'Archive', rootPath: '/music/two', active: false,
};

const firstTrack: Track = {...seedTracks[0], id: 'trk-one', relativePath: 'one.flac', fileName: 'one.flac', folderId: 'folder-root', title: '第一首'};
const secondTrack: Track = {...seedTracks[1], id: 'trk-two', relativePath: 'two.flac', fileName: 'two.flac', folderId: 'folder-root', title: '第二首'};

describe('LibraryPage active library boundary', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.listLibraries.mockResolvedValue([firstLibrary]);
    api.listTracks.mockResolvedValue([firstTrack]);
    api.switchLibrary.mockResolvedValue({id: 'job-switch', kind: 'scan', state: 'succeeded', title: '切换', detail: '完成', processed: 1, total: 1, succeeded: 1, failed: 0, startedAt: '刚刚'});
  });

  it('resets the old folder selection after switching libraries', async () => {
    const user = userEvent.setup();
    const onNotice = vi.fn();
    api.listLibraries.mockResolvedValueOnce([firstLibrary, secondLibrary]).mockResolvedValueOnce([{...secondLibrary, active: true}]);
    api.listTracks.mockResolvedValueOnce([firstTrack]).mockResolvedValueOnce([secondTrack]);
    render(<LibraryPage onOpenReview={vi.fn()} onOpenSettings={vi.fn()} onNotice={onNotice} playerPlaying={false} onPlayTrack={vi.fn()} onTogglePlayer={vi.fn()} />);

    expect(await screen.findByRole('heading', {name: '第一首'})).toBeInTheDocument();
    await user.click(screen.getByRole('button', {name: /1 首/}));
    await user.click(screen.getByRole('option', {name: /Archive/}));

    await waitFor(() => expect(api.switchLibrary).toHaveBeenCalledWith('lib-two', '/music/two'));
    expect(await screen.findByRole('heading', {name: '第二首'})).toBeInTheDocument();
    expect(screen.getByRole('button', {name: /Archive 1 首/})).toBeInTheDocument();
    expect(onNotice).toHaveBeenCalledWith('已切换到曲库：Archive');
  });

  it('shows the add-library empty state when the backend has no active root', async () => {
    api.listLibraries.mockResolvedValue([]);
    api.listTracks.mockResolvedValue([]);
    const onOpenSettings = vi.fn();
    render(<LibraryPage onOpenReview={vi.fn()} onOpenSettings={onOpenSettings} onNotice={vi.fn()} playerPlaying={false} onPlayTrack={vi.fn()} onTogglePlayer={vi.fn()} />);

    expect(await screen.findByText('尚未配置音乐曲库')).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole('button', {name: '打开设置添加曲库'}));
    expect(onOpenSettings).toHaveBeenCalledOnce();
  });
});
