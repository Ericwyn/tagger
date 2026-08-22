import {render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, describe, expect, it, vi} from 'vitest';
import type {LibrarySummary, MatchCandidate, ProviderConfig} from '@/types';

const api = vi.hoisted(() => ({
  listProviders: vi.fn(),
  updateProvider: vi.fn(),
  testProvider: vi.fn(),
  getLibrary: vi.fn(),
  rescanLibrary: vi.fn(),
  waitForJob: vi.fn(),
  candidateArtworkURL: vi.fn(() => undefined),
}));

vi.mock('@/api', () => ({...api, apiReadMode: 'real'}));

import {SettingsPage} from '@/pages/SettingsPage';

const provider: ProviderConfig = {
  id: 'musicbrainz', name: 'MusicBrainz', shortName: 'MB', description: '结构化音乐资料',
  capabilities: ['歌曲', '封面'], health: 'ready', enabled: true, accent: '#e84b2c', quotaLabel: '1 req/s',
};

const library: LibrarySummary = {
  id: 'lib-test', name: 'TestMusic', rootLabel: '/home/ericwyn/Downloads/TestMusic', trackCount: 24,
  folderCount: 3, writable: true, lastScanLabel: '刚刚', folders: [],
};

const candidate = {
  id: 'cand-mb-1', providerId: 'musicbrainz', providerName: 'MusicBrainz', externalId: 'recording-1',
  title: {value: '候选歌曲', source: 'MusicBrainz'}, artists: {value: ['测试歌手'], source: 'MusicBrainz'},
  album: {value: '测试专辑', source: 'MusicBrainz'}, albumArtists: {value: ['测试歌手'], source: 'MusicBrainz'},
  year: {value: 2020, source: 'MusicBrainz'}, trackNumber: {value: 1, source: 'MusicBrainz'}, trackTotal: {value: 10, source: 'MusicBrainz'},
  discNumber: {value: 1, source: 'MusicBrainz'}, discTotal: {value: 1, source: 'MusicBrainz'}, durationSeconds: {value: 248, source: 'MusicBrainz'},
  genres: {value: ['Pop'], source: 'MusicBrainz'}, hasLyrics: true, hasArtwork: true, coverTone: 'moss', score: 0.96,
  scoreLabel: '高度匹配', matchReasons: ['标题一致'],
  lyrics: {value: '[00:01.00] 第一行歌词\n[00:05.00] 第二行歌词', source: 'MusicBrainz'},
} as MatchCandidate;

describe('SettingsPage provider diagnostics', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.listProviders.mockResolvedValue([provider]);
    api.getLibrary.mockResolvedValue(library);
    api.rescanLibrary.mockResolvedValue({id: 'job-scan', state: 'waiting'});
    api.waitForJob.mockResolvedValue({id: 'job-scan', state: 'succeeded', succeeded: 24, total: 24, detail: '扫描完成'});
    api.testProvider.mockResolvedValue({
      provider,
      result: {status: 'ok', count: 1, latencyMs: 42},
      candidates: [candidate],
      logs: [{level: 'success', stage: 'artwork', message: '封面探测成功', details: {mime: 'image/jpeg', size: 1200}}],
    });
  });

  it('accepts a custom query and renders candidates plus fetch diagnostics', async () => {
    const user = userEvent.setup();
    render(<SettingsPage onNotice={vi.fn()} showGeneratedCovers={false} onShowGeneratedCoversChange={vi.fn()} />);

    expect(await screen.findByRole('heading', {name: '音乐数据源'})).toBeInTheDocument();
    await user.click(screen.getByRole('button', {name: '测试查询'}));
    expect(screen.getByRole('dialog', {name: '数据源搜索测试'})).toBeInTheDocument();
    await user.clear(screen.getByRole('textbox', {name: '测试歌曲名'}));
    await user.type(screen.getByRole('textbox', {name: '测试歌曲名'}), '再回首');
    await user.clear(screen.getByRole('textbox', {name: '测试歌手'}));
    await user.type(screen.getByRole('textbox', {name: '测试歌手'}), '姜育恒');
    await user.click(screen.getByRole('button', {name: '执行查询并探测封面'}));

    await waitFor(() => expect(api.testProvider).toHaveBeenCalledWith(provider, {
      title: '再回首', artists: ['姜育恒'], album: '', durationSeconds: 0,
    }));
    expect(await screen.findByText('候选歌曲')).toBeInTheDocument();
    await user.click(screen.getByText('查看歌词'));
    expect(screen.getByText(/\[00:01\.00\] 第一行歌词/)).toBeInTheDocument();
    expect(screen.getByText(/抓取与封面探测日志/)).toBeInTheDocument();
    expect(screen.getByText('封面探测成功')).toBeInTheDocument();
  });

  it('loads the configured library and runs a real rescan action', async () => {
    const user = userEvent.setup();
    const onNotice = vi.fn();
    render(<SettingsPage onNotice={onNotice} showGeneratedCovers={false} onShowGeneratedCoversChange={vi.fn()} />);

    await user.click(screen.getByRole('button', {name: /音乐目录/}));
    expect(await screen.findByRole('heading', {name: '音乐目录'})).toBeInTheDocument();
    expect(screen.getByText('/home/ericwyn/Downloads/TestMusic')).toBeInTheDocument();
    expect(screen.getByText('24')).toBeInTheDocument();

    await user.click(screen.getByRole('button', {name: '重新扫描'}));
    await waitFor(() => expect(api.rescanLibrary).toHaveBeenCalledWith('lib-test'));
    expect(api.waitForJob).toHaveBeenCalledWith('job-scan');
    expect(onNotice).toHaveBeenCalledWith(expect.stringContaining('曲库扫描完成'));
  });
});
