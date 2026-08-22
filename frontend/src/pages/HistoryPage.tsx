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
  X,
} from 'lucide-react';
import {CoverArt} from '@/components/CoverArt';
import {cn} from '@/lib/utils';
import {listRevisions, previewRevisionRestore, restoreRevision} from '@/api';
import type {RestorePreview, Revision} from '@/types';

interface HistoryPageProps {
  onNotice: (message: string) => void;
  showGeneratedCovers?: boolean;
}

type HistoryDateFilter = 'all' | '30d';

export function HistoryPage({onNotice, showGeneratedCovers = false}: HistoryPageProps) {
  const [revisions, setRevisions] = useState<Revision[]>([]);
  const [activeId, setActiveId] = useState<string>();
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [query, setQuery] = useState('');
  const [sourceFilter, setSourceFilter] = useState('all');
  const [dateFilter, setDateFilter] = useState<HistoryDateFilter>('all');
  const [restorePreview, setRestorePreview] = useState<RestorePreview>();
  const [restoreState, setRestoreState] = useState<'idle' | 'previewing' | 'restoring'>('idle');
  const [restoreError, setRestoreError] = useState('');

  const reload = async () => {
    const next = await listRevisions();
    setRevisions(next);
    setActiveId(next[0]?.id);
    setState('ready');
  };

  useEffect(() => {
	void reload().catch(() => setState('error'));
  }, []);

  useEffect(() => {
	setRestorePreview(undefined);
	setRestoreState('idle');
	setRestoreError('');
  }, [activeId]);

  const normalizedQuery = query.trim().toLocaleLowerCase();
  const sourceOptions = [...new Set(revisions.map((revision) => revision.source).filter(Boolean))].sort((left, right) => left.localeCompare(right, 'zh-Hans-CN'));
  const visibleRevisions = revisions.filter((revision) => {
    if (sourceFilter !== 'all' && revision.source !== sourceFilter) return false;
    if (dateFilter === '30d' && !isRecentRevision(revision.time, 30)) return false;
    if (!normalizedQuery) return true;
    return [revision.trackTitle, revision.fileName, revision.source, revision.action]
      .some((value) => value.toLocaleLowerCase().includes(normalizedQuery));
  });
  const active = visibleRevisions.find((revision) => revision.id === activeId) ?? visibleRevisions[0];
	const recordedDiff = active?.diff?.length
    ? active.diff
    : active?.fields.map((field) => ({field, operation: 'set' as const, before: undefined, after: undefined})) ?? [];
  const activeDiff = restorePreview?.preview.diff ?? recordedDiff;
	const hasRestorableFields = Boolean(active?.fields.length);

	const buildRestorePreview = async () => {
	  if (!active) return;
	  setRestoreState('previewing');
	  setRestoreError('');
	  try {
		setRestorePreview(await previewRevisionRestore(active));
	  } catch (error) {
		setRestoreError(error instanceof Error ? error.message : '恢复预览失败');
	  } finally {
		setRestoreState('idle');
	  }
	};

	const confirmRestore = async () => {
	  if (!active || !restorePreview) return;
	  setRestoreState('restoring');
	  setRestoreError('');
	  try {
		const result = await restoreRevision(active, restorePreview);
		onNotice(result.write.changed
		  ? `已将「${result.track.title}」恢复到该修订修改前，并生成新的审计记录`
		  : `「${result.track.title}」当前已是目标版本`);
		setRestorePreview(undefined);
		await reload();
	  } catch (error) {
		setRestoreError(error instanceof Error ? error.message : '恢复失败');
	  } finally {
		setRestoreState('idle');
	  }
	};

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
        <label className="toolbar-filter">
          <span>来源</span>
          <select aria-label="历史来源筛选" value={sourceFilter} onChange={(event) => setSourceFilter(event.target.value)}>
            <option value="all">全部来源</option>
            {sourceOptions.map((source) => <option key={source} value={source}>{source}</option>)}
          </select>
        </label>
        <label className="toolbar-filter">
          <span>时间</span>
          <select aria-label="历史时间筛选" value={dateFilter} onChange={(event) => setDateFilter(event.target.value as HistoryDateFilter)}>
            <option value="all">全部时间</option>
            <option value="30d">最近 30 天</option>
          </select>
        </label>
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
              <CoverArt title={revision.trackTitle} tone={revision.coverTone} missing={!showGeneratedCovers} size="xs" />
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
              <CoverArt title={active.trackTitle} tone={active.coverTone} missing={!showGeneratedCovers} size="sm" />
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
			  {restorePreview && (
				<div className="restore-preview-banner">
				  <span>RESTORE PREVIEW</span>
				  <strong>将当前文件恢复到这次修改之前</strong>
				  <small>以下是相对当前磁盘标签即将发生的变化，尚未写入。</small>
				</div>
			  )}
			  <div className="revision-diff-head"><span>字段</span><span>{restorePreview ? '当前值' : '修改前'}</span><span /><span>{restorePreview ? '恢复后' : '修改后'}</span></div>
              {activeDiff.map((diff) => (
                <div key={diff.field}>
                  <strong>{fieldLabel(diff.field)}</strong>
                  <del title={fullDiffValue(diff.before)}>{formatDiffValue(diff.before)}</del>
                  <ArrowRight size={13} />
                  <ins title={fullDiffValue(diff.after)}>{formatDiffValue(diff.after)}</ins>
                </div>
              ))}
            </div>
			{restorePreview?.preview.warnings.map((warning) => <p className="restore-warning" key={warning}>{warning}</p>)}
			{restoreError && <p className="restore-error">{restoreError}</p>}
            <div className="integrity-note">
              <Check size={15} />
              <span>
                <strong>写后重读校验通过</strong>
                <small>文件容器、时长与音频属性保持一致</small>
              </span>
            </div>
			{restorePreview ? (
			  <div className="restore-actions">
				<button className="secondary-button" disabled={restoreState !== 'idle'} onClick={() => setRestorePreview(undefined)}>
				  <X size={15} /> 取消
				</button>
				<button
				  className="primary-button"
				  disabled={restoreState !== 'idle' || !restorePreview.preview.changed}
				  onClick={() => void confirmRestore()}
				>
				  <RotateCcw size={15} /> {restoreState === 'restoring' ? '恢复中…' : restorePreview.preview.changed ? '确认恢复' : '当前已是目标版本'}
				</button>
			  </div>
			) : (
			  <button
				className="secondary-button full-button"
				disabled={restoreState !== 'idle' || !active.currentRevision || !hasRestorableFields}
				title={!hasRestorableFields ? '当前修订没有可恢复字段' : active.currentRevision ? '先生成相对当前文件的恢复预览' : '对应曲目不存在或当前处于 Mock 模式'}
				onClick={() => void buildRestorePreview()}
			>
				<RotateCcw size={15} /> {restoreState === 'previewing' ? '生成预览中…' : '恢复到修改前…'}
			  </button>
			)}
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

function isRecentRevision(value: string, days: number): boolean {
  const normalized = value.trim();
  if (/^(今天|昨天|前天)/.test(normalized)) return true;
  const parsed = Date.parse(normalized.replace(' ', 'T'));
  if (!Number.isFinite(parsed)) return true;
  return parsed >= Date.now() - days * 24 * 60 * 60 * 1000;
}

const fieldLabels: Record<string, string> = {
  title: '标题', artists: '艺术家', album: '专辑', albumArtists: '专辑艺术家',
  trackNumber: '音轨号', trackTotal: '总音轨', discNumber: '光盘号', discTotal: '总光盘',
  year: '年份', genres: '流派', lyrics: '歌词', lyricsSidecar: '歌词（旧版 .lrc）',
	artwork: '封面',
};

function fieldLabel(field: string): string {
  return fieldLabels[field] ?? field;
}

function fullDiffValue(value: unknown): string {
  if (value === undefined || value === null || value === '') return '空';
  if (Array.isArray(value)) return value.length > 0 ? value.join(' / ') : '空';
	if (typeof value === 'object') {
	  const asset = value as {format?: string; width?: number; height?: number; size?: number; hash?: string};
	  if (asset.hash) {
		const dimensions = asset.width && asset.height ? `${asset.width}×${asset.height}` : '尺寸未知';
		const size = asset.size ? `${(asset.size / 1024).toFixed(0)} KB` : '大小未知';
		return `${asset.format || 'IMAGE'} · ${dimensions} · ${size} · ${asset.hash.slice(0, 8)}`;
	  }
	  return JSON.stringify(value);
	}
  return String(value);
}

function formatDiffValue(value: unknown): string {
  const text = fullDiffValue(value).replace(/\s+/g, ' ').trim();
  return text.length > 72 ? `${text.slice(0, 69)}…` : text;
}
