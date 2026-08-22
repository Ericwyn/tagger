import {useEffect, useMemo, useState} from 'react';
import {
  ArrowLeft,
  Check,
  ChevronDown,
  ChevronRight,
  CircleAlert,
  FileCheck2,
  LoaderCircle,
  Search,
  Sparkles,
  X,
} from 'lucide-react';
import {CoverArt} from '@/components/CoverArt';
import {cn, formatDuration} from '@/lib/utils';
import {candidatesFor} from '@/mock/data';
import {listTracks} from '@/api';
import type {MatchCandidate, Track} from '@/types';

interface ReviewPageProps {
  trackIds: string[];
  onBack: () => void;
  onComplete: () => void;
}

type ReviewState = 'accepted' | 'review' | 'skipped';

interface ReviewItem {
  track: Track;
  candidate: MatchCandidate;
  state: ReviewState;
}

export function ReviewPage({trackIds, onBack, onComplete}: ReviewPageProps) {
  const [items, setItems] = useState<ReviewItem[]>([]);
  const [activeId, setActiveId] = useState<string>();
  const [loading, setLoading] = useState(true);
  const [applying, setApplying] = useState(false);
  const [query, setQuery] = useState('');

  useEffect(() => {
    listTracks().then((allTracks) => {
      const ids = trackIds.length ? new Set(trackIds) : new Set(allTracks.filter((track) => track.album === '安泊猜想').map((track) => track.id));
      const next = allTracks
        .filter((track) => ids.has(track.id))
        .map((track) => {
          const candidate = candidatesFor(track)[0];
          return {
            track,
            candidate,
            state: candidate.score >= 0.92 ? 'accepted' as const : 'review' as const,
          };
        });
      setItems(next);
      setActiveId(next[0]?.track.id);
      setLoading(false);
    });
  }, [trackIds]);

  const visibleItems = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase();
    if (!normalized) return items;
    return items.filter(({track}) => [track.title, track.fileName, track.artists.join(' ')].some((value) => value.toLocaleLowerCase().includes(normalized)));
  }, [items, query]);

  const active = items.find(({track}) => track.id === activeId) ?? items[0];
  const accepted = items.filter((item) => item.state === 'accepted').length;
  const needsReview = items.filter((item) => item.state === 'review').length;
  const skipped = items.filter((item) => item.state === 'skipped').length;

  const setItemState = (trackId: string, state: ReviewState) => {
    setItems((current) => current.map((item) => item.track.id === trackId ? {...item, state} : item));
  };

  if (loading) {
    return <div className="app-loading"><LoaderCircle className="spin" size={22} /> 正在整理候选结果…</div>;
  }

  return (
    <div className="review-page">
      <header className="review-header">
        <button className="back-button" onClick={onBack}><ArrowLeft size={17} /> 返回曲库</button>
        <div>
          <div className="eyebrow">BATCH REVIEW / JOB DRAFT</div>
          <h1>审核抓取结果</h1>
          <p>自动分析已完成。确认字段差异后才会创建写入任务。</p>
        </div>
        <div className="review-stats">
          <div><strong>{accepted}</strong><span>高置信 / 已接受</span></div>
          <div><strong>{needsReview}</strong><span>需要人工确认</span></div>
          <div><strong>{skipped}</strong><span>已跳过</span></div>
        </div>
      </header>

      <div className="review-toolbar">
        <label className="search-box">
          <Search size={16} />
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="在本批次中搜索…" />
          {query && <button title="清除搜索" onClick={() => setQuery('')}><X size={14} /></button>}
        </label>
        <button className="toolbar-button">状态：全部 <ChevronDown size={13} /></button>
        <button className="toolbar-button">来源：全部 <ChevronDown size={13} /></button>
        <span />
        <button
          className="secondary-button"
          onClick={() => setItems((current) => current.map((item) => item.candidate.score >= 0.92 ? {...item, state: 'accepted'} : item))}
        >
          <Check size={15} /> 接受所有高置信
        </button>
      </div>

      <div className="review-layout">
        <section className="review-list">
          <div className="review-list-head">
            <span>本地曲目</span>
            <span>最佳候选</span>
            <span>状态</span>
          </div>
          {visibleItems.map((item) => (
            <button
              key={item.track.id}
              className={cn('review-row', active?.track.id === item.track.id && 'is-active')}
              onClick={() => setActiveId(item.track.id)}
            >
              <CoverArt title={item.track.title} artist={item.track.artists[0]} tone={item.track.coverTone} size="xs" />
              <span className="review-track-copy">
                <strong>{item.track.title}</strong>
                <small>{item.track.fileName}</small>
              </span>
              <span className="review-arrow"><ChevronRight size={15} /></span>
              <CoverArt title={item.candidate.title.value} artist={item.candidate.artists.value[0]} tone={item.candidate.coverTone} size="xs" />
              <span className="review-candidate-copy">
                <strong>{item.candidate.title.value}</strong>
                <small>{item.candidate.providerName} · {Math.round(item.candidate.score * 100)}%</small>
              </span>
              <span className={cn('review-state', `is-${item.state}`)}>
                {item.state === 'accepted' && <Check size={13} />}
                {item.state === 'review' && <CircleAlert size={13} />}
                {item.state === 'skipped' && <X size={13} />}
                {item.state === 'accepted' ? '已接受' : item.state === 'review' ? '待确认' : '已跳过'}
              </span>
            </button>
          ))}
        </section>

        {active && (
          <section className="review-detail">
            <div className="review-detail-head">
              <div className="record-comparison">
                <CoverArt title={active.track.title} artist={active.track.artists[0]} tone={active.track.coverTone} size="md" />
                <div className="comparison-line"><span /><Sparkles size={16} /><span /></div>
                <CoverArt title={active.candidate.title.value} artist={active.candidate.artists.value[0]} tone={active.candidate.coverTone} size="md" />
              </div>
              <div>
                <span className="confidence-badge"><Check size={13} /> {active.candidate.scoreLabel} · {Math.round(active.candidate.score * 100)}%</span>
                <h2>{active.track.title}</h2>
                <p>{active.track.artists.join(' / ')} · {formatDuration(active.track.durationSeconds)}</p>
                <small>候选来源：{active.candidate.providerName} / {active.candidate.externalId}</small>
              </div>
            </div>

            <div className="review-policy-note">
              <FileCheck2 size={18} />
              <div>
                <strong>当前策略：采用所选字段</strong>
                <span>不会删除候选中缺失的标签；评论和未知标签保持不变。</span>
              </div>
              <button>修改策略</button>
            </div>

            <div className="review-diff-table">
              <div className="review-diff-head"><span>采用</span><span>字段</span><span>当前值</span><span>候选值</span><span>来源</span></div>
              <ReviewDiff label="标题" current={active.track.title} next={active.candidate.title.value} source={active.candidate.providerName} />
              <ReviewDiff label="艺术家" current={active.track.artists.join(' / ')} next={active.candidate.artists.value.join(' / ')} source={active.candidate.providerName} />
              <ReviewDiff label="专辑" current={active.track.album || '空'} next={active.candidate.album.value} source={active.candidate.providerName} changed={!active.track.album} />
              <ReviewDiff
                label="音轨"
                current={active.track.trackNumber ? `${active.track.trackNumber} / ${active.track.trackTotal || '—'}` : '空'}
                next={`${active.candidate.trackNumber.value} / ${active.candidate.trackTotal.value}`}
                source={active.candidate.providerName}
                changed={!active.track.trackNumber}
              />
              <ReviewDiff label="年份" current={String(active.track.year || '空')} next={String(active.candidate.year.value)} source={active.candidate.providerName} />
              <ReviewDiff label="风格" current={active.track.genres.join(', ') || '空'} next={active.candidate.genres.value.join(', ')} source={active.candidate.providerName} />
            </div>

            <div className="review-detail-actions">
              <button className="danger-quiet" onClick={() => setItemState(active.track.id, 'skipped')}><X size={15} /> 跳过此曲</button>
              <button className="secondary-button">更换候选</button>
              <button className="primary-button" onClick={() => setItemState(active.track.id, 'accepted')}><Check size={15} /> 接受候选</button>
            </div>
          </section>
        )}
      </div>

      <footer className="review-footer">
        <div>
          <strong>{accepted}</strong> 首将写入，<strong>{needsReview}</strong> 首仍需确认，<strong>{skipped}</strong> 首跳过
        </div>
        <div>
          <button className="secondary-button" onClick={onBack}>保存草稿并返回</button>
          <button
            className="primary-button"
            disabled={accepted === 0 || applying}
            onClick={() => {
              setApplying(true);
              window.setTimeout(onComplete, 700);
            }}
          >
            {applying ? <LoaderCircle className="spin" size={15} /> : <FileCheck2 size={15} />}
            确认并创建写入任务
          </button>
        </div>
      </footer>
    </div>
  );
}

function ReviewDiff({
  label,
  current,
  next,
  source,
  changed = false,
}: {
  label: string;
  current: string;
  next: string;
  source: string;
  changed?: boolean;
}) {
  const [checked, setChecked] = useState(true);
  return (
    <div className={cn('review-diff-row', !checked && 'is-disabled')}>
      <button className={cn('square-check', checked && 'is-checked')} onClick={() => setChecked((value) => !value)}>
        {checked && <Check size={12} strokeWidth={3} />}
      </button>
      <strong>{label}</strong>
      <span>{current}</span>
      <span className={cn(changed && 'is-changed')}>{next}</span>
      <em>{source}</em>
    </div>
  );
}
