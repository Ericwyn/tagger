import {useEffect, useState, type CSSProperties} from 'react';
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
  Save,
  ServerCog,
  ShieldCheck,
  TestTube2,
  ToggleLeft,
  ToggleRight,
} from 'lucide-react';
import {cn} from '@/lib/utils';
import {apiReadMode, listProviders, testProvider as runProviderTest, updateProvider} from '@/api';
import type {ProviderConfig} from '@/types';

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

export function SettingsPage({onNotice, showGeneratedCovers, onShowGeneratedCoversChange}: SettingsPageProps) {
  const [tab, setTab] = useState<SettingsTab>('providers');
  const [providers, setProviders] = useState<ProviderConfig[]>([]);
  const [testingId, setTestingId] = useState<string>();

  useEffect(() => {
    listProviders().then(setProviders);
  }, []);

	const toggleProvider = async (provider: ProviderConfig) => {
	try {
	  const updated = await updateProvider(provider, !provider.enabled);
	  setProviders((current) => current.map((item) => item.id === updated.id ? updated : item));
	  onNotice(`${updated.name} 已${updated.enabled ? '启用' : '停用'}并持久化`);
	} catch (error) {
	  onNotice(error instanceof Error ? error.message : '数据源设置保存失败');
	}
  };

	const testProvider = async (provider: ProviderConfig) => {
	setTestingId(provider.id);
	try { onNotice(await runProviderTest(provider)); }
	catch (error) { onNotice(error instanceof Error ? error.message : '连接测试失败'); }
	finally { setTestingId(undefined); }
  };

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
					  <button disabled={testingId === provider.id || !provider.enabled} onClick={() => void testProvider(provider)}>
                        {testingId === provider.id ? <LoaderCircle size={13} className="spin" /> : <TestTube2 size={13} />}
                        测试
                      </button>
                      <button>配置</button>
                    </div>
                  </article>
                ))}
              </div>
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
                <button className="primary-button"><Plus size={15} /> 添加目录</button>
              </div>
              <article className="library-setting-card">
                <div className="library-setting-title">
                  <span><Database size={20} /></span>
                  <div><strong>TestMusic</strong><code>/home/ericwyn/Downloads/TestMusic</code></div>
                  <em><Check size={12} /> 可读写</em>
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
