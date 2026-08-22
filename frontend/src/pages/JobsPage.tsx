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
} from 'lucide-react';
import {cn} from '@/lib/utils';
import {listJobs} from '@/api';
import type {Job} from '@/types';

interface JobsPageProps {
  onOpenReview: () => void;
}

const stateMeta: Record<Job['state'], {label: string; icon: typeof Check}> = {
  running: {label: '执行中', icon: LoaderCircle},
  review: {label: '等待审核', icon: CircleAlert},
  waiting: {label: '等待重试', icon: Clock3},
  succeeded: {label: '已完成', icon: Check},
  partial: {label: '部分完成', icon: CircleAlert},
};

const kindIcon = {
  scan: ScanSearch,
  match: Sparkles,
  write: FilePenLine,
};

export function JobsPage({onOpenReview}: JobsPageProps) {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [selectedId, setSelectedId] = useState<string>();

  useEffect(() => {
    listJobs().then((next) => {
      setJobs(next);
      setSelectedId(next[0]?.id);
    });
  }, []);

  const active = jobs.find((job) => job.id === selectedId);

  return (
    <div className="section-page jobs-page">
      <header className="section-hero">
        <div>
          <div className="eyebrow">DURABLE QUEUE / MOCK</div>
          <h1>任务中心</h1>
          <p>扫描、抓取与文件写入都有独立进度；服务重启后仍能恢复。</p>
        </div>
        <button className="secondary-button"><RefreshCw size={15} /> 刷新状态</button>
      </header>

      <div className="section-tabs">
        <button className="is-active">全部 <em>{jobs.length}</em></button>
        <button>进行中 <em>0</em></button>
        <button>待审核 <em>1</em></button>
        <button>失败 <em>1</em></button>
      </div>

      <div className="jobs-layout">
        <section className="jobs-list">
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
              <strong>{Math.round((active.processed / active.total) * 100)}<small>%</small></strong>
              <div><span style={{width: `${(active.processed / active.total) * 100}%`}} /></div>
            </div>
            <dl>
              <div><dt>任务类型</dt><dd>{active.kind === 'scan' ? '曲库扫描' : active.kind === 'match' ? '元数据抓取' : '安全写入'}</dd></div>
              <div><dt>当前状态</dt><dd>{stateMeta[active.state].label}</dd></div>
              <div><dt>已处理</dt><dd>{active.processed} / {active.total}</dd></div>
              <div><dt>成功</dt><dd>{active.succeeded}</dd></div>
              <div><dt>失败</dt><dd className={active.failed ? 'danger-text' : undefined}>{active.failed}</dd></div>
              <div><dt>开始时间</dt><dd>{active.startedAt}</dd></div>
            </dl>
            <div className="job-log">
              <span>最新事件</span>
              <code>09:51:06　候选聚合完成</code>
              <code>09:51:05　LRCLIB 返回 8 条歌词</code>
              <code>09:51:04　MusicBrainz 限流等待 1s</code>
            </div>
            {active.state === 'review' && (
              <button className="primary-button full-button" onClick={onOpenReview}>
                <Sparkles size={15} /> 打开审核页
              </button>
            )}
            {active.state === 'partial' && (
              <button className="secondary-button full-button"><RefreshCw size={15} /> 重试失败项</button>
            )}
          </aside>
        )}
      </div>
    </div>
  );
}
