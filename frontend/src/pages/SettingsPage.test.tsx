import {render, screen, waitFor, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, describe, expect, it, vi} from 'vitest';
import type {LibrarySummary, MatchCandidate, ProviderConfig} from '@/types';

const api = vi.hoisted(() => ({
  listProviders: vi.fn(),
  updateProvider: vi.fn(),
  resetProvider: vi.fn(),
  testProvider: vi.fn(),
  getLibrary: vi.fn(),
  listLibraries: vi.fn(),
  probeLibrary: vi.fn(),
  switchLibrary: vi.fn(),
  deleteLibrary: vi.fn(),
  purgeMissing: vi.fn(),
  rescanLibrary: vi.fn(),
  waitForJob: vi.fn(),
  getSystem: vi.fn(),
  clearRuntimeCache: vi.fn(),
  updateSystemSettings: vi.fn(),
  candidateArtworkURL: vi.fn(() => undefined),
}));

vi.mock('@/api', () => ({...api, apiReadMode: 'real'}));

import {SettingsPage} from '@/pages/SettingsPage';

const provider: ProviderConfig = {
  id: 'musicbrainz', name: 'MusicBrainz', shortName: 'MB', description: '结构化音乐资料',
  capabilities: ['歌曲', '封面'], health: 'ready', enabled: true, accent: '#e84b2c', quotaLabel: '1 req/s',
  config: [
    {key: 'baseUrl', label: 'API Base URL', type: 'url', value: 'https://musicbrainz.org/ws/2/recording/', required: true},
    {key: 'archiveDownloadBaseUrl', label: 'Internet Archive 下载基址', type: 'url', value: 'https://archive.org', required: true},
    {key: 'proxyUrl', label: 'HTTP 代理 URL', type: 'url', value: '', placeholder: 'http://127.0.0.1:7890'},
  ],
};

const library: LibrarySummary = {
  id: 'lib-test', name: 'TestMusic', rootLabel: 'TestMusic', rootPath: '/home/ericwyn/Downloads/TestMusic', trackCount: 24,
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

async function openProviderTab(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', {name: /数据源/}));
  await screen.findByRole('heading', {name: '音乐数据源'});
}

describe('SettingsPage provider diagnostics', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.removeItem('tagger-history-retention');
    localStorage.removeItem('tagger-provider-test-query-v1');
    api.listProviders.mockResolvedValue([provider]);
    api.updateProvider.mockImplementation((item: ProviderConfig, enabled: boolean, config?: Record<string, string>) => Promise.resolve({...item, enabled, config: config ? item.config?.map((field) => ({...field, value: config[field.key] ?? field.value})) : item.config}));
    api.resetProvider.mockImplementation((item: ProviderConfig) => Promise.resolve({...item, config: item.config?.map((field) => ({
      ...field,
      value: field.key === 'baseUrl' ? 'https://musicbrainz.org/ws/2/recording/' : field.key === 'archiveDownloadBaseUrl' ? 'https://archive.org' : field.key === 'proxyUrl' ? '' : field.value,
    }))}));
    api.getLibrary.mockResolvedValue(library);
    api.listLibraries.mockResolvedValue([{...library, active: true}]);
    api.probeLibrary.mockResolvedValue({path: '/home/ericwyn/Downloads/TestMusic', name: 'TestMusic', readable: true, writable: true, audioFiles: 24, folders: 3, formats: {mp3: 9, flac: 15, wav: 0}, warnings: []});
    api.switchLibrary.mockResolvedValue({id: 'job-switch', state: 'waiting', kind: 'scan', title: '切换曲库', detail: '等待', processed: 0, total: 24, succeeded: 0, failed: 0, startedAt: '刚刚'});
    api.deleteLibrary.mockResolvedValue({id: 'lib-archive', deleted: true});
    api.purgeMissing.mockResolvedValue({removed: 2});
    api.rescanLibrary.mockResolvedValue({id: 'job-scan', state: 'waiting'});
    api.waitForJob.mockResolvedValue({id: 'job-scan', state: 'succeeded', succeeded: 24, total: 24, detail: '扫描完成'});
    api.getSystem.mockResolvedValue({version: '1.0.2', tag_engine: 'taglib', listen: '127.0.0.1:8090', writeHistory: true, batchTrackLimit: 2000, storage: {databaseBytes: 1024 * 1024, artworkCacheBytes: 2048, providerCacheEntries: 2, artworkReferenceEntries: 1, totalBytes: 1024 * 1024 + 2048}});
    api.clearRuntimeCache.mockResolvedValue({databaseBytes: 1024 * 1024, artworkCacheBytes: 0, providerCacheEntries: 0, artworkReferenceEntries: 0, totalBytes: 1024 * 1024});
    api.updateSystemSettings.mockImplementation((settings: {historyRetention?: number; writeHistory?: boolean; batchTrackLimit?: number}) => Promise.resolve({historyRetention: settings.historyRetention ?? 20, writeHistory: settings.writeHistory ?? true, batchTrackLimit: settings.batchTrackLimit ?? 2000}));
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

    await openProviderTab(user);
    await user.click(await screen.findByRole('button', {name: '测试查询'}));
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

  it('uses the Chinese default query and remembers later test input', async () => {
    const user = userEvent.setup();
    render(<SettingsPage onNotice={vi.fn()} showGeneratedCovers={false} onShowGeneratedCoversChange={vi.fn()} />);
    await openProviderTab(user);
    await user.click(await screen.findByRole('button', {name: '测试查询'}));
    expect(screen.getByRole('textbox', {name: '测试歌曲名'})).toHaveValue('最佳歌手');
    expect(screen.getByRole('textbox', {name: '测试歌手'})).toHaveValue('许嵩');
    await user.clear(screen.getByRole('textbox', {name: '测试歌曲名'}));
    await user.type(screen.getByRole('textbox', {name: '测试歌曲名'}), '新测试歌曲');
    await waitFor(() => expect(JSON.parse(localStorage.getItem('tagger-provider-test-query-v1') || '{}')).toMatchObject({title: '新测试歌曲', artists: ['许嵩']}));
  });

  it('keeps retryable provider failures visible in the diagnostics panel', async () => {
    const user = userEvent.setup();
    api.testProvider.mockResolvedValueOnce({
      provider,
      result: {status: 'error', count: 0, latencyMs: 1200, retryable: true, retryAfterMs: 1500, hint: '数据源服务暂时不可用，请稍后重试或切换备用接口', error: 'provider HTTP 503: upstream busy'},
      candidates: [],
      logs: [{level: 'error', stage: 'search', message: '数据源搜索失败', details: {error: 'provider HTTP 503: upstream busy', retryable: true, retryAfterMs: 1500}}],
    });
    render(<SettingsPage onNotice={vi.fn()} showGeneratedCovers={false} onShowGeneratedCoversChange={vi.fn()} />);
    await openProviderTab(user);
    await user.click(await screen.findByRole('button', {name: '测试查询'}));
    await user.click(screen.getByRole('button', {name: '执行查询并探测封面'}));
    expect(await screen.findByText('查询异常')).toBeInTheDocument();
    expect(screen.getByText('可重试')).toBeInTheDocument();
    expect(screen.getByText('建议等待 2 秒')).toBeInTheDocument();
    expect(screen.getByText(/数据源服务暂时不可用/)).toBeInTheDocument();
    expect(screen.getByText(/provider HTTP 503: upstream busy/)).toBeInTheDocument();
  });

  it('opens strategy-owned fields and persists a provider configuration', async () => {
    const user = userEvent.setup();
    const onNotice = vi.fn();
    render(<SettingsPage onNotice={onNotice} showGeneratedCovers={false} onShowGeneratedCoversChange={vi.fn()} />);
    await openProviderTab(user);
    await user.click(await screen.findByRole('button', {name: '配置'}));
    expect(screen.getByRole('dialog', {name: '数据源配置'})).toBeInTheDocument();
    const baseURL = screen.getByRole('textbox', {name: 'API Base URL'});
    await user.clear(baseURL);
    await user.type(baseURL, 'https://mirror.example.test/recording/');
    const archiveDownloadBaseURL = screen.getByRole('textbox', {name: 'Internet Archive 下载基址'});
    expect(archiveDownloadBaseURL).toHaveValue('https://archive.org');
    await user.clear(archiveDownloadBaseURL);
    await user.type(archiveDownloadBaseURL, 'https://vercel-proxy.bytelen.com/https/archive.org');
    const proxyURL = screen.getByRole('textbox', {name: 'HTTP 代理 URL'});
    await user.type(proxyURL, 'http://127.0.0.1:7890');
    await user.click(screen.getByRole('button', {name: '保存并应用'}));
    await waitFor(() => expect(api.updateProvider).toHaveBeenCalledWith(provider, true, {
      baseUrl: 'https://mirror.example.test/recording/',
      archiveDownloadBaseUrl: 'https://vercel-proxy.bytelen.com/https/archive.org',
      proxyUrl: 'http://127.0.0.1:7890',
    }));
    expect(onNotice).toHaveBeenCalledWith(expect.stringContaining('配置已保存'));
  });

  it('sends an empty proxy URL to restore environment proxy behavior', async () => {
    const user = userEvent.setup();
    const configuredProvider: ProviderConfig = {
      ...provider,
      config: provider.config?.map((field) => field.key === 'proxyUrl' ? {...field, value: 'http://127.0.0.1:7890'} : field),
    };
    api.listProviders.mockResolvedValueOnce([configuredProvider]);
    render(<SettingsPage onNotice={vi.fn()} showGeneratedCovers={false} onShowGeneratedCoversChange={vi.fn()} />);
    await openProviderTab(user);
    await user.click(await screen.findByRole('button', {name: '配置'}));
    const proxyURL = screen.getByRole('textbox', {name: 'HTTP 代理 URL'});
    expect(proxyURL).toHaveValue('http://127.0.0.1:7890');
    await user.clear(proxyURL);
    await user.click(screen.getByRole('button', {name: '保存并应用'}));
    await waitFor(() => expect(api.updateProvider).toHaveBeenCalledWith(configuredProvider, true, {
      baseUrl: 'https://musicbrainz.org/ws/2/recording/',
      archiveDownloadBaseUrl: 'https://archive.org',
      proxyUrl: '',
    }));
  });

  it('requires confirmation before restoring a provider configuration', async () => {
    const user = userEvent.setup();
    const onNotice = vi.fn();
    render(<SettingsPage onNotice={onNotice} showGeneratedCovers={false} onShowGeneratedCoversChange={vi.fn()} />);
    await openProviderTab(user);
    await user.click(await screen.findByRole('button', {name: '配置'}));
    const resetButton = screen.getByRole('button', {name: '恢复默认配置'});
    expect(resetButton).toHaveClass('provider-config-reset-button');
    await user.click(resetButton);
    expect(screen.getByText('会清除自定义地址、代理和鉴权')).toBeInTheDocument();
    await user.click(screen.getByRole('button', {name: '确认恢复'}));
    await waitFor(() => expect(api.resetProvider).toHaveBeenCalledWith(provider));
    expect(onNotice).toHaveBeenCalledWith(expect.stringContaining('恢复默认配置'));
  });

  it('loads the configured library and runs a real rescan action', async () => {
    const user = userEvent.setup();
    const onNotice = vi.fn();
    render(<SettingsPage onNotice={onNotice} showGeneratedCovers={false} onShowGeneratedCoversChange={vi.fn()} />);

    await user.click(screen.getByRole('button', {name: /音乐目录/}));
    expect(await screen.findByRole('heading', {name: '音乐目录'})).toBeInTheDocument();
    expect(screen.getAllByText('/home/ericwyn/Downloads/TestMusic').length).toBeGreaterThan(0);
    expect(screen.getByText('24')).toBeInTheDocument();

    await user.click(screen.getByRole('button', {name: '重新扫描'}));
    await waitFor(() => expect(api.rescanLibrary).toHaveBeenCalledWith('lib-test'));
    expect(api.waitForJob).toHaveBeenCalledWith('job-scan');
    expect(onNotice).toHaveBeenCalledWith(expect.stringContaining('曲库扫描完成'));
  });

  it('uses an internal confirmation dialog before deleting a registered library', async () => {
    const user = userEvent.setup();
    const onNotice = vi.fn();
    const archived = {...library, id: 'lib-archive', name: 'Archive', active: false};
    api.listLibraries.mockResolvedValue([{...library, active: true}, archived]);
    render(<SettingsPage onNotice={onNotice} showGeneratedCovers={false} onShowGeneratedCoversChange={vi.fn()} />);
    await user.click(screen.getByRole('button', {name: /音乐目录/}));
    await screen.findByRole('heading', {name: '音乐目录'});
    await user.click(screen.getByRole('button', {name: '删除'}));

    const dialog = screen.getByRole('dialog', {name: '删除曲库索引？'});
    expect(dialog).toHaveTextContent('Archive');
    expect(dialog).toHaveTextContent('本地音乐文件不会被删除或修改');
    expect(api.deleteLibrary).not.toHaveBeenCalled();
    await user.click(within(dialog).getByRole('button', {name: '确认删除'}));
    await waitFor(() => expect(api.deleteLibrary).toHaveBeenCalledWith('lib-archive'));
    expect(onNotice).toHaveBeenCalledWith(expect.stringContaining('本地音乐文件未修改'));
  });

  it('uses an internal confirmation dialog before purging missing indexes', async () => {
    const user = userEvent.setup();
    render(<SettingsPage onNotice={vi.fn()} showGeneratedCovers={false} onShowGeneratedCoversChange={vi.fn()} />);
    await user.click(screen.getByRole('button', {name: /音乐目录/}));
    await screen.findByRole('heading', {name: '音乐目录'});
    await user.click(screen.getByRole('button', {name: '清理缺失索引'}));
    const dialog = screen.getByRole('dialog', {name: '清理缺失曲目索引？'});
    expect(dialog).toHaveTextContent('本地音乐文件不会被删除或修改');
    expect(api.purgeMissing).not.toHaveBeenCalled();
    await user.click(within(dialog).getByRole('button', {name: '确认清理'}));
    await waitFor(() => expect(api.purgeMissing).toHaveBeenCalledWith('lib-test'));
  });

  it('probes a directory from the library settings panel', async () => {
    const user = userEvent.setup();
    render(<SettingsPage onNotice={vi.fn()} showGeneratedCovers={false} onShowGeneratedCoversChange={vi.fn()} />);
    await user.click(screen.getByRole('button', {name: /音乐目录/}));
    await screen.findByRole('heading', {name: '音乐目录'});
    await user.click(screen.getByRole('button', {name: '验证目录'}));
    expect(screen.getByRole('dialog', {name: '目录探测'})).toBeInTheDocument();
    const path = screen.getByRole('textbox', {name: '目录路径'});
    await user.clear(path);
    await user.type(path, '/home/ericwyn/Downloads/TestMusic');
    await user.click(screen.getByRole('button', {name: '开始探测'}));
    await waitFor(() => expect(api.probeLibrary).toHaveBeenCalledWith('/home/ericwyn/Downloads/TestMusic'));
    expect(within(screen.getByRole('dialog', {name: '目录探测'})).getByText('24')).toBeInTheDocument();
    expect(within(screen.getByRole('dialog', {name: '目录探测'})).getByText(/添加或切换会排队扫描/)).toBeInTheDocument();
  });

  it('switches the active library through a guarded background job', async () => {
    const user = userEvent.setup();
    const onNotice = vi.fn();
    render(<SettingsPage onNotice={onNotice} showGeneratedCovers={false} onShowGeneratedCoversChange={vi.fn()} />);
    await user.click(screen.getByRole('button', {name: /音乐目录/}));
    await screen.findByRole('heading', {name: '音乐目录'});
    await user.click(screen.getByRole('button', {name: '验证目录'}));
    await user.click(screen.getByRole('button', {name: '开始探测'}));
    await waitFor(() => expect(api.probeLibrary).toHaveBeenCalled());
    api.waitForJob.mockResolvedValueOnce({id: 'job-switch', state: 'succeeded', detail: '已切换', processed: 24, total: 24, succeeded: 24, failed: 0});
    await user.click(screen.getByRole('button', {name: '添加并切换到此目录'}));
    await waitFor(() => expect(api.switchLibrary).toHaveBeenCalledWith('lib-test', '/home/ericwyn/Downloads/TestMusic'));
    expect(api.waitForJob).toHaveBeenCalledWith('job-switch');
    expect(onNotice).toHaveBeenCalledWith(expect.stringContaining('已切换到曲库'));
  });

  it('shows the startup HTTP listener as read-only and exposes history retention choices', async () => {
    const user = userEvent.setup();
    render(<SettingsPage onNotice={vi.fn()} showGeneratedCovers={false} onShowGeneratedCoversChange={vi.fn()} />);

    await user.click(screen.getByRole('button', {name: /系统/}));
    expect(await screen.findByText('1.0.2')).toBeInTheDocument();
    expect(screen.getByRole('link', {name: /github\.com\/Ericwyn\/tagger/i})).toHaveAttribute('href', 'https://github.com/Ericwyn/tagger');
    expect(screen.getByRole('link', {name: /github\.com\/Ericwyn\/tagger/i})).toHaveAttribute('target', '_blank');
    expect(await screen.findByText('127.0.0.1:8090')).toBeInTheDocument();
    expect(screen.queryByDisplayValue('127.0.0.1:8090')).not.toBeInTheDocument();
    const retention = screen.getByRole('combobox', {name: '历史保留次数'});
    expect([...retention.querySelectorAll('option')].map((option) => option.textContent)).toEqual(['最近 3 次', '最近 5 次', '最近 10 次', '最近 20 次']);
    await user.selectOptions(retention, '5');
    expect(localStorage.getItem('tagger-history-retention')).toBe('5');
    await waitFor(() => expect(api.updateSystemSettings).toHaveBeenCalledWith({historyRetention: 5}));
    const historySwitch = screen.getByRole('switch', {name: '启用写前历史'});
    expect(historySwitch).toBeChecked();
    await user.click(historySwitch);
    await waitFor(() => expect(api.updateSystemSettings).toHaveBeenCalledWith({writeHistory: false}));
    expect(historySwitch).not.toBeChecked();
    expect(screen.getByText('运行数据占用')).toBeInTheDocument();
    const batchLimit = screen.getByRole('spinbutton', {name: '单次批量曲目上限'});
    expect(batchLimit).toHaveValue(2000);
    await user.clear(batchLimit);
    await user.type(batchLimit, '5000');
    await user.click(screen.getByRole('button', {name: '保存'}));
    await waitFor(() => expect(api.updateSystemSettings).toHaveBeenCalledWith({batchTrackLimit: 5000}));
    await user.click(screen.getByRole('button', {name: '清理运行缓存'}));
    await waitFor(() => expect(api.clearRuntimeCache).toHaveBeenCalledTimes(1));
  });
});
