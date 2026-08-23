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

import {LibraryPage, libraryBrowseStateKey} from '@/pages/LibraryPage';

const firstLibrary: LibrarySummary = {
  id: 'lib-one', name: 'TestMusic', rootLabel: 'TestMusic', rootPath: '/music/one', active: true,
  trackCount: 1, folderCount: 0, writable: true, lastScanLabel: '刚刚', folders: [],
};

const secondLibrary: LibrarySummary = {
  ...firstLibrary, id: 'lib-two', name: 'Archive', rootLabel: 'Archive', rootPath: '/music/two', active: false,
};

const firstTrack: Track = {...seedTracks[0], id: 'trk-one', relativePath: 'one.flac', fileName: 'one.flac', folderId: 'folder-root', title: '第一首'};
const secondTrack: Track = {...seedTracks[1], id: 'trk-two', relativePath: 'two.flac', fileName: 'two.flac', folderId: 'folder-root', title: '第二首'};

const nestedLibrary: LibrarySummary = {
  ...firstLibrary,
  id: 'lib-nested', name: 'Nested', rootLabel: 'Nested', trackCount: 2, folderCount: 3,
  folders: [
    {id: 'folder-root', name: '根目录单曲', count: 0},
    {id: 'folder-album', name: '艺人 · 专辑', count: 1},
    {id: 'folder-disc', name: '艺人 · 专辑 · Disc 2', count: 1},
  ],
};
const folderTrack: Track = {...firstTrack, id: 'trk-folder', relativePath: '艺人/专辑/曲目.flac', fileName: '曲目.flac', folderId: 'folder-album', title: '目录曲目'};
const childFolderTrack: Track = {...secondTrack, id: 'trk-child', relativePath: '艺人/专辑/Disc 2/子目录曲目.flac', fileName: '子目录曲目.flac', folderId: 'folder-disc', title: '子目录曲目', health: 'missing-lyrics'};

describe('LibraryPage active library boundary', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.removeItem(libraryBrowseStateKey);
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

  it('restores the last selected directory after returning to the library page', async () => {
    const user = userEvent.setup();
    api.listLibraries.mockResolvedValue([nestedLibrary]);
    api.listTracks.mockResolvedValue([folderTrack, childFolderTrack]);
    const firstRender = render(<LibraryPage onOpenReview={vi.fn()} onOpenSettings={vi.fn()} onNotice={vi.fn()} playerPlaying={false} onPlayTrack={vi.fn()} onTogglePlayer={vi.fn()} />);

    await screen.findByRole('heading', {name: '目录曲目'});
    await user.click(screen.getByRole('button', {name: /艺人 2/}));
    await user.click(screen.getByRole('button', {name: /专辑 2/}));
    expect(screen.getByRole('button', {name: /专辑 2/})).toHaveClass('is-active');
    firstRender.unmount();

    render(<LibraryPage onOpenReview={vi.fn()} onOpenSettings={vi.fn()} onNotice={vi.fn()} playerPlaying={false} onPlayTrack={vi.fn()} onTogglePlayer={vi.fn()} />);
    await screen.findByRole('heading', {name: '目录曲目'});
    expect(await screen.findByRole('button', {name: /专辑 2/})).toHaveClass('is-active');
  });

  it('can include tracks from child directories in the selected folder', async () => {
    const user = userEvent.setup();
    api.listLibraries.mockResolvedValue([nestedLibrary]);
    api.listTracks.mockResolvedValue([folderTrack, childFolderTrack]);
    render(<LibraryPage onOpenReview={vi.fn()} onOpenSettings={vi.fn()} onNotice={vi.fn()} playerPlaying={false} onPlayTrack={vi.fn()} onTogglePlayer={vi.fn()} />);

    await screen.findByText('目录曲目');
    await user.click(screen.getByRole('button', {name: /艺人 2/}));
    expect(screen.queryByText('子目录曲目')).not.toBeInTheDocument();
    const recursiveToggle = screen.getByRole('checkbox', {name: '包含子目录'});
    expect(recursiveToggle).toBeEnabled();
    await user.click(recursiveToggle);
    expect(await screen.findByText('显示 2 / 2 首')).toBeInTheDocument();
  });

  it('applies former smart filters from the list toolbar', async () => {
    const user = userEvent.setup();
    const statusLibrary: LibrarySummary = {...firstLibrary, trackCount: 2};
    const completeTrack = {...firstTrack, title: '完整曲目', health: 'complete' as const};
    const missingTrack = {...secondTrack, title: '无歌词曲目', health: 'missing-lyrics' as const};
    api.listLibraries.mockResolvedValue([statusLibrary]);
    api.listTracks.mockResolvedValue([completeTrack, missingTrack]);
    render(<LibraryPage onOpenReview={vi.fn()} onOpenSettings={vi.fn()} onNotice={vi.fn()} playerPlaying={false} onPlayTrack={vi.fn()} onTogglePlayer={vi.fn()} />);

    expect(await screen.findByText('完整曲目')).toBeInTheDocument();
    expect(screen.queryByText('智能筛选')).not.toBeInTheDocument();
    await user.selectOptions(screen.getByRole('combobox', {name: '曲目状态筛选'}), 'missing-lyrics');
    expect(screen.queryByText('完整曲目')).not.toBeInTheDocument();
    expect(screen.getByText('无歌词曲目')).toBeInTheDocument();
  });
});
