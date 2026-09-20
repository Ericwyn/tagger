import {useEffect, useMemo, useState} from 'react';
import {AlertTriangle, Check, LoaderCircle, MoveRight, X} from 'lucide-react';
import {cn} from '@/lib/utils';
import {previewOrganize} from '@/api';
import type {OrganizeMode, OrganizePreviewItem, Track} from '@/types';

interface OrganizePanelProps {
  open: boolean;
  tracks: Track[];
  saving: boolean;
  onClose: () => void;
  onApply: (mode: OrganizeMode) => Promise<void>;
}

const stateLabels: Record<OrganizePreviewItem['state'], string> = {
  ready: '可整理',
  noop: '无需移动',
  conflict: '目标冲突',
  invalid: '无法整理',
  moved: '已移动',
  failed: '失败',
};

const modeOptions: Array<{value: OrganizeMode; label: string; hint: string}> = [
  {value: 'artist_album', label: '第一歌手 / 专辑 / 原文件名', hint: '适合按专辑浏览的曲库'},
  {value: 'artist', label: '第一歌手 / 原文件名', hint: '更扁平，适合单曲较多的曲库'},
];

export function OrganizePanel({open, tracks, saving, onClose, onApply}: OrganizePanelProps) {
  const [items, setItems] = useState<OrganizePreviewItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [mode, setMode] = useState<OrganizeMode>('artist_album');

  const trackKey = useMemo(() => tracks.map((track) => `${track.id}:${track.revision}`).join('|'), [tracks]);
  useEffect(() => {
    if (!open) return;
    let active = true;
    setLoading(true);
    setError('');
    setItems([]);
    void previewOrganize(tracks.map((track) => ({trackId: track.id, baseRevision: track.revision})), mode)
      .then((next) => { if (active) setItems(next); })
      .catch((reason) => { if (active) setError(reason instanceof Error ? reason.message : '整理预览失败'); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [open, trackKey, mode]);

  if (!open) return null;
  const selectedMode = modeOptions.find((option) => option.value === mode) ?? modeOptions[0];
  const blocked = items.some((item) => item.state !== 'ready' && item.state !== 'noop');
  const readyCount = items.filter((item) => item.state === 'ready').length;
  const sidecarCount = items.filter((item) => item.sidecarExists).length;

  return (
    <div className="candidate-layer organize-layer">
      <button className="candidate-backdrop" aria-label="关闭文件整理" onClick={onClose} />
      <aside className="organize-drawer" role="dialog" aria-modal="true" aria-label="整理文件位置">
        <header className="batch-edit-head">
          <div>
            <div className="eyebrow">ORGANIZE / SAFE MOVE</div>
            <h2>整理文件位置</h2>
            <p>按所选层级移动文件，不会修改音频文件名和标签。</p>
          </div>
          <button className="icon-button" title="关闭文件整理" onClick={onClose}><X size={19} /></button>
        </header>

        <div className="organize-body">
          <div className="batch-edit-summary">
            <span><strong>{tracks.length}</strong> 首选中</span>
            <span><strong>{readyCount}</strong> 首待移动</span>
            <span><strong>{sidecarCount}</strong> 个歌词 sidecar</span>
          </div>
          <div className="organize-mode-picker">
            <div>
              <span className="organize-mode-label">整理层级</span>
              <small>{selectedMode.hint}</small>
            </div>
            <select aria-label="文件整理模式" value={mode} disabled={saving} onChange={(event) => setMode(event.target.value as OrganizeMode)}>
              {modeOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
            </select>
          </div>
          <div className="organize-policy-note">
            <MoveRight size={16} />
            <span>目标冲突不会覆盖已有文件；同名 .lrc 会和音频一起移动。</span>
          </div>
          {loading && <div className="organize-loading"><LoaderCircle className="spin" size={17} /> 正在计算安全路径…</div>}
          {error && <div className="organize-error"><AlertTriangle size={15} /> {error}</div>}
          {!loading && !error && (
            <div className="organize-preview-list">
              {items.map((item) => (
                <article className={cn('organize-preview-row', item.state !== 'ready' && item.state !== 'noop' && 'is-blocked')} key={item.trackId}>
                  <div className="organize-preview-paths">
                    <code>{item.source}</code>
                    <MoveRight size={14} />
                    <code>{item.target}</code>
                  </div>
                  <div className="organize-preview-meta">
                    <span>{item.primaryArtist} · {item.album}</span>
                    <strong className={`is-${item.state}`}>
                      {item.state === 'ready' || item.state === 'noop' ? <Check size={13} /> : <AlertTriangle size={13} />}
                      {stateLabels[item.state]}
                    </strong>
                  </div>
                  {(item.warnings?.length ?? 0) > 0 && <small>{item.warnings!.join('；')}</small>}
                  {item.sidecarExists && <em>包含歌词 sidecar</em>}
                </article>
              ))}
              {items.length === 0 && <div className="organize-empty">没有可整理的曲目。</div>}
            </div>
          )}
        </div>

        <footer className="batch-edit-footer organize-footer">
          <span>{blocked ? '请解决目标冲突后重新预览' : '确认后将创建后台整理任务'}</span>
          <button className="secondary-button" onClick={onClose} disabled={saving}>取消</button>
          <button className="primary-button" disabled={loading || Boolean(error) || blocked || items.length === 0 || saving} onClick={() => void onApply(mode)}>
            {saving ? <LoaderCircle className="spin" size={15} /> : <Check size={15} />}
            {saving ? '创建中…' : `确认整理 ${readyCount} 首`}
          </button>
        </footer>
      </aside>
    </div>
  );
}
