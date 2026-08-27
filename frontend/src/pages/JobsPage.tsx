import {useEffect, useRef, useState} from 'react';
import {
  Check,
  ChevronRight,
  CircleAlert,
  Clock3,
  FilePenLine,
  LoaderCircle,
  RefreshCw,
  ScanSearch,
  Sparkles,
  Tags,
  X,
} from 'lucide-react';
import {cn} from '@/lib/utils';
import {apiReadMode, cancelJob, listBatchEditItems, listJobs, resolveTracks, retryJob, subscribeJobEvents} from '@/api';
import {ConfirmDialog} from '@/components/ConfirmDialog';
import type {BatchEditItem, Job} from '@/types';

interface JobsPageProps {
  onOpenReview: (jobId: string) => void;
  focusJobId?: string;
}

const stateMeta: Record<Job['state'], {label: string; icon: typeof Check}> = {
  running: {label: '执行中', icon: LoaderCircle},
  review: {label: '等待审核', icon: CircleAlert},
	waiting: {label: '等待执行', icon: Clock3},
  succeeded: {label: '已完成', icon: Check},
  partial: {label: '部分完成', icon: CircleAlert},
	failed: {label: '失败', icon: CircleAlert},
	cancelled: {label: '已取消', icon: CircleAlert},
};

const kindIcon = {
  scan: ScanSearch,
  match: Sparkles,
  write: FilePenLine,
  batch_edit: Tags,
};

const batchItemStateText: Record<BatchEditItem['state'], string> = {
  pending: '等待中',
  written: '已写入',
  failed: '失败',
};

const batchFieldText: Record<string, string> = {
  album: '专辑',
  albumArtists: '专辑艺术家',
  genres: '风格',
  year: '年份',
  trackNumber: '音轨号',
  trackTotal: '总音轨',
  comment: '注释',
  composers: '作曲家',
  conductor: '指挥',
  lyricists: '作词家',
  copyright: '版权',
  bpm: 'BPM',
  isrc: 'ISRC',
};

function formatBatchValue(value: unknown): string {
  if (Array.isArray(value)) return value.length ? value.join(' / ') : '空';
  if (value === undefined || value === null || value === '') return '空';
  return String(value);
}

function jobProgress(job: Job): number {
  if (job.total <= 0) return job.state === 'succeeded' ? 100 : 0;
  return Math.min(100, Math.max(0, Math.round((job.processed / job.total) * 100)));
}

function isMatchReviewable(job: Job): boolean {
  return job.kind === 'match' && (job.state === 'review' || job.state === 'partial' || job.state === 'failed');
}

export function JobsPage({onOpenReview, focusJobId}: JobsPageProps) {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [selectedId, setSelectedId] = useState<string>();
	const focusConsumed = useRef(false);
	const [jobFilter, setJobFilter] = useState<'all' | 'running' | 'review' | 'failed'>('all');
	const [actionError, setActionError] = useState('');
	const [batchItems, setBatchItems] = useState<BatchEditItem[]>([]);
	const [trackNames, setTrackNames] = useState<Map<string, string>>(new Map());
	const [discardJob, setDiscardJob] = useState<Job>();
	const [discarding, setDiscarding] = useState(false);
	const [discardError, setDiscardError] = useState('');

	const refresh = () => listJobs().then((next) => {
	  setJobs(next);
	  setSelectedId((current) => {
	    if (!focusConsumed.current && focusJobId && next.some((job) => job.id === focusJobId)) {
	      focusConsumed.current = true;
	      return focusJobId;
	    }
	    return next.some((job) => job.id === current) ? current : next[0]?.id;
	  });
	});

	useEffect(() => {
	  focusConsumed.current = false;
	}, [focusJobId]);

	const updateJob = (next: Job) => {
	  setJobs((current) => current.map((job) => job.id === next.id ? next : job));
	};

	const discardReviewJob = async () => {
	  if (!discardJob || discarding) return;
	  setDiscarding(true);
	  setDiscardError('');
	  try {
	    const next = await cancelJob(discardJob.id);
	    if (next) updateJob(next);
	    setDiscardJob(undefined);
	  } catch (error) {
	    setDiscardError(error instanceof Error ? error.message : '丢弃审核任务失败');
	  } finally {
	    setDiscarding(false);
	  }
	};

  useEffect(() => {
	void refresh();
	if (apiReadMode === 'mock') return;
	const timer = window.setInterval(() => void refresh(), 1000);
	return () => window.clearInterval(timer);
  }, []);

	useEffect(() => {
	  if (apiReadMode === 'mock' || !selectedId) return;
	  return subscribeJobEvents(selectedId, updateJob);
	}, [selectedId]);

	const visibleJobs = jobs.filter((job) => {
	  if (jobFilter === 'running') return job.state === 'running' || job.state === 'waiting';
	  if (jobFilter === 'review') return job.state === 'review';
	  if (jobFilter === 'failed') return job.state === 'failed' || job.state === 'partial';
	  return true;
	});
	const active = visibleJobs.find((job) => job.id === selectedId) ?? (jobFilter === 'all' ? jobs.find((job) => job.id === selectedId) : undefined);
	const activeJobId = active?.id;
	const activeJobKind = active?.kind;
	const activeProgress = active ? jobProgress(active) : 0;
	const canRetryActive = Boolean(active && apiReadMode === 'real' &&
	  (active.state === 'partial' || active.state === 'failed' || active.state === 'cancelled') &&
	  (active.kind === 'scan' || active.state === 'cancelled' || active.failed > 0 || active.processed < active.total));

	useEffect(() => {
	  if (apiReadMode === 'mock' || activeJobKind !== 'batch_edit' || !activeJobId) {
		setBatchItems([]);
		setTrackNames(new Map());
		return;
	  }
	  let disposed = false;
	  const loadItems = () => void listBatchEditItems(activeJobId).then(async (items) => {
		if (disposed) return;
		setBatchItems(items);
		const ids = [...new Set(items.map((item) => item.trackId).filter(Boolean))];
		if (ids.length === 0) {
		  setTrackNames(new Map());
		  return;
		}
		const resolved = await resolveTracks({ids});
		if (!disposed) setTrackNames(new Map(resolved.tracks.map((track) => [track.id, track.fileName])));
	  }).catch(() => undefined);
	  loadItems();
	  const timer = window.setInterval(loadItems, 1000);
	  return () => {
		disposed = true;
		window.clearInterval(timer);
	  };
	}, [activeJobId, activeJobKind]);

  return (
    <div className="section-page jobs-page">
      <header className="section-hero">
        <div>
		  <div className="eyebrow">DURABLE QUEUE / {apiReadMode === 'real' ? 'SQLITE' : 'MOCK'}</div>
          <h1>任务中心</h1>
          <p>扫描、抓取与文件写入都有独立进度；服务重启后仍能恢复。</p>
        </div>
		<button className="secondary-button" onClick={() => void refresh()}><RefreshCw size={15} /> 刷新状态</button>
      </header>

      <div className="section-tabs">
        <button className={jobFilter === 'all' ? 'is-active' : undefined} onClick={() => setJobFilter('all')}>全部 <em>{jobs.length}</em></button>
		<button className={jobFilter === 'running' ? 'is-active' : undefined} onClick={() => setJobFilter('running')}>进行中 <em>{jobs.filter((job) => job.state === 'running' || job.state === 'waiting').length}</em></button>
		<button className={jobFilter === 'review' ? 'is-active' : undefined} onClick={() => setJobFilter('review')}>待审核 <em>{jobs.filter((job) => job.state === 'review').length}</em></button>
		<button className={jobFilter === 'failed' ? 'is-active' : undefined} onClick={() => setJobFilter('failed')}>失败 <em>{jobs.filter((job) => job.state === 'failed' || job.state === 'partial').length}</em></button>
      </div>

      <div className="jobs-layout">
		<section className="jobs-list">
		  {visibleJobs.length === 0 && <div className="empty-state"><span>∅</span><strong>{jobs.length === 0 ? '还没有任务' : '没有符合条件的任务'}</strong><p>{jobs.length === 0 ? '重新扫描或批量操作后会出现在这里。' : '切换其他状态查看任务。'}</p></div>}
          {visibleJobs.map((job) => {
            const KindIcon = kindIcon[job.kind];
            const meta = stateMeta[job.state];
            const StateIcon = meta.icon;
			const progress = jobProgress(job);
            return (
              <button
                key={job.id}
                className={cn('job-card', selectedId === job.id && 'is-active')}
                onClick={() => setSelectedId(job.id)}
              >
                <span className={cn('job-kind', `kind-${job.kind}`)}><KindIcon size={18} /></span>
                <span className="job-copy">
                  <span><strong>{job.title}</strong><small>{job.startedAt}</small></span>
                  <p>{job.detail}</p>
                  <span className="job-progress"><i style={{width: `${progress}%`}} /></span>
				  <span className="job-numbers">已处理 {job.processed} / {job.total} 项 · 处理进度 {progress}%</span>
                </span>
                <span className={cn('job-state', `state-${job.state}`)}>
                  <StateIcon size={13} className={job.state === 'running' ? 'spin' : undefined} />
                  {meta.label}
                </span>
                <ChevronRight size={17} />
              </button>
            );
          })}
        </section>

        {active && (
		  <aside className="job-detail">
            <div className="eyebrow">JOB / {active.id.toUpperCase()}</div>
            <h2>{active.title}</h2>
            <p>{active.detail}</p>
			<div className="job-detail-progress" aria-label={`处理进度 ${activeProgress}%`}>
			  <em className="job-detail-progress-label">处理进度</em>
			  <strong>{activeProgress}<small>%</small></strong>
			  <div><span style={{width: `${activeProgress}%`}} /></div>
            </div>
            <dl>
              <div><dt>任务类型</dt><dd>{active.kind === 'scan' ? '曲库扫描' : active.kind === 'match' ? '元数据抓取' : active.kind === 'write' ? '安全写入' : '批量编辑'}</dd></div>
              <div><dt>当前状态</dt><dd>{stateMeta[active.state].label}</dd></div>
              <div><dt>已处理</dt><dd>{active.processed} / {active.total}</dd></div>
              <div><dt>成功</dt><dd>{active.succeeded}</dd></div>
              <div><dt>失败</dt><dd className={active.failed ? 'danger-text' : undefined}>{active.failed}</dd></div>
              <div><dt>开始时间</dt><dd>{active.startedAt}</dd></div>
            </dl>
		  <div className="job-log">
			<span>最新事件</span>
			<code>{active.startedAt}　{active.detail}</code>
			{active.error && <code>{active.error}</code>}
            </div>
			{active.kind === 'batch_edit' && (
			  <section className="job-items">
				<div className="job-items-head"><span>逐文件结果</span><small>{batchItems.length} / {active.total} 已返回</small></div>
				{batchItems.length === 0 ? (
				  <div className="job-items-empty">任务开始后，这里会显示每个文件的写入状态和字段差异。</div>
				) : (
				  <div className="job-item-list">
					{batchItems.map((item) => (
					  <article className={cn('job-item', `is-${item.state}`)} key={item.id || item.trackId}>
						<div className="job-item-head">
						  <strong>{trackNames.get(item.trackId) ?? item.trackId}</strong>
						  <span>{batchItemStateText[item.state]}</span>
						</div>
						{item.error && <p className="job-item-error">{item.error}</p>}
						{item.diff.length > 0 ? (
						  <div className="job-item-diff">
							{item.diff.map((diff, index) => (
							  <p key={`${diff.field}-${index}`}><em>{batchFieldText[diff.field] ?? diff.field}</em><del>{formatBatchValue(diff.before)}</del><b>→</b><ins>{formatBatchValue(diff.after)}</ins></p>
							))}
						  </div>
						) : item.state === 'written' ? <small className="job-item-noop">没有字段变化</small> : null}
					  </article>
					))}
				  </div>
				)}
			  </section>
			)}
			{isMatchReviewable(active) && <button className="primary-button full-button" onClick={() => onOpenReview(active.id)}>
			  <Sparkles size={15} /> 打开审核页
			</button>}
			{active.state === 'review' && apiReadMode === 'real' && <button className="danger-quiet full-button" onClick={() => { setDiscardError(''); setDiscardJob(active); }}>
			  <X size={15} /> 丢弃审核任务
			</button>}
			{(active.state === 'running' || active.state === 'waiting') && apiReadMode === 'real' && (
			  <button className="danger-quiet full-button" onClick={() => {
				setActionError('');
				void cancelJob(active.id).then((next) => { if (next) updateJob(next); }).catch((error) => setActionError(error instanceof Error ? error.message : '取消任务失败'));
			  }}>
				<X size={15} /> 取消任务
			  </button>
			)}
			{canRetryActive && (
			  <button className="secondary-button full-button" onClick={() => {
				setActionError('');
				void retryJob(active.id).then((next) => { if (next) updateJob(next); }).catch((error) => setActionError(error instanceof Error ? error.message : '重试任务失败'));
			  }}>
				<RefreshCw size={15} /> {active.kind === 'scan' || active.state === 'cancelled' ? '重试任务' : '重试失败项'}
			  </button>
			)}
			{actionError && <p className="restore-error">{actionError}</p>}
          </aside>
        )}
      </div>
      <ConfirmDialog
        open={Boolean(discardJob)}
        title="丢弃待审核结果？"
        description="这只会删除本次抓取的候选和审核快照，不会修改任何音乐文件。丢弃后才能安全切换到其他曲库。"
        confirmLabel="确认丢弃"
        busy={discarding}
        error={discardError}
        onCancel={() => { if (!discarding) { setDiscardJob(undefined); setDiscardError(''); } }}
        onConfirm={() => void discardReviewJob()}
      />
    </div>
  );
}
