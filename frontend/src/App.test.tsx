import {render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, describe, expect, it} from 'vitest';
import {App} from '@/App';
import {resetMockState} from '@/mock/api';

describe('Tagger app prototype', () => {
  afterEach(() => resetMockState());

  it('loads the TestMusic library and active track inspector', async () => {
    render(<App />);
    expect(await screen.findByRole('heading', {name: '全部音乐'})).toBeInTheDocument();
    expect(await screen.findByRole('heading', {name: '再回首'})).toBeInTheDocument();
    expect(screen.getByText('24 首曲目 · 15 首无损音频')).toBeInTheDocument();
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
    await waitFor(() => expect(screen.getByText('MusicBrainz')).toBeInTheDocument());
  });

  it('keeps the global player mounted while changing pages', async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole('heading', {name: '再回首'});

    await user.click(screen.getByTitle('试听'));
    expect(screen.getByRole('region', {name: '全局播放器'})).toBeInTheDocument();
    expect(screen.getByTitle('暂停播放')).toBeInTheDocument();

    await user.click(screen.getByRole('button', {name: /任务/}));
    expect(await screen.findByRole('heading', {name: '任务中心'})).toBeInTheDocument();
    expect(screen.getByRole('region', {name: '全局播放器'})).toBeInTheDocument();
    expect(screen.getByTitle('暂停播放')).toBeInTheDocument();
  });

  it('searches metadata candidates for the active track', async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole('heading', {name: '再回首'});

    await user.click(screen.getByRole('button', {name: /从数据源补全/}));
    expect(screen.getByText('正在查询 3 个数据源')).toBeInTheDocument();
    expect(await screen.findByText('找到 3 个候选', {}, {timeout: 2000})).toBeInTheDocument();
    expect(screen.getAllByText('MusicBrainz').length).toBeGreaterThan(0);
  });

  it('keeps the inspector aligned with the selected folder', async () => {
    const user = userEvent.setup();
    render(<App />);

    await user.click(await screen.findByRole('button', {name: '许嵩 · 青年晚报 9'}));
    expect(await screen.findByRole('heading', {name: '奇谈'})).toBeInTheDocument();
  });
});
