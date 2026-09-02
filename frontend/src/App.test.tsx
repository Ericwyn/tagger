import {render, screen, waitFor, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, beforeEach, describe, expect, it} from 'vitest';
import {App} from '@/App';
import {resetMockState} from '@/mock/api';
import {clearLibraryViewSnapshots} from '@/pages/libraryViewCache';

describe('Tagger app prototype', () => {
  beforeEach(() => {
    resetMockState();
    clearLibraryViewSnapshots();
    localStorage.clear();
    window.history.replaceState({}, '', '/');
  });

  afterEach(() => {
    resetMockState();
    clearLibraryViewSnapshots();
    localStorage.clear();
    window.history.replaceState({}, '', '/');
  });

  it('loads the TestMusic library and active track inspector', async () => {
    render(<App />);
    expect(screen.getByText('无播放歌曲')).toBeInTheDocument();
    expect(await screen.findByRole('heading', {name: '全部音乐'})).toBeInTheDocument();
    expect(await screen.findByRole('heading', {name: '愛上一個不回家的人'})).toBeInTheDocument();
    expect(screen.getByText('24')).toBeInTheDocument();
    expect(screen.queryByTitle('全局搜索快捷键')).not.toBeInTheDocument();
    expect(screen.queryByTitle('查看通知')).not.toBeInTheDocument();
    expect(screen.queryByTitle('账户与系统信息')).not.toBeInTheDocument();
  });

  it('keeps the navigation compact and leaves the far right for the player', async () => {
    render(<App />);
    const navigation = screen.getByRole('navigation', {name: '主导航'});
    const buttons = within(navigation).getAllByRole('button');
    expect(buttons[3]).toHaveTextContent('设置');
    expect(buttons).toHaveLength(4);
    expect(screen.queryByTitle('切换深色主题')).not.toBeInTheDocument();
    expect(screen.getByRole('region', {name: '全局播放器'})).toBeInTheDocument();
  });

  it('navigates between jobs, history and provider settings', async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole('heading', {name: '全部音乐'});

    await user.click(screen.getByRole('button', {name: /任务/}));
    expect(await screen.findByRole('heading', {name: '任务中心'})).toBeInTheDocument();

    await user.click(screen.getByRole('button', {name: '历史'}));
    expect(await screen.findByRole('heading', {name: '修改历史'})).toBeInTheDocument();

    await user.click(screen.getByRole('button', {name: '设置'}));
    expect(await screen.findByRole('heading', {name: '设置'})).toBeInTheDocument();
    await user.click(screen.getByRole('button', {name: /数据源/}));
    await waitFor(() => expect(screen.getByText('MusicBrainz')).toBeInTheDocument());
  });

  it('updates the browser URL and responds to browser back navigation', async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole('heading', {name: '全部音乐'});
    await user.click(screen.getByRole('button', {name: /任务/}));
    expect(window.location.pathname).toBe('/jobs');
    await user.click(screen.getByRole('button', {name: '历史'}));
    expect(window.location.pathname).toBe('/history');
    window.history.back();
    expect(await screen.findByRole('heading', {name: '任务中心'})).toBeInTheDocument();
  });

  it('keeps theme and font choices inside settings', async () => {
    const user = userEvent.setup();
    render(<App />);
    await user.click(screen.getByRole('button', {name: '设置'}));
    await screen.findByRole('heading', {name: '设置'});
    await user.click(screen.getByRole('button', {name: /系统/}));
    await user.click(screen.getByRole('button', {name: /灰绿色/}));
    expect(document.documentElement.dataset.theme).toBe('sage');
    await user.selectOptions(screen.getByRole('combobox', {name: '界面字体'}), 'clean');
    expect(document.documentElement.dataset.font).toBe('clean');
  });

  it('keeps the global player mounted while changing pages', async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole('heading', {name: '愛上一個不回家的人'});

    await user.click(screen.getByTitle('试听'));
    expect(screen.getByRole('region', {name: '全局播放器'})).toBeInTheDocument();
    expect(screen.getByTitle('暂停播放')).toBeInTheDocument();
    await user.click(screen.getByTitle('开启单曲循环'));
    expect(screen.getByTitle('关闭单曲循环')).toBeInTheDocument();

    await user.click(screen.getByRole('button', {name: /任务/}));
    expect(await screen.findByRole('heading', {name: '任务中心'})).toBeInTheDocument();
    expect(screen.getByRole('region', {name: '全局播放器'})).toBeInTheDocument();
    expect(screen.getByTitle('暂停播放')).toBeInTheDocument();
  });

  it('searches metadata candidates for the active track', async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole('heading', {name: '愛上一個不回家的人'});

    await user.click(screen.getByRole('button', {name: /从数据源补全/}));
    expect(screen.getByText('正在查询已启用数据源')).toBeInTheDocument();
		expect(await screen.findByText('找到 4 个候选', {}, {timeout: 2000})).toBeInTheDocument();
		expect(screen.getAllByText(/智能选择/).length).toBeGreaterThan(0);
    expect(screen.getAllByText('MusicBrainz').length).toBeGreaterThan(0);
  });

  it('keeps the inspector aligned with the selected folder', async () => {
    const user = userEvent.setup();
    render(<App />);

    await user.click(await screen.findByRole('button', {name: '许嵩 18'}));
    await user.click(await screen.findByRole('button', {name: '青年晚报 9'}));
    expect(await screen.findByRole('heading', {name: '奇谈'})).toBeInTheDocument();
  });

  it('filters the library by format and changes the track ordering', async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole('heading', {name: '全部音乐'});

    await user.selectOptions(screen.getByRole('combobox', {name: '曲目格式筛选'}), 'flac');
    expect(await screen.findByText('已加载 15 / 共 15 首')).toBeInTheDocument();
    await user.selectOptions(screen.getByRole('combobox', {name: '曲目排序'}), 'title');
    expect(screen.getByRole('combobox', {name: '曲目排序'})).toHaveValue('title');
  });
});
