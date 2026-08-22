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

  useEffect(() => {
    listRevisions().then((next) => {
      setRevisions(next);
      setActiveId(next[0]?.id);
    });
  }, []);

  const active = revisions.find((revision) => revision.id === activeId);

  return (
    <div className="section-page history-page">
      <header className="section-hero">
        <div>
          <div className="eyebrow">REVISION LEDGER</div>
          <h1>修改历史</h1>
          <p>每次手工编辑、数据源补全和外部变化都会留下可审计的标签快照。</p>
        </div>
        <div className="history-security"><ShieldCheck size={17} /> 历史完整 · 4 个修订</div>
      </header>

      <div className="history-toolbar">
        <label className="search-box">
          <Search size={16} />
          <input placeholder="搜索曲目、文件或来源…" />
        </label>
        <button className="toolbar-button">全部来源</button>
        <button className="toolbar-button">最近 30 天</button>
      </div>

      <div className="history-layout">
        <section className="revision-list">
          <div className="revision-date"><span>今天</span><i /></div>
          {revisions.map((revision, index) => (
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
              {index < revisions.length - 1 && <i className="timeline-line" />}
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
              {active.fields.map((field, index) => (
                <div key={field}>
                  <strong>{field}</strong>
                  <del>{index === 0 ? '空' : '原始值'}</del>
                  <ArrowRight size={13} />
                  <ins>{index === 0 ? active.trackTitle : '已补全值'}</ins>
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
              onClick={() => onNotice(`已为「${active.trackTitle}」生成恢复预览`)}
            >
              <RotateCcw size={15} /> 生成恢复预览
            </button>
          </aside>
        )}
      </div>
    </div>
  );
}
