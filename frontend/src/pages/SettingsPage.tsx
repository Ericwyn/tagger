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
  Plus,
  RefreshCw,
  Save,
  ServerCog,
  ShieldCheck,
  TestTube2,
  ToggleLeft,
  ToggleRight,
  X,
} from 'lucide-react';
import {CoverArt} from '@/components/CoverArt';
import {cn} from '@/lib/utils';
import {apiReadMode, candidateArtworkURL, getLibrary, listProviders, rescanLibrary, testProvider as runProviderTest, updateProvider, waitForJob} from '@/api';
import type {CandidateSearchQuery, LibrarySummary, MatchCandidate, ProviderConfig, ProviderTestResponse} from '@/types';

interface SettingsPageProps {
  onNotice: (message: string) => void;
  showGeneratedCovers: boolean;
  onShowGeneratedCoversChange: (value: boolean) => void;
}

type SettingsTab = 'libraries' | 'providers' | 'system';

const healthText = {
  ready: '连接正常',
  degraded: '部分可用',
  misconfigured: '需要配置',
  disabled: '未启用',
};

const defaultTestQuery: CandidateSearchQuery = {
  title: 'Imagine', artists: ['John Lennon'], album: '', durationSeconds: 0,
};

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

export function SettingsPage({onNotice, showGeneratedCovers, onShowGeneratedCoversChange}: SettingsPageProps) {
  const [tab, setTab] = useState<SettingsTab>('providers');
  const [providers, setProviders] = useState<ProviderConfig[]>([]);
  const [testingId, setTestingId] = useState<string>();
  const [testProviderId, setTestProviderId] = useState<string>();
  const [testQuery, setTestQuery] = useState<CandidateSearchQuery>(defaultTestQuery);
  const [testArtistInput, setTestArtistInput] = useState(defaultTestQuery.artists.join(' / '));
  const [testResponse, setTestResponse] = useState<ProviderTestResponse>();
  const [testError, setTestError] = useState('');
  const [library, setLibrary] = useState<LibrarySummary>();
  const [libraryLoading, setLibraryLoading] = useState(false);
  const [libraryScanning, setLibraryScanning] = useState(false);
  const [libraryError, setLibraryError] = useState('');

  useEffect(() => {
    listProviders().then(setProviders);
  }, []);

  const loadLibrary = async () => {
    setLibraryLoading(true);
    setLibraryError('');
    try {
      setLibrary(await getLibrary());
    } catch (error) {
      setLibraryError(error instanceof Error ? error.message : '曲库信息读取失败');
    } finally {
      setLibraryLoading(false);
    }
  };

  useEffect(() => {
    if (tab === 'libraries') void loadLibrary();
  }, [tab]);

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

  const explainDirectoryConfiguration = () => {
    onNotice(apiReadMode === 'real'
      ? '当前单二进制通过 --music-dir 或 TAGGER_MUSIC_DIR 配置曲库；修改后请重启服务'
      : 'Mock 原型暂不切换真实目录；连接 Go 后端后由 --music-dir 配置受控根目录');
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
              </div>
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
                <button className="secondary-button"><Plus size={15} /> 添加自定义来源</button>
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
                      <button>配置</button>
                    </div>
                  </article>
                ))}
              </div>
              {providerTestPanel}
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
                <button className="primary-button" onClick={explainDirectoryConfiguration}><Plus size={15} /> 添加目录</button>
              </div>
              {libraryError && <div className="provider-test-error"><CircleAlert size={14} /> {libraryError}</div>}
              <article className="library-setting-card" aria-busy={libraryLoading}>
                <div className="library-setting-title">
                  <span><Database size={20} /></span>
                  <div><strong>{library?.name ?? (libraryLoading ? '正在读取曲库…' : '未配置曲库')}</strong><code>{library?.rootLabel ?? '请检查 --music-dir / TAGGER_MUSIC_DIR'}</code></div>
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
                  <label><span>扫描模式</span><select defaultValue="hybrid"><option value="hybrid">监听 + 定时对账</option><option>仅手动</option></select></label>
                  <label><span>定时对账</span><select defaultValue="6h"><option value="6h">每 6 小时</option><option>每天</option></select></label>
                  <label><span>符号链接</span><select defaultValue="off"><option value="off">不跟随</option><option>仅根目录内</option></select></label>
                </div>
                <div className="ignore-box"><span>忽略规则</span><code>@eaDir/　.Trash-*/　.DS_Store</code><button>编辑</button></div>
              </article>
              <div className="settings-policy-note">
                <FolderCog size={18} />
				<div><strong>{apiReadMode === 'real' ? '当前使用真实曲库索引' : '当前使用 Mock 数据'}</strong><span>{apiReadMode === 'real' ? '目录权限、索引和扫描任务由 Go 后端管理。' : '连接 Go 后端后，这里会读取真实目录权限。'}</span></div>
              </div>
            </>
          )}

          {tab === 'system' && (
            <>
              <div className="settings-content-head"><div><h2>系统与安全</h2><p>单进程运行参数和文件修改保护。</p></div></div>
              <div className="system-settings">
                <section>
                  <div className="system-icon"><ServerCog size={19} /></div>
                  <div><strong>HTTP 服务</strong><p>默认仅监听本机，外网访问建议使用 HTTPS 反向代理。</p></div>
                  <label><span>监听地址</span><input defaultValue="127.0.0.1:8080" /></label>
                </section>
                <section>
                  <div className="system-icon"><KeyRound size={19} /></div>
                  <div><strong>管理员会话</strong><p>HttpOnly session · Origin 检查 · CSRF 防护</p></div>
                  <button className="secondary-button">修改密码</button>
                </section>
                <section>
                  <div className="system-icon"><ShieldCheck size={19} /></div>
                  <div><strong>安全写入</strong><p>临时副本、重读校验、原子替换和标签历史。</p></div>
                  <label className="inline-switch"><input type="checkbox" defaultChecked /> 启用写前历史</label>
                </section>
                <section>
                  <div className="system-icon"><Database size={19} /></div>
                  <div><strong>历史保留</strong><p>标签和封面修订按内容 hash 去重保存。</p></div>
                  <label><span>每文件</span><select defaultValue="20"><option value="20">最近 20 次</option><option>最近 50 次</option></select></label>
                </section>
                <section>
                  <div className="system-icon"><ImagePlus size={19} /></div>
                  <div><strong>封面占位</strong><p>没有真实封面时，列表和候选结果默认显示空白占位。</p></div>
                  <label className="inline-switch"><input type="checkbox" checked={showGeneratedCovers} onChange={(event) => { onShowGeneratedCoversChange(event.target.checked); onNotice(event.target.checked ? '已启用生成式封面占位' : '已关闭生成式封面占位'); }} /> 使用生成式占位</label>
                </section>
              </div>
            </>
          )}
        </section>
      </div>
    </div>
  );
}
