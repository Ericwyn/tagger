import {useEffect, useMemo, useState} from 'react';
import {AlertTriangle, Check, FileDown, FileUp, LoaderCircle, X} from 'lucide-react';
import {cn} from '@/lib/utils';
import type {Track, TrackPatch} from '@/types';

export const snapshotFields = [
  'title', 'artists', 'album', 'albumArtists', 'trackNumber', 'trackTotal', 'discNumber', 'discTotal', 'year',
  'genres', 'lyrics', 'comment', 'composers', 'conductor', 'lyricists', 'copyright', 'bpm', 'isrc',
  'musicbrainzTrackId', 'musicbrainzReleaseId', 'musicbrainzArtistIds', 'acoustidId', 'acoustidFingerprint',
] as const;

type SnapshotField = typeof snapshotFields[number];
type SnapshotRecord = Record<string, unknown>;

export interface SnapshotUpdate {
  track: Track;
  patch: TrackPatch;
  changedFields: SnapshotField[];
  revisionConflict: boolean;
}

interface TagSnapshotPanelProps {
  open: boolean;
  tracks: Track[];
  saving: boolean;
  onClose: () => void;
  onApply: (updates: SnapshotUpdate[]) => Promise<void>;
}

const listFields = new Set<SnapshotField>([
  'artists', 'albumArtists', 'genres', 'composers', 'lyricists', 'musicbrainzArtistIds',
]);
const numberFields = new Set<SnapshotField>([
  'trackNumber', 'trackTotal', 'discNumber', 'discTotal', 'year', 'bpm',
]);

export function trackToSnapshot(track: Track): SnapshotRecord {
  const snapshot: SnapshotRecord = {
    id: track.id,
    fileName: track.fileName,
    relativePath: track.relativePath,
    revision: track.revision,
  };
  snapshotFields.forEach((field) => { snapshot[field] = track[field]; });
  return snapshot;
}

export function serializeSnapshotJSON(tracks: Track[]): string {
  return JSON.stringify({version: 1, exportedAt: new Date().toISOString(), tracks: tracks.map(trackToSnapshot)}, null, 2);
}

function csvValue(value: unknown): string {
  if (Array.isArray(value)) return value.join('|');
  if (value === undefined || value === null) return '';
  return String(value);
}

function csvEscape(value: string): string {
  return /[",\n]/.test(value) ? `"${value.replaceAll('"', '""')}"` : value;
}

export function serializeSnapshotCSV(tracks: Track[]): string {
  const columns = ['id', 'fileName', 'relativePath', 'revision', ...snapshotFields];
  const rows = [columns.join(',')];
  tracks.forEach((track) => {
    const snapshot = trackToSnapshot(track);
    rows.push(columns.map((column) => csvEscape(csvValue(snapshot[column]))).join(','));
  });
  return `${rows.join('\n')}\n`;
}

function parseCSV(content: string): SnapshotRecord[] {
  const rows: string[][] = [];
  let row: string[] = [];
  let value = '';
  let quoted = false;
  const source = content.replace(/^\uFEFF/, '');
  for (let index = 0; index < source.length; index += 1) {
    const character = source[index];
    if (character === '"' && quoted && source[index + 1] === '"') {
      value += '"';
      index += 1;
    } else if (character === '"') {
      quoted = !quoted;
    } else if (character === ',' && !quoted) {
      row.push(value);
      value = '';
    } else if (character === '\n' && !quoted) {
      row.push(value.replace(/\r$/, ''));
      if (row.some((item) => item !== '')) rows.push(row);
      row = [];
      value = '';
    } else {
      value += character;
    }
  }
  if (value !== '' || row.length > 0) {
    row.push(value);
    if (row.some((item) => item !== '')) rows.push(row);
  }
  if (rows.length < 2) return [];
  const headers = rows[0];
  return rows.slice(1).map((values) => Object.fromEntries(headers.map((header, index) => [header, values[index] ?? ''])));
}

export function parseSnapshot(content: string, fileName = 'snapshot.json'): SnapshotRecord[] {
  if (fileName.toLocaleLowerCase().endsWith('.csv')) return parseCSV(content);
  const payload = JSON.parse(content) as unknown;
  if (Array.isArray(payload)) return payload.filter((item): item is SnapshotRecord => Boolean(item && typeof item === 'object'));
  if (payload && typeof payload === 'object' && Array.isArray((payload as {tracks?: unknown}).tracks)) {
    return (payload as {tracks: unknown[]}).tracks.filter((item): item is SnapshotRecord => Boolean(item && typeof item === 'object'));
  }
  throw new Error('快照文件必须是曲目数组或包含 tracks 数组的 JSON');
}

function parseSnapshotValue(field: SnapshotField, value: unknown): unknown {
  if (listFields.has(field)) {
    if (Array.isArray(value)) return value.filter((item): item is string => typeof item === 'string');
    return typeof value === 'string' ? value.split('|').map((item) => item.trim()).filter(Boolean) : [];
  }
  if (numberFields.has(field)) {
    if (value === '' || value === null || value === undefined) return undefined;
    const parsed = typeof value === 'number' ? value : Number.parseInt(String(value), 10);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
  }
  return typeof value === 'string' ? value : value === null || value === undefined ? '' : String(value);
}

function sameValue(left: unknown, right: unknown): boolean {
  if (Array.isArray(left) || Array.isArray(right)) {
    return Array.isArray(left) && Array.isArray(right) && left.length === right.length && left.every((value, index) => value === right[index]);
  }
  return left === right;
}

export function trackToPatch(track: Track): TrackPatch {
  return {
    title: track.title,
    artists: [...track.artists],
    album: track.album,
    albumArtists: [...track.albumArtists],
    trackNumber: track.trackNumber,
    trackTotal: track.trackTotal,
    discNumber: track.discNumber,
    discTotal: track.discTotal,
    year: track.year,
    genres: [...track.genres],
    lyrics: track.lyrics,
    comment: track.comment,
    composers: [...track.composers],
    conductor: track.conductor,
    lyricists: [...track.lyricists],
    copyright: track.copyright,
    bpm: track.bpm,
    isrc: track.isrc,
    musicbrainzTrackId: track.musicbrainzTrackId,
    musicbrainzReleaseId: track.musicbrainzReleaseId,
    musicbrainzArtistIds: [...track.musicbrainzArtistIds],
    acoustidId: track.acoustidId,
    acoustidFingerprint: track.acoustidFingerprint,
  };
}

export function buildSnapshotUpdates(records: SnapshotRecord[], tracks: Track[]): SnapshotUpdate[] {
  const matched = new Set<string>();
  return records.flatMap((record) => {
    const id = typeof record.id === 'string' ? record.id : '';
    const relativePath = typeof record.relativePath === 'string' ? record.relativePath : '';
    const track = tracks.find((item) => (id && item.id === id) || (relativePath && item.relativePath === relativePath));
    if (!track || matched.has(track.id)) return [];
    matched.add(track.id);
    const patch = trackToPatch(track);
    const changedFields = snapshotFields.filter((field) => {
      if (!Object.prototype.hasOwnProperty.call(record, field)) return false;
      const next = parseSnapshotValue(field, record[field]);
      if (field in patch && sameValue(patch[field], next)) return false;
      (patch as unknown as Record<string, unknown>)[field] = next;
      return true;
    });
    return changedFields.length > 0 ? [{
      track,
      patch,
      changedFields,
      revisionConflict: typeof record.revision === 'string' && Boolean(record.revision) && record.revision !== track.revision,
    }] : [];
  });
}

function download(name: string, content: string, type: string): void {
  const url = URL.createObjectURL(new Blob([content], {type}));
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = name;
  anchor.click();
  URL.revokeObjectURL(url);
}

function readFileText(file: File): Promise<string> {
  if (typeof file.text === 'function') return file.text();
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result ?? ''));
    reader.onerror = () => reject(reader.error ?? new Error('无法读取文件'));
    reader.readAsText(file);
  });
}

function fieldLabel(field: SnapshotField): string {
  const labels: Partial<Record<SnapshotField, string>> = {
    title: '标题', artists: '艺术家', album: '专辑', albumArtists: '专辑艺术家', trackNumber: '音轨号', trackTotal: '总音轨',
    discNumber: '光盘号', discTotal: '总光盘', year: '年份', genres: '风格', lyrics: '歌词', comment: '注释', composers: '作曲家',
    conductor: '指挥', lyricists: '作词家', copyright: '版权', bpm: 'BPM', isrc: 'ISRC', musicbrainzTrackId: 'MB Track ID',
    musicbrainzReleaseId: 'MB Release ID', musicbrainzArtistIds: 'MB Artist ID', acoustidId: 'AcoustID', acoustidFingerprint: 'AcoustID 指纹',
  };
  return labels[field] ?? field;
}

function displaySnapshotValue(value: unknown): string {
  if (Array.isArray(value)) return value.length > 0 ? value.join(' / ') : '空';
  if (value === undefined || value === null || value === '') return '空';
  return String(value);
}

export function TagSnapshotPanel({open, tracks, saving, onClose, onApply}: TagSnapshotPanelProps) {
  const [fileName, setFileName] = useState('');
  const [records, setRecords] = useState<SnapshotRecord[]>([]);
  const [error, setError] = useState('');

  useEffect(() => {
    if (open) {
      setFileName('');
      setRecords([]);
      setError('');
    }
  }, [open]);

  const updates = useMemo(() => buildSnapshotUpdates(records, tracks), [records, tracks]);
  const missingCount = records.length - updates.length;
  const conflicts = updates.filter((update) => update.revisionConflict).length;

  const handleFile = async (file?: File) => {
    if (!file) return;
    setFileName(file.name);
    setError('');
    try {
      setRecords(parseSnapshot(await readFileText(file), file.name));
    } catch (parseError) {
      setRecords([]);
      setError(parseError instanceof Error ? parseError.message : '无法读取快照文件');
    }
  };

  if (!open) return null;

  return (
    <div className="candidate-layer snapshot-layer">
      <button className="candidate-backdrop" aria-label="关闭标签快照" onClick={onClose} />
      <aside className="snapshot-drawer">
        <header className="snapshot-head">
          <div>
            <div className="eyebrow">TAG SNAPSHOT / JSON · CSV</div>
            <h2>标签快照</h2>
            <p>导出内嵌元数据，或导入后先预览差异，再逐文件安全写入。</p>
          </div>
          <button className="icon-button" title="关闭标签快照" onClick={onClose}><X size={19} /></button>
        </header>

        <div className="snapshot-body">
          <section className="snapshot-actions">
            <div>
              <strong>当前范围：{tracks.length} 首曲目</strong>
              <small>仅包含内嵌标签字段，不包含技术属性和远程封面 URL。</small>
            </div>
            <div className="snapshot-export-buttons">
              <button className="secondary-button" disabled={tracks.length === 0} onClick={() => download('tagger-snapshot.json', serializeSnapshotJSON(tracks), 'application/json')}><FileDown size={15} /> 导出 JSON</button>
              <button className="secondary-button" disabled={tracks.length === 0} onClick={() => download('tagger-snapshot.csv', serializeSnapshotCSV(tracks), 'text/csv;charset=utf-8')}><FileDown size={15} /> 导出 CSV</button>
            </div>
          </section>

          <label className="snapshot-import-drop">
            <FileUp size={20} />
            <strong>{fileName || '选择要导入的 JSON / CSV 快照'}</strong>
            <small>按照 id 或 relativePath 匹配当前曲库；缺失曲目不会写入。</small>
            <input type="file" accept=".json,.csv,application/json,text/csv" onChange={(event) => void handleFile(event.target.files?.[0])} />
          </label>

          {error && <div className="snapshot-error"><AlertTriangle size={15} /> {error}</div>}
          {records.length > 0 && (
            <section className="snapshot-preview">
              <div className="snapshot-summary">
                <span><strong>{updates.length}</strong> 首待写入</span>
                <span className={cn(missingCount > 0 && 'is-warning')}><strong>{missingCount}</strong> 首未匹配</span>
                <span className={cn(conflicts > 0 && 'is-warning')}><strong>{conflicts}</strong> 首版本变化</span>
              </div>
              <div className="snapshot-preview-list">
                {updates.slice(0, 30).map((update) => (
                  <div className="snapshot-preview-row" key={update.track.id}>
                    <div><strong>{update.track.title || update.track.fileName}</strong><small>{update.track.fileName}</small></div>
                    <span>{update.changedFields.map((field) => `${fieldLabel(field)} → ${displaySnapshotValue(update.patch[field])}`).join(' · ')}</span>
                    {update.revisionConflict && <span title="导出后文件已发生变化"><AlertTriangle size={14} /></span>}
                  </div>
                ))}
              </div>
              {updates.length > 30 && <small className="snapshot-more">仅显示前 30 首，全部匹配项都会写入。</small>}
            </section>
          )}
        </div>

        <footer className="snapshot-footer">
          <button className="secondary-button" onClick={onClose}>取消</button>
          <button className="primary-button" disabled={saving || updates.length === 0} onClick={() => void onApply(updates)}>
            {saving ? <LoaderCircle className="spin" size={15} /> : <Check size={15} />}
            {saving ? '正在安全写入…' : `确认导入 ${updates.length} 首`}
          </button>
        </footer>
      </aside>
    </div>
  );
}
