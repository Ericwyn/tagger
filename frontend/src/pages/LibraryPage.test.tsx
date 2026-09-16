import {render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, describe, expect, it, vi} from 'vitest';
import {seedTracks} from '@/mock/data';
import type {LibrarySummary, Track} from '@/types';

const api = vi.hoisted(() => ({
  listLibraries: vi.fn(),
  listTrackPage: vi.fn(),
  resolveTracks: vi.fn(),
  getSystem: vi.fn(),
  switchLibrary: vi.fn(),
}));

vi.mock('@/api', async () => ({
  ...(await vi.importActual<typeof import('@/api')>('@/api')),
  ...api,
}));

import {LibraryPage, libraryBrowseStateKey, librarySidebarWidthKey, shouldPollLibrary} from '@/pages/LibraryPage';
import {clearLibraryViewSnapshots} from '@/pages/libraryViewCache';

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

function page(tracks: Track[]) {
  return {tracks, total: tracks.length, hasMore: false};
}

function installTrackPageSource(source: Track[]) {
  api.listTrackPage.mockImplementation(async (query: {q?: string; folderPath?: string; includeSubfolders?: boolean; health?: string; format?: string} = {}) => {
    const folderPath = query.folderPath?.replaceAll(' · ', '/') ?? '';
    const filtered = source.filter((track) => {
      const directory = track.relativePath.split('/').slice(0, -1).join('/');
      if (folderPath && directory !== folderPath && (!query.includeSubfolders || !directory.startsWith(`${folderPath}/`))) return false;
      if (query.health && track.health !== query.health) return false;
      if (query.format && track.format !== query.format) return false;
      if (query.q && ![track.title, track.fileName, track.album, ...track.artists].some((value) => value.toLocaleLowerCase().includes(query.q!.toLocaleLowerCase()))) return false;
      return true;
    });
    return page(filtered);
  });
  api.resolveTracks.mockImplementation(async ({query, ids}: {query?: Parameters<typeof api.listTrackPage>[0]; ids?: string[]}) => {
    if (ids) return {tracks: source.filter((track) => ids.includes(track.id)), total: ids.length};
    const result = await api.listTrackPage(query ?? {});
    return {tracks: result.tracks, total: result.total};
  });
}

describe('LibraryPage active library boundary', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.removeItem(libraryBrowseStateKey);
    localStorage.removeItem(librarySidebarWidthKey);
    clearLibraryViewSnapshots();
    api.getSystem.mockResolvedValue({batchTrackLimit: 2000});
    api.listLibraries.mockResolvedValue([firstLibrary]);
    installTrackPageSource([firstTrack]);
    api.switchLibrary.mockResolvedValue({id: 'job-switch', kind: 'scan', state: 'succeeded', title: '切换', detail: '完成', processed: 1, total: 1, succeeded: 1, failed: 0, startedAt: '刚刚'});
  });

	  it('polls only for explicit or automatic watcher fallback', () => {
		expect(shouldPollLibrary({watchMode: 'auto', watchState: 'healthy'})).toBe(false);
		expect(shouldPollLibrary({watchMode: 'auto', watchState: 'degraded'})).toBe(true);
		expect(shouldPollLibrary({watchMode: 'poll', watchState: 'polling'})).toBe(true);
		expect(shouldPollLibrary({watchMode: 'events', watchState: 'degraded'})).toBe(false);
	  });

  it('resets the old folder selection after switching libraries', async () => {
    const user = userEvent.setup();
    const onNotice = vi.fn();
    api.listLibraries.mockResolvedValueOnce([firstLibrary, secondLibrary]).mockResolvedValueOnce([{...secondLibrary, active: true}]);
    api.listTrackPage.mockReset().mockResolvedValueOnce(page([firstTrack])).mockResolvedValueOnce(page([secondTrack]));
    api.resolveTracks.mockResolvedValue({tracks: [firstTrack], total: 1});
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
    installTrackPageSource([]);
    const onOpenSettings = vi.fn();
    render(<LibraryPage onOpenReview={vi.fn()} onOpenSettings={onOpenSettings} onNotice={vi.fn()} playerPlaying={false} onPlayTrack={vi.fn()} onTogglePlayer={vi.fn()} />);

    expect(await screen.findByText('尚未配置音乐曲库')).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole('button', {name: '打开设置添加曲库'}));
    expect(onOpenSettings).toHaveBeenCalledOnce();
  });

  it('restores the last selected directory after returning to the library page', async () => {
    const user = userEvent.setup();
    api.listLibraries.mockResolvedValue([nestedLibrary]);
    installTrackPageSource([folderTrack, childFolderTrack]);
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
    installTrackPageSource([folderTrack, childFolderTrack]);
    render(<LibraryPage onOpenReview={vi.fn()} onOpenSettings={vi.fn()} onNotice={vi.fn()} playerPlaying={false} onPlayTrack={vi.fn()} onTogglePlayer={vi.fn()} />);

    await screen.findByText('目录曲目');
    await user.click(screen.getByRole('button', {name: /艺人 2/}));
    expect(screen.queryByText('子目录曲目')).not.toBeInTheDocument();
    const recursiveToggle = screen.getByRole('checkbox', {name: '包含子目录'});
    expect(recursiveToggle).toBeEnabled();
    await user.click(recursiveToggle);
    expect(await screen.findByText('已加载 2 / 共 2 首')).toBeInTheDocument();
  });

  it('applies former smart filters from the list toolbar', async () => {
    const user = userEvent.setup();
    const statusLibrary: LibrarySummary = {...firstLibrary, trackCount: 2};
    const completeTrack = {...firstTrack, title: '完整曲目', health: 'complete' as const};
    const missingTrack = {...secondTrack, title: '无歌词曲目', health: 'missing-lyrics' as const};
    api.listLibraries.mockResolvedValue([statusLibrary]);
    installTrackPageSource([completeTrack, missingTrack]);
    render(<LibraryPage onOpenReview={vi.fn()} onOpenSettings={vi.fn()} onNotice={vi.fn()} playerPlaying={false} onPlayTrack={vi.fn()} onTogglePlayer={vi.fn()} />);

    expect(await screen.findByText('完整曲目')).toBeInTheDocument();
    expect(screen.queryByText('智能筛选')).not.toBeInTheDocument();
    await user.selectOptions(screen.getByRole('combobox', {name: '曲目状态筛选'}), 'missing-lyrics');
    expect(screen.queryByText('完整曲目')).not.toBeInTheDocument();
    expect(await screen.findByText('无歌词曲目')).toBeInTheDocument();
  });

  it('restores the loaded page and selection from the session view cache', async () => {
    const user = userEvent.setup();
    api.listLibraries.mockResolvedValue([firstLibrary]);
    api.listTrackPage.mockResolvedValue({tracks: [firstTrack], total: 2, nextCursor: 'next-page', hasMore: true});
    const firstRender = render(<LibraryPage onOpenReview={vi.fn()} onOpenSettings={vi.fn()} onNotice={vi.fn()} playerPlaying={false} onPlayTrack={vi.fn()} onTogglePlayer={vi.fn()} />);

    await screen.findByRole('heading', {name: '第一首'});
    await user.click(screen.getByRole('button', {name: '全选当前结果集'}));
    firstRender.unmount();

    api.listTrackPage.mockResolvedValue({tracks: [firstTrack], total: 2, nextCursor: 'next-page', hasMore: true});
    render(<LibraryPage onOpenReview={vi.fn()} onOpenSettings={vi.fn()} onNotice={vi.fn()} playerPlaying={false} onPlayTrack={vi.fn()} onTogglePlayer={vi.fn()} />);
    expect(await screen.findByRole('heading', {name: '第一首'})).toBeInTheDocument();
    expect(screen.getByRole('button', {name: '取消全选'})).toBeInTheDocument();
    expect(api.listTrackPage).toHaveBeenCalled();
  });

  it('opens and closes the track inspector from the compact toolbar', async () => {
    const user = userEvent.setup();
    const {container} = render(<LibraryPage onOpenReview={vi.fn()} onOpenSettings={vi.fn()} onNotice={vi.fn()} playerPlaying={false} onPlayTrack={vi.fn()} onTogglePlayer={vi.fn()} />);

    await screen.findByText('第一首');
    const inspector = container.querySelector('.track-inspector');
    expect(inspector).not.toHaveClass('is-mobile-open');

    await user.click(screen.getByRole('button', {name: '打开曲目详情'}));
    expect(inspector).toHaveClass('is-mobile-open');
    await user.click(screen.getByRole('button', {name: '关闭详情'}));
    expect(inspector).not.toHaveClass('is-mobile-open');
  });

  it('adjusts, persists, and resets the library sidebar width from the separator', async () => {
    const user = userEvent.setup();
    render(<LibraryPage onOpenReview={vi.fn()} onOpenSettings={vi.fn()} onNotice={vi.fn()} playerPlaying={false} onPlayTrack={vi.fn()} onTogglePlayer={vi.fn()} />);
    await screen.findByText('第一首');
    const resizer = screen.getByRole('separator', {name: '调整目录栏宽度'});
    resizer.focus();
    await user.keyboard('{ArrowRight}');
    expect(resizer).toHaveAttribute('aria-valuenow', '270');
    expect(localStorage.getItem(librarySidebarWidthKey)).toBe('270');
    await user.dblClick(resizer);
    expect(localStorage.getItem(librarySidebarWidthKey)).toBeNull();
  });

	  it('shows draft files immediately while keeping metadata operations locked', async () => {
		const user = userEvent.setup();
		const draftTrack: Track = {...firstTrack, syncState: 'draft'};
		installTrackPageSource([draftTrack]);
		render(<LibraryPage onOpenReview={vi.fn()} onOpenSettings={vi.fn()} onNotice={vi.fn()} playerPlaying={false} onPlayTrack={vi.fn()} onTogglePlayer={vi.fn()} />);

		expect(await screen.findByText(/正在读取标签、封面和技术信息/)).toBeInTheDocument();
		expect(screen.getByRole('button', {name: '索引完成后可试听'})).toBeDisabled();
		await user.click(screen.getByRole('button', {name: '全选当前结果集'}));
		expect(screen.getByRole('button', {name: '批量编辑'})).toBeDisabled();
		expect(screen.getByRole('button', {name: '抓取元数据'})).toBeDisabled();
	  });
});
