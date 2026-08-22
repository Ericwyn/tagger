import {useEffect, useState} from 'react';
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
import {apiReadMode, cancelJob, listJobs, retryJob, subscribeJobEvents} from '@/api';
import type {Job} from '@/types';

interface JobsPageProps {
  onOpenReview: () => void;
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

export function JobsPage({onOpenReview}: JobsPageProps) {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [selectedId, setSelectedId] = useState<string>();
	const [actionError, setActionError] = useState('');

	const refresh = () => listJobs().then((next) => {
	  setJobs(next);
	  setSelectedId((current) => next.some((job) => job.id === current) ? current : next[0]?.id);
	  });

	const updateJob = (next: Job) => {
	  setJobs((current) => current.map((job) => job.id === next.id ? next : job));
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

  const active = jobs.find((job) => job.id === selectedId);

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
        <button className="is-active">全部 <em>{jobs.length}</em></button>
		<button>进行中 <em>{jobs.filter((job) => job.state === 'running' || job.state === 'waiting').length}</em></button>
		<button>待审核 <em>{jobs.filter((job) => job.state === 'review').length}</em></button>
		<button>失败 <em>{jobs.filter((job) => job.state === 'failed' || job.state === 'partial').length}</em></button>
      </div>

      <div className="jobs-layout">
		<section className="jobs-list">
		  {jobs.length === 0 && <div className="empty-state"><span>∅</span><strong>还没有任务</strong><p>重新扫描或批量操作后会出现在这里。</p></div>}
          {jobs.map((job) => {
            const KindIcon = kindIcon[job.kind];
            const meta = stateMeta[job.state];
            const StateIcon = meta.icon;
            const progress = job.total ? Math.round((job.processed / job.total) * 100) : 0;
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
                  <span className="job-numbers">{job.processed} / {job.total} 项 · {progress}%</span>
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
            <div className="job-detail-progress">
			  <strong>{active.total ? Math.round((active.processed / active.total) * 100) : 0}<small>%</small></strong>
			  <div><span style={{width: `${active.total ? (active.processed / active.total) * 100 : 0}%`}} /></div>
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
            {active.state === 'review' && (
              <button className="primary-button full-button" onClick={onOpenReview}>
                <Sparkles size={15} /> 打开审核页
              </button>
            )}
			{(active.state === 'running' || active.state === 'waiting') && apiReadMode === 'real' && (
			  <button className="danger-quiet full-button" onClick={() => {
				setActionError('');
				void cancelJob(active.id).then((next) => { if (next) updateJob(next); }).catch((error) => setActionError(error instanceof Error ? error.message : '取消任务失败'));
			  }}>
				<X size={15} /> 取消任务
			  </button>
			)}
			{(active.state === 'partial' || active.state === 'failed' || active.state === 'cancelled') && apiReadMode === 'real' && (
			  <button className="secondary-button full-button" onClick={() => {
				setActionError('');
				void retryJob(active.id).then((next) => { if (next) updateJob(next); }).catch((error) => setActionError(error instanceof Error ? error.message : '重试任务失败'));
			  }}>
				<RefreshCw size={15} /> 重试失败项
			  </button>
			)}
			{actionError && <p className="restore-error">{actionError}</p>}
          </aside>
        )}
      </div>
    </div>
  );
}
