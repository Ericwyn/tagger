import {useEffect, useMemo, useState} from 'react';
import {Check, ChevronDown, LoaderCircle, Wand2, X} from 'lucide-react';
import {cn} from '@/lib/utils';
import type {BatchEditOperation, Track, TrackPatch} from '@/types';

type EditableField = 'album' | 'albumArtists' | 'year' | 'genres' | 'comment' | 'composers' | 'conductor' | 'lyricists' | 'copyright' | 'bpm' | 'isrc';
type OperationMode = '' | 'set' | 'append' | 'delete';

export type BatchOperation = BatchEditOperation;

interface OperationState {
  mode: OperationMode;
  value: string;
}

interface BatchEditPanelProps {
  open: boolean;
  tracks: Track[];
  saving: boolean;
  onClose: () => void;
  onApply: (operations: BatchOperation[], sequenceTracks: boolean) => Promise<void>;
}

const fields: Array<{id: EditableField; label: string; kind: 'text' | 'list' | 'number'; allowAppend?: boolean}> = [
  {id: 'album', label: '专辑', kind: 'text', allowAppend: false},
  {id: 'albumArtists', label: '专辑艺术家', kind: 'list'},
  {id: 'year', label: '年份', kind: 'number'},
  {id: 'genres', label: '风格', kind: 'list'},
  {id: 'comment', label: '注释', kind: 'text', allowAppend: false},
  {id: 'composers', label: '作曲家', kind: 'list'},
  {id: 'conductor', label: '指挥', kind: 'text', allowAppend: false},
  {id: 'lyricists', label: '作词家', kind: 'list'},
  {id: 'copyright', label: '版权', kind: 'text', allowAppend: false},
  {id: 'bpm', label: 'BPM', kind: 'number'},
  {id: 'isrc', label: 'ISRC', kind: 'text', allowAppend: false},
];

const emptyOperations = (): Record<EditableField, OperationState> => ({
  album: {mode: '', value: ''},
  albumArtists: {mode: '', value: ''},
  year: {mode: '', value: ''},
  genres: {mode: '', value: ''},
  comment: {mode: '', value: ''},
  composers: {mode: '', value: ''},
  conductor: {mode: '', value: ''},
  lyricists: {mode: '', value: ''},
  copyright: {mode: '', value: ''},
  bpm: {mode: '', value: ''},
  isrc: {mode: '', value: ''},
});

export function BatchEditPanel({open, tracks, saving, onClose, onApply}: BatchEditPanelProps) {
  const [operationState, setOperationState] = useState<Record<EditableField, OperationState>>(emptyOperations);
  const [sequenceTracks, setSequenceTracks] = useState(false);

  useEffect(() => {
    if (open) {
      setOperationState(emptyOperations());
      setSequenceTracks(false);
    }
  }, [open]);

  const operations = useMemo(
    () => fields.flatMap(({id}) => {
      const operation = operationState[id];
      return operation.mode ? [{field: id, mode: operation.mode, value: operation.value}] : [];
    }),
    [operationState],
  );

  const previews = useMemo(() => tracks.slice(0, 20).map((track, index) => {
    const patch = buildBatchPatch(track, operations, sequenceTracks ? {index, total: tracks.length} : undefined);
    const changes = fields.flatMap(({id, label}) => {
      const before = displayValue(track, id);
      const after = displayValue(patch, id);
      return before === after ? [] : [{label, before, after}];
    });
    if (sequenceTracks && track.trackNumber !== index + 1) {
      changes.push({label: '音轨', before: track.trackNumber ? `${track.trackNumber} / ${track.trackTotal || '—'}` : '空', after: `${index + 1} / ${tracks.length}`});
    }
    return {track, changes};
  }), [operations, sequenceTracks, tracks]);

  if (!open) return null;

  const writableCount = tracks.filter((track) => track.writable).length;
  const readOnlyCount = tracks.length - writableCount;
  const canApply = !saving && (operations.length > 0 || sequenceTracks) && writableCount > 0;

  return (
    <div className="candidate-layer batch-edit-layer">
      <button className="candidate-backdrop" aria-label="关闭批量编辑" onClick={onClose} />
      <aside className="batch-edit-drawer">
        <header className="batch-edit-head">
          <div>
            <div className="eyebrow">BATCH EDIT / SAFE PATCH</div>
            <h2>批量编辑标签</h2>
            <p>为选中的 {tracks.length} 首曲目创建逐文件 revision 安全写入。</p>
          </div>
          <button className="icon-button" title="关闭批量编辑" onClick={onClose}><X size={19} /></button>
        </header>

        <div className="batch-edit-body">
          <div className="batch-edit-summary">
            <span><strong>{tracks.length}</strong> 首影响文件</span>
            <span className={cn(readOnlyCount > 0 && 'is-warning')}><strong>{readOnlyCount}</strong> 首不可写</span>
            <span><strong>{previews.reduce((count, preview) => count + preview.changes.length, 0)}</strong> 条预览差异</span>
          </div>

          <section className="batch-edit-section">
            <div className="batch-edit-section-head"><span>公共字段操作</span><small>未选择的字段保持原值</small></div>
            <div className="batch-edit-fields">
              {fields.map((field) => {
                const state = operationState[field.id];
                const mixed = mixedValue(tracks, field.id);
                return (
                  <div className="batch-edit-field" key={field.id}>
                    <strong>{field.label}</strong>
                    <label className="batch-edit-select">
                      <select aria-label={`${field.label}操作`} value={state.mode} onChange={(event) => setOperationState((current) => ({...current, [field.id]: {...current[field.id], mode: event.target.value as OperationMode}}))}>
                        <option value="">保持不变</option>
                        <option value="set">设置为</option>
                        {field.kind !== 'number' && field.allowAppend !== false && <option value="append">追加</option>}
                        <option value="delete">删除</option>
                      </select>
                      <ChevronDown size={13} />
                    </label>
                    <input
                      type={field.kind === 'number' ? 'number' : 'text'}
                      aria-label={`${field.label}值`}
                      value={state.value}
                      disabled={!state.mode || state.mode === 'delete'}
                      placeholder={mixed}
                      onChange={(event) => setOperationState((current) => ({...current, [field.id]: {...current[field.id], value: event.target.value}}))}
                    />
                  </div>
                );
              })}
            </div>
          </section>

          <label className={cn('batch-edit-sequence', sequenceTracks && 'is-selected')}>
            <button type="button" className={cn('square-check', sequenceTracks && 'is-checked')} onClick={() => setSequenceTracks((current) => !current)}>
              {sequenceTracks && <Check size={12} strokeWidth={3} />}
            </button>
            <span><strong>按当前选中顺序生成音轨号</strong><small>从 1 开始，并把总音轨数设为 {tracks.length}</small></span>
          </label>

          <section className="batch-edit-section batch-edit-preview">
            <div className="batch-edit-section-head"><span>差异预览</span><small>最多显示前 20 首</small></div>
            {previews.length === 0 || previews.every((preview) => preview.changes.length === 0) ? (
              <div className="batch-edit-empty">选择一个操作后，这里会显示写入前后的差异。</div>
            ) : (
              <div className="batch-edit-preview-list">
                {previews.filter((preview) => preview.changes.length > 0).map((preview) => (
                  <div className="batch-edit-preview-row" key={preview.track.id}>
                    <span><strong>{preview.track.title || preview.track.fileName}</strong><small>{preview.track.fileName}</small></span>
                    <div>{preview.changes.map((change) => <p key={change.label}><em>{change.label}</em><del>{change.before}</del><b>→</b><ins>{change.after}</ins></p>)}</div>
                  </div>
                ))}
              </div>
            )}
          </section>
        </div>

        <footer className="batch-edit-footer">
          <button className="secondary-button" onClick={onClose}>取消</button>
          <button className="primary-button" disabled={!canApply} onClick={() => void onApply(operations, sequenceTracks)}>
            {saving ? <LoaderCircle className="spin" size={15} /> : <Wand2 size={15} />}
            {saving ? '正在逐文件写入…' : `应用到 ${writableCount} 首`}
          </button>
        </footer>
      </aside>
    </div>
  );
}

export function buildBatchPatch(track: Track, operations: BatchOperation[], sequence?: {index: number; total: number}): TrackPatch {
  const patch: TrackPatch = {
    title: track.title,
    artists: track.artists,
    album: track.album,
    albumArtists: track.albumArtists,
    trackNumber: track.trackNumber,
    trackTotal: track.trackTotal,
    discNumber: track.discNumber,
    discTotal: track.discTotal,
    year: track.year,
    genres: track.genres,
    lyrics: track.lyrics,
    comment: track.comment,
    composers: track.composers,
    conductor: track.conductor,
    lyricists: track.lyricists,
    copyright: track.copyright,
    bpm: track.bpm,
    isrc: track.isrc,
    musicbrainzTrackId: track.musicbrainzTrackId,
    musicbrainzReleaseId: track.musicbrainzReleaseId,
    musicbrainzArtistIds: track.musicbrainzArtistIds,
    acoustidId: track.acoustidId,
    acoustidFingerprint: track.acoustidFingerprint,
  };
  operations.forEach((operation) => {
    if (operation.field === 'album') patch.album = applyText(operation.mode, track.album, operation.value);
    if (operation.field === 'albumArtists') patch.albumArtists = applyList(operation.mode, track.albumArtists, operation.value);
    if (operation.field === 'genres') patch.genres = applyList(operation.mode, track.genres, operation.value);
    if (operation.field === 'year') patch.year = applyYear(operation.mode, track.year, operation.value);
    if (operation.field === 'comment') patch.comment = applyText(operation.mode, track.comment, operation.value);
    if (operation.field === 'composers') patch.composers = applyList(operation.mode, track.composers, operation.value);
    if (operation.field === 'conductor') patch.conductor = applyText(operation.mode, track.conductor, operation.value);
    if (operation.field === 'lyricists') patch.lyricists = applyList(operation.mode, track.lyricists, operation.value);
    if (operation.field === 'copyright') patch.copyright = applyText(operation.mode, track.copyright, operation.value);
    if (operation.field === 'bpm') patch.bpm = applyYear(operation.mode, track.bpm, operation.value);
    if (operation.field === 'isrc') patch.isrc = applyText(operation.mode, track.isrc, operation.value);
  });
  if (sequence) {
    patch.trackNumber = sequence.index + 1;
    patch.trackTotal = sequence.total;
  }
  return patch;
}

function applyText(mode: Exclude<OperationMode, ''>, current: string, value: string): string {
  if (mode === 'delete') return '';
  if (mode === 'append') return current ? `${current} ${value.trim()}`.trim() : value.trim();
  return value.trim();
}

function applyList(mode: Exclude<OperationMode, ''>, current: string[], value: string): string[] {
  if (mode === 'delete') return [];
  const next = splitList(value);
  if (mode === 'append') return [...current, ...next.filter((item) => !current.includes(item))];
  return next;
}

function applyYear(mode: Exclude<OperationMode, ''>, current: number | undefined, value: string): number | undefined {
  if (mode === 'delete') return undefined;
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : current;
}

function splitList(value: string): string[] {
  return value.split(/[\n,，]/).map((item) => item.trim()).filter(Boolean);
}

function mixedValue(tracks: Track[], field: EditableField): string {
  const values = tracks.map((track) => displayValue(track, field));
  const unique = new Set(values);
  if (unique.size === 1) return values[0] || '当前为空';
  return `混合值 · ${unique.size} 种不同内容`;
}

function displayValue(track: Track | TrackPatch, field: EditableField): string {
  if (field === 'album') return track.album || '';
  if (field === 'albumArtists') return track.albumArtists.join(' / ');
  if (field === 'genres') return track.genres.join(' / ');
  if (field === 'comment') return track.comment || '';
  if (field === 'composers') return track.composers.join(' / ');
  if (field === 'conductor') return track.conductor || '';
  if (field === 'lyricists') return track.lyricists.join(' / ');
  if (field === 'copyright') return track.copyright || '';
  if (field === 'bpm') return track.bpm ? String(track.bpm) : '';
  if (field === 'isrc') return track.isrc || '';
  return track.year ? String(track.year) : '';
}
