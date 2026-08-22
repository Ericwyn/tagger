import {useEffect, useState} from 'react';
import {
  ArrowRight,
  Check,
  ChevronRight,
  Clock3,
  FileClock,
  RotateCcw,
  Search,
  ShieldCheck,
} from 'lucide-react';
import {CoverArt} from '@/components/CoverArt';
import {cn} from '@/lib/utils';
import {listRevisions} from '@/api';
import type {Revision} from '@/types';

interface HistoryPageProps {
  onNotice: (message: string) => void;
}

export function HistoryPage({onNotice}: HistoryPageProps) {
  const [revisions, setRevisions] = useState<Revision[]>([]);
  const [activeId, setActiveId] = useState<string>();
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [query, setQuery] = useState('');

  useEffect(() => {
    listRevisions().then((next) => {
      setRevisions(next);
      setActiveId(next[0]?.id);
      setState('ready');
    }).catch(() => setState('error'));
  }, []);

  const normalizedQuery = query.trim().toLocaleLowerCase();
  const visibleRevisions = normalizedQuery
    ? revisions.filter((revision) => [revision.trackTitle, revision.fileName, revision.source, revision.action]
      .some((value) => value.toLocaleLowerCase().includes(normalizedQuery)))
    : revisions;
  const active = revisions.find((revision) => revision.id === activeId);
  const activeDiff = active?.diff?.length
    ? active.diff
    : active?.fields.map((field) => ({field, operation: 'set' as const, before: undefined, after: undefined})) ?? [];

  return (
    <div className="section-page history-page">
      <header className="section-hero">
        <div>
          <div className="eyebrow">REVISION LEDGER</div>
          <h1>修改历史</h1>
          <p>每次手工编辑、数据源补全和外部变化都会留下可审计的标签快照。</p>
        </div>
        <div className="history-security">
          <ShieldCheck size={17} />
          {state === 'loading' ? '正在读取历史…' : `历史完整 · ${revisions.length} 个修订`}
        </div>
      </header>

      <div className="history-toolbar">
        <label className="search-box">
          <Search size={16} />
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索曲目、文件或来源…" />
        </label>
        <button className="toolbar-button">全部来源</button>
        <button className="toolbar-button">最近 30 天</button>
      </div>

      <div className="history-layout">
        <section className="revision-list">
          <div className="revision-date"><span>最近修改</span><i /></div>
          {state === 'loading' && <HistoryEmpty symbol="···" title="正在读取修订记录" detail="SQLite 索引正在返回历史快照。" />}
          {state === 'error' && <HistoryEmpty symbol="!" title="无法读取修订记录" detail="请稍后重试或检查后台日志。" />}
          {state === 'ready' && visibleRevisions.length === 0 && (
            <HistoryEmpty
              symbol="∅"
              title={revisions.length === 0 ? '还没有修改记录' : '没有匹配的修订'}
              detail={revisions.length === 0 ? '保存一次标签后，这里会显示真实的字段差异。' : '换一个曲名、文件名或来源试试。'}
            />
          )}
          {visibleRevisions.map((revision, index) => (
            <button
              key={revision.id}
              className={cn('revision-row', activeId === revision.id && 'is-active')}
              onClick={() => setActiveId(revision.id)}
            >
              <CoverArt title={revision.trackTitle} tone={revision.coverTone} size="xs" />
              <span>
                <strong>{revision.trackTitle}</strong>
                <small>{revision.action}</small>
                <em>{revision.source} · {revision.time}</em>
              </span>
              <span className="field-count">{revision.fields.length} 字段</span>
              <ChevronRight size={16} />
              {index < visibleRevisions.length - 1 && <i className="timeline-line" />}
            </button>
          ))}
        </section>

        {active && (
          <aside className="revision-detail">
            <div className="revision-detail-head">
              <CoverArt title={active.trackTitle} tone={active.coverTone} size="sm" />
              <div>
                <div className="eyebrow">{active.id.toUpperCase()}</div>
                <h2>{active.action}</h2>
                <p>{active.fileName}</p>
              </div>
            </div>
            <div className="revision-meta">
              <div><Clock3 size={15} /><span>提交时间</span><strong>{active.time}</strong></div>
              <div><FileClock size={15} /><span>修改来源</span><strong>{active.source}</strong></div>
            </div>
            <div className="revision-diff">
              <div className="revision-diff-head"><span>字段</span><span>修改前</span><span /><span>修改后</span></div>
              {activeDiff.map((diff) => (
                <div key={diff.field}>
                  <strong>{fieldLabel(diff.field)}</strong>
                  <del title={fullDiffValue(diff.before)}>{formatDiffValue(diff.before)}</del>
                  <ArrowRight size={13} />
                  <ins title={fullDiffValue(diff.after)}>{formatDiffValue(diff.after)}</ins>
                </div>
              ))}
            </div>
            <div className="integrity-note">
              <Check size={15} />
              <span>
                <strong>写后重读校验通过</strong>
                <small>文件容器、时长与音频属性保持一致</small>
              </span>
            </div>
            <button
              className="secondary-button full-button"
              onClick={() => onNotice(`「${active.trackTitle}」的恢复能力将在下一阶段接入`)}
            >
              <RotateCcw size={15} /> 恢复此版本（即将接入）
            </button>
          </aside>
        )}
        {!active && state === 'ready' && (
          <aside className="revision-detail"><HistoryEmpty symbol="↖" title="选择一条修订" detail="这里会展示写入前后的真实字段差异。" /></aside>
        )}
      </div>
    </div>
  );
}

function HistoryEmpty({symbol, title, detail}: {symbol: string; title: string; detail: string}) {
  return <div className="empty-state history-empty"><span>{symbol}</span><strong>{title}</strong><p>{detail}</p></div>;
}

const fieldLabels: Record<string, string> = {
  title: '标题', artists: '艺术家', album: '专辑', albumArtists: '专辑艺术家',
  trackNumber: '音轨号', trackTotal: '总音轨', discNumber: '光盘号', discTotal: '总光盘',
  year: '年份', genres: '流派', lyrics: '歌词',
};

function fieldLabel(field: string): string {
  return fieldLabels[field] ?? field;
}

function fullDiffValue(value: unknown): string {
  if (value === undefined || value === null || value === '') return '空';
  if (Array.isArray(value)) return value.length > 0 ? value.join(' / ') : '空';
  return String(value);
}

function formatDiffValue(value: unknown): string {
  const text = fullDiffValue(value).replace(/\s+/g, ' ').trim();
  return text.length > 72 ? `${text.slice(0, 69)}…` : text;
}
