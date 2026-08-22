import {useEffect, useState, type CSSProperties} from 'react';
import {
  Check,
  ChevronRight,
  CircleAlert,
  Database,
  FolderCog,
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
import {listProviders} from '@/api';
import type {ProviderConfig} from '@/types';

interface SettingsPageProps {
  onNotice: (message: string) => void;
}

type SettingsTab = 'libraries' | 'providers' | 'system';

const healthText = {
  ready: '连接正常',
  degraded: '部分可用',
  misconfigured: '需要配置',
  disabled: '未启用',
};

export function SettingsPage({onNotice}: SettingsPageProps) {
  const [tab, setTab] = useState<SettingsTab>('providers');
  const [providers, setProviders] = useState<ProviderConfig[]>([]);
  const [testingId, setTestingId] = useState<string>();

  useEffect(() => {
    listProviders().then(setProviders);
  }, []);

  const toggleProvider = (id: string) => {
    setProviders((current) => current.map((provider) => provider.id === id
      ? {...provider, enabled: !provider.enabled, health: provider.enabled ? 'disabled' : provider.health === 'disabled' ? 'ready' : provider.health}
      : provider));
  };

  const testProvider = (id: string, name: string) => {
    setTestingId(id);
    window.setTimeout(() => {
      setTestingId(undefined);
      onNotice(`${name} 连接测试完成`);
    }, 700);
  };

  return (
    <div className="section-page settings-page">
      <header className="section-hero">
        <div>
          <div className="eyebrow">SYSTEM CONFIGURATION</div>
          <h1>设置</h1>
          <p>管理受控音乐目录、元数据来源和单二进制运行参数。</p>
        </div>
        <button className="primary-button" onClick={() => onNotice('设置已保存到 Mock 配置层')}><Save size={15} /> 保存设置</button>
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
                        onClick={() => toggleProvider(provider.id)}
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
                      <button disabled={testingId === provider.id} onClick={() => testProvider(provider.id, provider.name)}>
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
                <div><strong>来源用途策略</strong><span>Apple/iTunes promotional artwork 默认不能直接写入文件；实验性来源需要用户主动启用。</span></div>
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
                <div><strong>当前使用 Mock 数据</strong><span>真实 Go 后端完成后，这里会读取目录权限并对 TestMusic 执行试扫描。</span></div>
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
              </div>
            </>
          )}
        </section>
      </div>
    </div>
  );
}
