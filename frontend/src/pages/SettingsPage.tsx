import {useEffect, useState, type CSSProperties, type FormEvent} from 'react';
import {createPortal} from 'react-dom';
import {
  Check,
  ChevronRight,
  CircleAlert,
  Database,
  FolderCog,
  ImagePlus,
  KeyRound,
  LoaderCircle,
  Network,
  Palette,
  Plus,
  RefreshCw,
  Save,
  ServerCog,
  ShieldCheck,
  TestTube2,
  Type,
  ToggleLeft,
  ToggleRight,
  X,
} from 'lucide-react';
import {CoverArt} from '@/components/CoverArt';
import {cn} from '@/lib/utils';
import {apiReadMode, candidateArtworkURL, getSystem, listLibraries, listProviders, probeLibrary, registerLibrary, resetProvider, rescanLibrary, switchLibrary, testProvider as runProviderTest, updateProvider, updateSystemSettings, waitForJob} from '@/api';
import type {SystemInfo} from '@/api/real';
import {fontOptions, themeOptions, type FontID, type ThemeID} from '@/theme';
import {historyRetentionOptions, type CandidateSearchQuery, type DirectoryProbe, type HistoryRetention, type LibrarySummary, type MatchCandidate, type ProviderConfig, type ProviderTestResponse} from '@/types';

interface SettingsPageProps {
  onNotice: (message: string) => void;
  showGeneratedCovers: boolean;
  onShowGeneratedCoversChange: (value: boolean) => void;
  theme?: ThemeID;
  onThemeChange?: (value: ThemeID) => void;
  font?: FontID;
  onFontChange?: (value: FontID) => void;
}

type SettingsTab = 'libraries' | 'providers' | 'system';

const healthText = {
  ready: '连接正常',
  degraded: '部分可用',
  misconfigured: '需要配置',
  disabled: '未启用',
};

const defaultTestQuery: CandidateSearchQuery = {
  title: '最佳歌手', artists: ['许嵩'], album: '', durationSeconds: 0,
};

const historyRetentionKey = 'tagger-history-retention';
const providerTestQueryKey = 'tagger-provider-test-query-v1';

function readProviderTestQuery(): CandidateSearchQuery {
  try {
    const raw = JSON.parse(localStorage.getItem(providerTestQueryKey) || '') as Partial<CandidateSearchQuery>;
    return {
      title: typeof raw.title === 'string' && raw.title.trim() ? raw.title : defaultTestQuery.title,
      artists: Array.isArray(raw.artists) ? raw.artists.filter((item): item is string => typeof item === 'string') : defaultTestQuery.artists,
      album: typeof raw.album === 'string' ? raw.album : defaultTestQuery.album,
      durationSeconds: typeof raw.durationSeconds === 'number' && Number.isFinite(raw.durationSeconds) ? Math.max(0, raw.durationSeconds) : defaultTestQuery.durationSeconds,
    };
  } catch {
    return defaultTestQuery;
  }
}

function readHistoryRetention(): HistoryRetention {
  const value = Number(localStorage.getItem(historyRetentionKey));
  return historyRetentionOptions.includes(value as HistoryRetention) ? value as HistoryRetention : 20;
}

function formatLogDetails(details: Record<string, string | number | boolean | string[] | undefined> | undefined): string {
  if (!details) return '';
  return Object.entries(details)
    .filter(([, value]) => value !== undefined)
    .map(([key, value]) => `${key}=${Array.isArray(value) ? value.join(' / ') : String(value)}`)
    .join(' · ');
}

function candidateAssetLabel(candidate: MatchCandidate): string {
  const assets = [];
  if (candidate.hasArtwork) assets.push('封面');
  if (candidate.hasLyrics) assets.push('歌词');
  return assets.length > 0 ? assets.join(' + ') : '仅元数据';
}

export function SettingsPage({onNotice, showGeneratedCovers, onShowGeneratedCoversChange, theme = 'ivory', onThemeChange = () => undefined, font = 'editorial', onFontChange = () => undefined}: SettingsPageProps) {
  const [tab, setTab] = useState<SettingsTab>('providers');
  const [providers, setProviders] = useState<ProviderConfig[]>([]);
  const [testingId, setTestingId] = useState<string>();
  const [testProviderId, setTestProviderId] = useState<string>();
  const [testQuery, setTestQuery] = useState<CandidateSearchQuery>(readProviderTestQuery);
  const [testArtistInput, setTestArtistInput] = useState(() => readProviderTestQuery().artists.join(' / '));
  const [testResponse, setTestResponse] = useState<ProviderTestResponse>();
  const [testError, setTestError] = useState('');
  const [configProviderId, setConfigProviderId] = useState<string>();
  const [configValues, setConfigValues] = useState<Record<string, string>>({});
  const [configSaving, setConfigSaving] = useState(false);
  const [configResetting, setConfigResetting] = useState(false);
  const [configResetPending, setConfigResetPending] = useState(false);
  const [configError, setConfigError] = useState('');
  const [library, setLibrary] = useState<LibrarySummary>();
  const [libraries, setLibraries] = useState<LibrarySummary[]>([]);
  const [libraryLoading, setLibraryLoading] = useState(false);
  const [libraryScanning, setLibraryScanning] = useState(false);
  const [libraryError, setLibraryError] = useState('');
  const [directoryProbeOpen, setDirectoryProbeOpen] = useState(false);
  const [directoryPath, setDirectoryPath] = useState('');
  const [directoryProbe, setDirectoryProbe] = useState<DirectoryProbe>();
  const [directoryProbeError, setDirectoryProbeError] = useState('');
  const [directoryProbing, setDirectoryProbing] = useState(false);
  const [directorySwitching, setDirectorySwitching] = useState(false);
  const [systemInfo, setSystemInfo] = useState<SystemInfo>();
  const [historyRetention, setHistoryRetention] = useState<HistoryRetention>(readHistoryRetention);
  const [writeHistory, setWriteHistory] = useState(true);

  useEffect(() => {
    listProviders().then(setProviders);
  }, []);

  useEffect(() => {
    try {
      localStorage.setItem(providerTestQueryKey, JSON.stringify({
        ...testQuery,
        artists: testArtistInput.split(/[,，/]/).map((item) => item.trim()).filter(Boolean),
      }));
    } catch {
      // Browser storage may be disabled; the current session still works.
    }
  }, [testArtistInput, testQuery]);

  const loadLibrary = async () => {
    setLibraryLoading(true);
    setLibraryError('');
    try {
      const items = await listLibraries();
      setLibraries(items);
      setLibrary(items.find((item) => item.active));
    } catch (error) {
      setLibraries([]);
      setLibrary(undefined);
      setLibraryError(error instanceof Error ? error.message : '曲库信息读取失败');
    } finally {
      setLibraryLoading(false);
    }
  };

  const openDirectoryProbe = () => {
    setDirectoryPath(library?.rootPath || library?.rootLabel || '');
    setDirectoryProbe(undefined);
    setDirectoryProbeError('');
    setDirectoryProbeOpen(true);
  };

  const executeDirectoryProbe = async (event: FormEvent) => {
    event.preventDefault();
    if (!directoryPath.trim()) {
      setDirectoryProbeError('请输入要探测的目录路径');
      return;
    }
    setDirectoryProbing(true);
    setDirectoryProbeError('');
    try {
      setDirectoryProbe(await probeLibrary(directoryPath));
    } catch (error) {
      setDirectoryProbe(undefined);
      setDirectoryProbeError(error instanceof Error ? error.message : '目录探测失败');
    } finally {
      setDirectoryProbing(false);
    }
  };

  const switchActiveLibrary = async () => {
    if (!directoryProbe || directorySwitching) return;
    setDirectorySwitching(true);
    try {
      const normalizedPath = directoryProbe.path.trim().replace(/[\\/]+$/, '');
      const existing = libraries.find((item) => (item.rootPath || '').trim().replace(/[\\/]+$/, '') === normalizedPath);
      const job = existing ? await switchLibrary(existing.id, directoryProbe.path) : await registerLibrary(directoryProbe.path);
      if (job && apiReadMode === 'real') {
        const completed = await waitForJob(job.id);
        if (completed.state !== 'succeeded') {
          throw new Error(completed.detail || `曲库切换${completed.state}`);
        }
      }
      await loadLibrary();
      setDirectoryProbeOpen(false);
      onNotice(`${existing ? '已切换到' : '已添加并切换到'}曲库：${directoryProbe.name}`);
    } catch (error) {
      setDirectoryProbeError(error instanceof Error ? error.message : '曲库切换失败');
    } finally {
      setDirectorySwitching(false);
    }
  };

  const switchRegisteredLibrary = async (target: LibrarySummary) => {
    if (target.active || directorySwitching || !target.rootPath) return;
    setDirectorySwitching(true);
    try {
      const job = await switchLibrary(target.id, target.rootPath);
      if (job && apiReadMode === 'real') {
        const completed = await waitForJob(job.id);
        if (completed.state !== 'succeeded') throw new Error(completed.detail || `曲库切换${completed.state}`);
      }
      await loadLibrary();
      onNotice(`已切换到曲库：${target.name}`);
    } catch (error) {
      onNotice(error instanceof Error ? error.message : '曲库切换失败');
    } finally {
      setDirectorySwitching(false);
    }
  };

  useEffect(() => {
    if (tab === 'libraries') void loadLibrary();
  }, [tab]);

  useEffect(() => {
    if (tab !== 'system') return;
    void getSystem().then((info) => {
      setSystemInfo(info);
      if (info.historyRetention && historyRetentionOptions.includes(info.historyRetention as HistoryRetention)) {
        setHistoryRetention(info.historyRetention as HistoryRetention);
        localStorage.setItem(historyRetentionKey, String(info.historyRetention));
      }
      if (typeof info.writeHistory === 'boolean') setWriteHistory(info.writeHistory);
    }).catch(() => setSystemInfo(undefined));
  }, [tab]);

  const updateHistoryRetention = async (value: HistoryRetention) => {
    setHistoryRetention(value);
    localStorage.setItem(historyRetentionKey, String(value));
    try {
      await updateSystemSettings({historyRetention: value});
      onNotice(`历史保留策略已更新：每首曲目最近 ${value} 次修订`);
    } catch (error) {
      onNotice(error instanceof Error ? error.message : '历史保留策略保存失败');
    }
  };

  const updateWriteHistory = async (value: boolean) => {
    const previous = writeHistory;
    setWriteHistory(value);
    try {
      await updateSystemSettings({writeHistory: value});
      onNotice(value ? '已启用写前历史' : '已关闭写前历史；后续写入不会创建新修订');
    } catch (error) {
      setWriteHistory(previous);
      onNotice(error instanceof Error ? error.message : '写前历史设置保存失败');
    }
  };

  const runLibraryScan = async () => {
    if (!library || libraryScanning) return;
    setLibraryScanning(true);
    try {
      const queued = await rescanLibrary(library.id);
      if (queued) {
        const completed = await waitForJob(queued.id);
        onNotice(completed.state === 'succeeded'
          ? `曲库扫描完成：已索引 ${completed.succeeded || completed.total} 首曲目`
          : `曲库扫描结束：${completed.detail || completed.state}`);
      } else {
        onNotice('Mock 曲库扫描完成');
      }
      await loadLibrary();
    } catch (error) {
      onNotice(error instanceof Error ? error.message : '曲库扫描失败');
    } finally {
      setLibraryScanning(false);
    }
  };

  const toggleProvider = async (provider: ProviderConfig) => {
    try {
      const updated = await updateProvider(provider, !provider.enabled);
      setProviders((current) => current.map((item) => item.id === updated.id ? updated : item));
      onNotice(`${updated.name} 已${updated.enabled ? '启用' : '停用'}并持久化`);
    } catch (error) {
      onNotice(error instanceof Error ? error.message : '数据源设置保存失败');
    }
  };

  const openProviderTest = (provider: ProviderConfig) => {
    setTestProviderId(provider.id);
    setTestResponse(undefined);
    setTestError('');
  };

  const openProviderConfig = (provider: ProviderConfig) => {
    setConfigProviderId(provider.id);
    setConfigError('');
    setConfigResetPending(false);
    setConfigValues(Object.fromEntries((provider.config ?? []).map((field) => [field.key, field.secret ? '' : field.value ?? ''])));
  };

  const resetProviderConfig = async () => {
    const provider = providers.find((item) => item.id === configProviderId);
    if (!provider || configResetting) return;
    setConfigResetting(true);
    setConfigError('');
    try {
      const updated = await resetProvider(provider);
      setProviders((current) => current.map((item) => item.id === updated.id ? updated : item));
      setConfigValues(Object.fromEntries((updated.config ?? []).map((field) => [field.key, field.secret ? '' : field.value ?? ''])));
      setConfigResetPending(false);
      onNotice(`${provider.name} 已恢复默认配置`);
    } catch (error) {
      setConfigError(error instanceof Error ? error.message : '恢复默认配置失败');
    } finally {
      setConfigResetting(false);
    }
  };

  const saveProviderConfig = async (event: FormEvent) => {
    event.preventDefault();
    const provider = providers.find((item) => item.id === configProviderId);
    if (!provider) return;
    const fields = provider.config ?? [];
    const config: Record<string, string> = {};
    for (const field of fields) {
      const value = (configValues[field.key] ?? '').trim();
      if (field.required && !value) {
        setConfigError(`请填写${field.label}`);
        return;
      }
      // A blank password means “keep the existing secret”, not “erase it”.
      if (field.secret && !value) continue;
      config[field.key] = value;
    }
    setConfigSaving(true);
    setConfigError('');
    try {
      const updated = await updateProvider(provider, provider.enabled, config);
      setProviders((current) => current.map((item) => item.id === updated.id ? updated : item));
      setConfigProviderId(undefined);
      onNotice(`${provider.name} 配置已保存并立即生效`);
    } catch (error) {
      setConfigError(error instanceof Error ? error.message : '数据源配置保存失败');
    } finally {
      setConfigSaving(false);
    }
  };

  const executeProviderTest = async (event: FormEvent) => {
    event.preventDefault();
    const provider = providers.find((item) => item.id === testProviderId);
    const title = testQuery.title.trim();
    if (!provider || !title) {
      setTestError('请输入歌曲名后再测试');
      return;
    }
    const query: CandidateSearchQuery = {
      ...testQuery,
      title,
      artists: testArtistInput.split(/[,，/]/).map((item) => item.trim()).filter(Boolean),
      album: testQuery.album.trim(),
    };
    setTestingId(provider.id);
    setTestError('');
    setTestResponse(undefined);
    try {
      setTestResponse(await runProviderTest(provider, query));
    } catch (error) {
      setTestError(error instanceof Error ? error.message : '数据源查询失败');
    } finally {
      setTestingId(undefined);
    }
	};

  const providerTestPanel = testProviderId ? (() => {
    const provider = providers.find((item) => item.id === testProviderId);
    if (!provider) return null;
    const candidates = testResponse?.candidates ?? [];

    return createPortal(
      <div className="provider-test-overlay">
        <button className="provider-test-backdrop" aria-label="关闭数据源搜索测试" onClick={() => setTestProviderId(undefined)} />
        <section className="provider-test-panel" role="dialog" aria-modal="true" aria-label="数据源搜索测试">
          <div className="provider-test-head">
            <div>
              <span className="eyebrow">PROVIDER DIAGNOSTICS</span>
              <h3>测试 {provider.name}</h3>
              <p>输入一组真实查询，查看候选、歌词、封面和探测日志。</p>
            </div>
            <button className="icon-button" title="关闭数据源测试" onClick={() => setTestProviderId(undefined)}><X size={17} /></button>
          </div>
          <form className="provider-test-form" onSubmit={(event) => void executeProviderTest(event)}>
            <label><span>歌曲名</span><input aria-label="测试歌曲名" value={testQuery.title} onChange={(event) => setTestQuery((current) => ({...current, title: event.target.value}))} placeholder="例如：再回首" required /></label>
            <label><span>歌手</span><input aria-label="测试歌手" value={testArtistInput} onChange={(event) => setTestArtistInput(event.target.value)} placeholder="多个歌手用 / 分隔" /></label>
            <label><span>专辑（可选）</span><input aria-label="测试专辑" value={testQuery.album} onChange={(event) => setTestQuery((current) => ({...current, album: event.target.value}))} placeholder="专辑名" /></label>
            <label><span>时长秒数（可选）</span><input aria-label="测试时长" type="number" min="0" value={testQuery.durationSeconds || ''} onChange={(event) => setTestQuery((current) => ({...current, durationSeconds: Math.max(0, Number(event.target.value) || 0)}))} placeholder="例如：248" /></label>
            <button className="primary-button" type="submit" disabled={testingId === provider.id}>
              {testingId === provider.id ? <LoaderCircle size={14} className="spin" /> : <TestTube2 size={14} />}
              执行查询并探测封面
            </button>
          </form>
          {testError && <div className="provider-test-error"><CircleAlert size={14} /> {testError}</div>}
          {testResponse && (
            <div className="provider-test-output">
              <div className="provider-test-summary">
                <strong className={cn(testResponse.result.status === 'ok' ? 'is-success' : 'is-error')}>{testResponse.result.status === 'ok' ? '查询完成' : '查询异常'}</strong>
                <span>{testResponse.result.count} 个候选</span>
                <span>{testResponse.result.latencyMs} ms</span>
                {testResponse.result.cached && <span>缓存命中</span>}
                {testResponse.result.retryable && <span>可重试</span>}
                {(testResponse.result.retryAfterMs ?? 0) > 0 && <span>建议等待 {Math.ceil((testResponse.result.retryAfterMs ?? 0) / 1000)} 秒</span>}
              </div>
              {testResponse.result.status !== 'ok' && testResponse.result.hint && <div className="provider-test-hint"><CircleAlert size={14} /><span>{testResponse.result.hint}</span></div>}
              {candidates.length > 0 ? (
                <div className="provider-test-candidates">
                  {candidates.map((candidate) => {
                    const imageUrl = candidateArtworkURL(candidate);
                    return (
                      <article className="provider-test-candidate" key={candidate.id}>
                        <CoverArt title={candidate.title.value} artist={candidate.artists.value[0]} tone={candidate.coverTone} size="sm" missing={!showGeneratedCovers && !imageUrl} imageUrl={imageUrl} blankOnImageError={!showGeneratedCovers} />
                        <div>
                          <strong>{candidate.title.value}</strong>
                          <span>{candidate.artists.value.join(' / ') || '未提供歌手'}</span>
                          <small>{candidate.album.value || '未提供专辑'} · {candidateAssetLabel(candidate)} · {Math.round(candidate.score * 100)}%</small>
                        </div>
                        {candidate.lyrics?.value && (
                          <details className="provider-test-lyrics">
                            <summary>查看歌词</summary>
                            <pre>{candidate.lyrics.value}</pre>
                          </details>
                        )}
                      </article>
                    );
                  })}
                </div>
              ) : <div className="provider-test-empty">没有候选。请先查看下方日志，确认查询参数和数据源是否返回了结果。</div>}
              {(testResponse.logs?.length ?? 0) > 0 && (
                <details className="provider-test-logs" open>
                  <summary>抓取与封面探测日志（{testResponse.logs!.length} 条）</summary>
                  <div>{testResponse.logs!.map((log, index) => <div className={cn('provider-test-log', `is-${log.level}`)} key={`${log.stage}-${index}`}><span>{log.stage}</span><p><strong>{log.message}</strong>{formatLogDetails(log.details) && <small>{formatLogDetails(log.details)}</small>}</p></div>)}</div>
                </details>
              )}
            </div>
          )}
        </section>
      </div>,
      document.body,
    );
  })() : null;

  const providerConfigPanel = configProviderId ? (() => {
    const provider = providers.find((item) => item.id === configProviderId);
    if (!provider) return null;
    return createPortal(
      <div className="provider-test-overlay">
        <button className="provider-test-backdrop" aria-label="关闭数据源配置" onClick={() => setConfigProviderId(undefined)} />
        <section className="provider-config-panel" role="dialog" aria-modal="true" aria-label="数据源配置">
          <div className="provider-test-head">
            <div>
              <span className="eyebrow">PROVIDER CONFIGURATION</span>
              <h3>配置 {provider.name}</h3>
              <p>这些字段由当前策略声明，保存后会立即用于后续搜索；不会写入音乐文件。</p>
            </div>
            <button className="icon-button" title="关闭数据源配置" onClick={() => setConfigProviderId(undefined)}><X size={17} /></button>
          </div>
          <form className="provider-config-form" onSubmit={(event) => void saveProviderConfig(event)}>
            {(provider.config ?? []).map((field) => (
              <label key={field.key}>
                <span>{field.label}{field.required && <em>必填</em>}</span>
                <input
                  aria-label={field.label}
                  type={field.type === 'password' ? 'password' : field.type === 'number' ? 'number' : field.type === 'url' ? 'url' : 'text'}
                  value={configValues[field.key] ?? ''}
                  placeholder={field.secret && field.configured ? '已配置，留空保持不变' : field.placeholder}
                  onChange={(event) => setConfigValues((current) => ({...current, [field.key]: event.target.value}))}
                />
                {field.description && <small>{field.description}</small>}
              </label>
            ))}
            {configError && <div className="provider-test-error"><CircleAlert size={14} /> {configError}</div>}
            <div className="provider-config-actions">
              {!configResetPending ? (
                <button className="provider-config-reset-button" type="button" onClick={() => setConfigResetPending(true)} disabled={configSaving || configResetting}>恢复默认配置</button>
              ) : (
                <span className="provider-config-reset-confirm">
                  <small>会清除自定义地址和鉴权</small>
                  <button className="provider-config-reset-button" type="button" onClick={() => void resetProviderConfig()} disabled={configSaving || configResetting}>{configResetting ? '恢复中…' : '确认恢复'}</button>
                  <button className="secondary-button" type="button" onClick={() => setConfigResetPending(false)} disabled={configResetting}>保留当前</button>
                </span>
              )}
              <button className="secondary-button" type="button" onClick={() => setConfigProviderId(undefined)} disabled={configSaving || configResetting}>取消</button>
              <button className="primary-button" type="submit" disabled={configSaving || configResetting}>
                {configSaving ? <LoaderCircle size={14} className="spin" /> : <Save size={14} />}
                {configSaving ? '保存中…' : '保存并应用'}
              </button>
            </div>
          </form>
        </section>
      </div>,
      document.body,
    );
  })() : null;

  const directoryProbePanel = directoryProbeOpen ? createPortal(
    <div className="provider-test-overlay">
      <button className="provider-test-backdrop" aria-label="关闭目录探测" onClick={() => setDirectoryProbeOpen(false)} />
      <section className="provider-config-panel directory-probe-panel" role="dialog" aria-modal="true" aria-label="目录探测">
        <div className="provider-test-head">
          <div>
            <span className="eyebrow">LIBRARY DIRECTORY PROBE</span>
            <h3>验证音乐目录</h3>
            <p>先读取目录权限和音频样本；确认后会添加或切换曲库并排队扫描，不会直接修改文件。</p>
          </div>
          <button className="icon-button" title="关闭目录探测" onClick={() => setDirectoryProbeOpen(false)}><X size={17} /></button>
        </div>
        <form className="provider-config-form" onSubmit={(event) => void executeDirectoryProbe(event)}>
          <label><span>目录路径</span><input aria-label="目录路径" value={directoryPath} onChange={(event) => setDirectoryPath(event.target.value)} placeholder="/home/user/Music" autoFocus /></label>
          <button className="primary-button" type="submit" disabled={directoryProbing}>{directoryProbing ? <LoaderCircle size={14} className="spin" /> : <FolderCog size={14} />} {directoryProbing ? '探测中…' : '开始探测'}</button>
          {directoryProbeError && <div className="provider-test-error"><CircleAlert size={14} /> {directoryProbeError}</div>}
          {directoryProbe && (
            <div className="directory-probe-result">
              <div className="directory-probe-path"><strong>{directoryProbe.name}</strong><code>{directoryProbe.path}</code></div>
              <div className="directory-probe-stats">
                <span className={directoryProbe.readable ? 'is-good' : 'is-bad'}>{directoryProbe.readable ? '可读取' : '不可读取'}</span>
                <span className={directoryProbe.writable ? 'is-good' : 'is-bad'}>{directoryProbe.writable ? '权限位可写' : '权限位只读'}</span>
                <span><strong>{directoryProbe.audioFiles}</strong> 首音频</span>
                <span><strong>{directoryProbe.folders}</strong> 个子目录</span>
              </div>
              <div className="directory-probe-formats">{Object.entries(directoryProbe.formats).map(([format, count]) => <span key={format}>{format.toUpperCase()} <strong>{count}</strong></span>)}</div>
              {(directoryProbe.warnings?.length ?? 0) > 0 && <div className="directory-probe-warnings">{directoryProbe.warnings!.map((warning) => <p key={warning}><CircleAlert size={13} /> {warning}</p>)}</div>}
              <small>添加或切换会排队扫描并更新当前曲库；有运行中或待审核任务时会被拒绝。未显式提供 <code>--music-dir</code> 时，重启会恢复最近一次选择。</small>
              <button className="primary-button" type="button" disabled={!directoryProbe.readable || directorySwitching} onClick={() => void switchActiveLibrary()}>{directorySwitching ? <LoaderCircle size={14} className="spin" /> : <FolderCog size={14} />} {directorySwitching ? '处理中…' : '添加并切换到此目录'}</button>
            </div>
          )}
        </form>
      </section>
    </div>,
    document.body,
  ) : null;

  return (
    <div className="section-page settings-page">
      <header className="section-hero">
        <div>
          <div className="eyebrow">SYSTEM CONFIGURATION</div>
          <h1>设置</h1>
          <p>管理受控音乐目录、元数据来源和单二进制运行参数。</p>
        </div>
        <button className="primary-button" onClick={() => onNotice(apiReadMode === 'real' ? '数据源开关已实时保存到 SQLite' : '设置已保存到 Mock 配置层')}><Save size={15} /> 保存设置</button>
      </header>

      <div className="settings-layout">
        <nav className="settings-nav">
          <button className={cn(tab === 'libraries' && 'is-active')} onClick={() => setTab('libraries')}>
            <FolderCog size={17} /><span><strong>音乐目录</strong><small>根目录与扫描策略</small></span><ChevronRight size={15} />
          </button>
          <button className={cn(tab === 'providers' && 'is-active')} onClick={() => setTab('providers')}>
            <Network size={17} /><span><strong>数据源</strong><small>搜索、歌词与封面</small></span><ChevronRight size={15} />
          </button>
          <button className={cn(tab === 'system' && 'is-active')} onClick={() => setTab('system')}>
            <ServerCog size={17} /><span><strong>系统</strong><small>服务、认证与历史</small></span><ChevronRight size={15} />
          </button>
        </nav>

        <section className="settings-content">
          {tab === 'providers' && (
            <>
              <div className="settings-content-head">
                <div><h2>音乐数据源</h2><p>数据源按能力组合；单个来源失败不会影响其他结果。</p></div>
                <span className="settings-readonly-hint">内置策略可配置；自定义策略将在后续版本开放</span>
              </div>
              <div className="provider-grid">
                {providers.map((provider) => (
                  <article key={provider.id} className={cn('provider-card', provider.enabled && 'is-enabled')}>
                    <div className="provider-card-head">
                      <span className="provider-monogram" style={{'--provider-accent': provider.accent} as CSSProperties}>
                        {provider.shortName}
                      </span>
                      <span>
                        <strong>{provider.name}</strong>
                        <small className={`health-${provider.health}`}>
                          {provider.health === 'ready' ? <Check size={11} /> : <CircleAlert size={11} />}
                          {healthText[provider.health]}
                        </small>
                      </span>
                      <button
                        className="provider-toggle"
                        title={provider.enabled ? '停用数据源' : '启用数据源'}
                        onClick={() => void toggleProvider(provider)}
                      >
                        {provider.enabled ? <ToggleRight size={28} /> : <ToggleLeft size={28} />}
                      </button>
                    </div>
                    <p>{provider.description}</p>
                    <div className="provider-caps">
                      {provider.capabilities.map((capability) => <span key={capability}>{capability}</span>)}
                    </div>
                    {provider.experimental && <div className="experimental-note"><CircleAlert size={13} /> 实验性非官方接口</div>}
                    <div className="provider-foot">
                      <small>{provider.quotaLabel}</small>
                      <button disabled={!provider.enabled} onClick={() => openProviderTest(provider)}>
                        <TestTube2 size={13} />
                        测试查询
                      </button>
                      <button onClick={() => openProviderConfig(provider)}>配置</button>
                    </div>
                    {provider.configError && <small className="provider-config-error">配置未生效：{provider.configError}</small>}
                  </article>
                ))}
              </div>
              {providerTestPanel}
              {providerConfigPanel}
			  <div className="settings-policy-note">
				<ShieldCheck size={18} />
				<div><strong>来源用途策略</strong><span>远程封面只通过后端候选 ID、安全下载和图片验证后写入；实验性来源需要已实现适配器才能启用。</span></div>
              </div>
            </>
          )}

          {tab === 'libraries' && (
            <>
              <div className="settings-content-head">
                <div><h2>音乐目录</h2><p>后端只允许访问这里注册的根目录。</p></div>
                <button className="primary-button" onClick={openDirectoryProbe}><Plus size={15} /> 验证目录</button>
              </div>
              {libraryError && <div className="provider-test-error"><CircleAlert size={14} /> {libraryError}</div>}
              <article className="library-setting-card" aria-busy={libraryLoading}>
                <div className="library-setting-title">
                  <span><Database size={20} /></span>
                  <div><strong>{library?.name ?? (libraryLoading ? '正在读取曲库…' : '未配置曲库')}</strong><code>{library?.rootPath || library?.rootLabel || '请检查 --music-dir / TAGGER_MUSIC_DIR'}</code></div>
                  <em>{library?.writable ? <><Check size={12} /> 可读写</> : <><CircleAlert size={12} /> 只读或不可用</>}</em>
                </div>
                <div className="library-setting-summary">
                  <span><strong>{library?.trackCount ?? '—'}</strong> 首曲目</span>
                  <span><strong>{library?.folderCount ?? '—'}</strong> 个文件夹</span>
                  <span><strong>{library?.lastScanLabel ?? '—'}</strong> 最近扫描</span>
                  <button className="secondary-button" disabled={!library || libraryScanning} onClick={() => void runLibraryScan()}>
                    <RefreshCw size={14} className={libraryScanning ? 'spin' : undefined} /> {libraryScanning ? '扫描中…' : '重新扫描'}
                  </button>
                </div>
                <div className="library-setting-grid">
                  <label><span>扫描模式</span><select aria-label="扫描模式" value="manual" disabled onChange={() => undefined}><option value="manual">仅手动 / 任务队列</option></select></label>
                  <label><span>定时对账</span><select aria-label="定时对账" value="off" disabled onChange={() => undefined}><option value="off">未启用</option></select></label>
                  <label><span>符号链接</span><select aria-label="符号链接" value="off" disabled onChange={() => undefined}><option value="off">不跟随</option></select></label>
                </div>
                <div className="ignore-box"><span>默认忽略规则</span><code>@eaDir/　.Trash-*/　.DS_Store</code><small>由扫描器固定处理</small></div>
              </article>
              <div className="registered-libraries" aria-label="已注册曲库">
                <div className="registered-libraries-head"><div><h3>已注册曲库</h3><p>切换只会改变当前浏览和写入目标，不会删除其他曲库的索引。</p></div><span>{libraries.length} 个</span></div>
                {libraries.length === 0 && <div className="empty-library-state"><Database size={18} /><span>还没有已完成扫描的曲库。验证一个本地目录即可添加。</span></div>}
                {libraries.map((item) => (
                  <article className={cn('registered-library-row', item.active && 'is-active')} key={item.id}>
                    <span className="registered-library-icon"><Database size={17} /></span>
                    <div><strong>{item.name}</strong><code>{item.rootPath || item.rootLabel}</code><small>{item.trackCount} 首 · {item.folderCount} 个目录 · {item.lastScanLabel || '尚未扫描'}</small></div>
                    {item.active ? <span className="registered-library-active"><Check size={13} /> 当前</span> : <button className="secondary-button" disabled={directorySwitching} onClick={() => void switchRegisteredLibrary(item)}>{directorySwitching ? <LoaderCircle size={13} className="spin" /> : <RefreshCw size={13} />} 切换</button>}
                  </article>
                ))}
              </div>
              <div className="settings-policy-note">
                <FolderCog size={18} />
				<div><strong>{apiReadMode === 'real' ? '当前使用真实曲库索引' : '当前使用 Mock 数据'}</strong><span>{apiReadMode === 'real' ? '目录权限、索引和扫描任务由 Go 后端管理。' : '连接 Go 后端后，这里会读取真实目录权限。'}</span></div>
              </div>
              {directoryProbePanel}
            </>
          )}

          {tab === 'system' && (
            <>
              <div className="settings-content-head"><div><h2>系统与安全</h2><p>单进程运行参数和文件修改保护。</p></div></div>
              <div className="system-settings">
                <section className="theme-settings-row">
                  <div className="system-icon"><Palette size={19} /></div>
                  <div><strong>主题与字体</strong><p>配色和文字排版只保存在当前浏览器，不影响音乐文件。</p></div>
                  <div className="theme-settings-controls">
                    <div className="theme-option-grid" role="group" aria-label="界面主题">
                      {themeOptions.map((option) => (
                        <button key={option.id} type="button" className={cn('theme-option', theme === option.id && 'is-selected')} aria-pressed={theme === option.id} onClick={() => onThemeChange(option.id)}>
                          <span className="theme-swatch" aria-hidden="true"><i style={{background: option.swatch[0]}} /><i style={{background: option.swatch[1]}} /><i style={{background: option.swatch[2]}} /></span>
                          <span><strong>{option.label}</strong><small>{option.description}</small></span>
                        </button>
                      ))}
                    </div>
                    <label className="system-select theme-font-select"><span><Type size={13} />界面字体</span><select aria-label="界面字体" value={font} onChange={(event) => onFontChange(event.target.value as FontID)}>
                      {fontOptions.map((option) => <option key={option.id} value={option.id}>{option.label}</option>)}
                    </select></label>
                  </div>
                </section>
                <section>
                  <div className="system-icon"><ServerCog size={19} /></div>
                  <div><strong>HTTP 服务</strong><p>默认仅监听本机，外网访问建议使用 HTTPS 反向代理。</p></div>
                  <div className="system-readonly">
                    <span>启动监听</span>
                    <code>{systemInfo?.listen || '由 --listen / TAGGER_LISTEN 决定'}</code>
                    <small>服务启动后不能热切换；修改启动参数后请重新启动。</small>
                  </div>
                </section>
                <section>
                  <div className="system-icon"><KeyRound size={19} /></div>
                  <div><strong>访问鉴权令牌</strong><p>单用户 Bearer token；通过 --auth-token 或 TAGGER_AUTH_TOKEN 启用，未配置时不鉴权。</p></div>
                  <span className="system-value">启动参数配置</span>
                </section>
                <section>
                  <div className="system-icon"><ShieldCheck size={19} /></div>
                  <div><strong>安全写入</strong><p>临时副本、重读校验、原子替换和标签历史。</p></div>
                  <label className="switch-control">
                    <input type="checkbox" checked={writeHistory} role="switch" aria-label="启用写前历史" onChange={(event) => void updateWriteHistory(event.target.checked)} />
                    <span className="switch-track" aria-hidden="true"><span /></span>
                    <span>启用写前历史</span>
                  </label>
                </section>
                <section>
                  <div className="system-icon"><Database size={19} /></div>
                  <div><strong>历史保留</strong><p>标签和封面修订按内容 hash 去重保存。</p></div>
                  <label className="system-select"><span>每文件</span><select aria-label="历史保留次数" value={historyRetention} onChange={(event) => void updateHistoryRetention(Number(event.target.value) as HistoryRetention)}>
                    {historyRetentionOptions.map((value) => <option key={value} value={value}>最近 {value} 次</option>)}
                  </select></label>
                </section>
                <section>
                  <div className="system-icon"><ImagePlus size={19} /></div>
                  <div><strong>封面占位</strong><p>没有真实封面时，列表和候选结果默认显示空白占位。</p></div>
                  <label className="switch-control">
                    <input type="checkbox" checked={showGeneratedCovers} role="switch" aria-label="使用生成式占位" onChange={(event) => { onShowGeneratedCoversChange(event.target.checked); onNotice(event.target.checked ? '已启用生成式封面占位' : '已关闭生成式封面占位'); }} />
                    <span className="switch-track" aria-hidden="true"><span /></span>
                    <span>使用生成式占位</span>
                  </label>
                </section>
              </div>
            </>
          )}
        </section>
      </div>
    </div>
  );
}
