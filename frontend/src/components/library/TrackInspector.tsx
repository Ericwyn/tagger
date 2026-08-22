import {useEffect, useMemo, useRef, useState} from 'react';
import {
  Check,
  ChevronRight,
  CircleEllipsis,
  Copy,
  FileAudio2,
  History,
  ImagePlus,
  LoaderCircle,
  Music,
  Pause,
  Play,
  RotateCcw,
  Save,
  Search,
  Sparkles,
  Trash2,
  Upload,
  X,
} from 'lucide-react';
import {CoverArt} from '@/components/CoverArt';
import {artworkURL, audioURL} from '@/api';
import {cn, formatBytes, formatDuration} from '@/lib/utils';
import type {InspectorTab, Track, TrackPatch} from '@/types';

interface TrackInspectorProps {
  track: Track | null;
  saving: boolean;
  mobileOpen: boolean;
  onCloseMobile: () => void;
  onSearch: () => void;
  onSave: (patch: TrackPatch) => Promise<void>;
  onArtworkChange: (file: File | null) => Promise<void>;
}

const tabs: Array<{id: InspectorTab; label: string}> = [
  {id: 'tags', label: '标签'},
  {id: 'artwork', label: '封面'},
  {id: 'lyrics', label: '歌词'},
  {id: 'technical', label: '技术'},
  {id: 'history', label: '历史'},
];

function toPatch(track: Track): TrackPatch {
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
  };
}

function patchEqual(left: TrackPatch, right: TrackPatch): boolean {
  return JSON.stringify(left) === JSON.stringify(right);
}

function parseList(value: string): string[] {
  return value.split(/[,，/]/).map((item) => item.trim()).filter(Boolean);
}

export function TrackInspector({
  track,
  saving,
  mobileOpen,
  onCloseMobile,
  onSearch,
  onSave,
  onArtworkChange,
}: TrackInspectorProps) {
  const [tab, setTab] = useState<InspectorTab>('tags');
  const [draft, setDraft] = useState<TrackPatch | null>(track ? toPatch(track) : null);
  const [showPreview, setShowPreview] = useState(false);
  const [playing, setPlaying] = useState(false);
  const [deleteArtworkArmed, setDeleteArtworkArmed] = useState(false);
  const audioRef = useRef<HTMLAudioElement>(null);
  const artworkInput = useRef<HTMLInputElement>(null);
  const playbackURL = track ? audioURL(track) : undefined;

  useEffect(() => {
    setDraft(track ? toPatch(track) : null);
    setShowPreview(false);
    setPlaying(false);
	setDeleteArtworkArmed(false);
	if (audioRef.current) {
	  audioRef.current.pause();
	  audioRef.current.currentTime = 0;
	}
  }, [track]);

  const original = useMemo(() => track ? toPatch(track) : null, [track]);
  const dirty = Boolean(draft && original && !patchEqual(draft, original));

  const changedFields = useMemo<Array<[keyof TrackPatch, string]>>(() => {
    if (!draft || !original) return [];
    const names: Array<[keyof TrackPatch, string]> = [
      ['title', '标题'],
      ['artists', '艺术家'],
      ['album', '专辑'],
      ['albumArtists', '专辑艺术家'],
      ['trackNumber', '音轨号'],
      ['trackTotal', '总音轨'],
      ['discNumber', '光盘号'],
      ['discTotal', '总光盘'],
      ['year', '年份'],
      ['genres', '风格'],
      ['lyrics', '歌词'],
    ];
    return names.filter(([key]) => JSON.stringify(draft[key]) !== JSON.stringify(original[key]));
  }, [draft, original]);

  if (!track || !draft) {
    return (
      <aside className={cn('track-inspector inspector-empty', mobileOpen && 'is-mobile-open')}>
        <div className="inspector-empty-disc">
          <span />
          <Music size={28} />
        </div>
        <strong>选择一首曲目</strong>
        <p>查看标签、技术参数和修改历史。</p>
      </aside>
    );
  }

  const set = <K extends keyof TrackPatch>(key: K, value: TrackPatch[K]) => {
    setDraft((current) => current ? {...current, [key]: value} : current);
  };

  const inputNumber = (value: string): number | undefined => {
    if (!value.trim()) return undefined;
    const next = Number(value);
    return Number.isFinite(next) ? next : undefined;
  };

  const togglePlayback = () => {
    if (!playbackURL || !audioRef.current) {
      setPlaying((value) => !value);
      return;
    }
    if (audioRef.current.paused) {
      void audioRef.current.play().catch(() => setPlaying(false));
    } else {
      audioRef.current.pause();
    }
  };

  return (
    <aside className={cn('track-inspector', mobileOpen && 'is-mobile-open')}>
      <div className="inspector-mobile-head">
        <span>曲目详情</span>
        <button title="关闭详情" onClick={onCloseMobile}><X size={18} /></button>
      </div>

      <div className="inspector-hero">
        <CoverArt
          title={track.title}
          artist={track.artists[0]}
          tone={track.coverTone}
          missing={track.artworkCount === 0}
          size="lg"
		  imageUrl={artworkURL(track)}
        />
        <div className="hero-copy">
          <div className="eyebrow">NOW INSPECTING · {track.format.toUpperCase()}</div>
          <h2>{track.title || '未命名曲目'}</h2>
          <p>{track.artists.join(' / ')} <span>·</span> {track.album || '未知专辑'}</p>
          <div className="mini-player">
            <button title={playing ? '暂停试听' : '试听'} onClick={togglePlayback}>
              {playing ? <Pause size={14} fill="currentColor" /> : <Play size={14} fill="currentColor" />}
            </button>
            <div className={cn('waveform', playing && 'is-playing')} aria-label="音频波形">
              {Array.from({length: 24}, (_, index) => (
                <i key={index} style={{height: `${8 + ((index * 13) % 18)}px`}} />
              ))}
            </div>
            <span>{playing ? '0:24' : formatDuration(track.durationSeconds)}</span>
          </div>
          {playbackURL && (
            <audio
              ref={audioRef}
              src={playbackURL}
              preload="metadata"
              onPlay={() => setPlaying(true)}
              onPause={() => setPlaying(false)}
              onEnded={() => setPlaying(false)}
              onError={() => setPlaying(false)}
              aria-label="试听音频"
            />
          )}
        </div>
        <button className="hero-more" title="更多曲目操作"><CircleEllipsis size={19} /></button>
      </div>

      <div className="inspector-tabs" role="tablist">
        {tabs.map((item) => (
          <button
            key={item.id}
            role="tab"
            aria-selected={tab === item.id}
            className={cn(tab === item.id && 'is-active')}
            onClick={() => setTab(item.id)}
          >
            {item.label}
            {item.id === 'lyrics' && !track.lyrics && <i />}
          </button>
        ))}
      </div>

      <div className="inspector-scroll">
        {tab === 'tags' && (
          <div className="inspector-pane tag-form">
            <div className="form-section-head">
              <span>基本信息</span>
              <button onClick={onSearch}><Sparkles size={14} /> 从数据源补全</button>
            </div>
            <label className="field-row">
              <span>标题</span>
              <input value={draft.title} onChange={(event) => set('title', event.target.value)} />
            </label>
            <label className="field-row">
              <span>艺术家</span>
              <input
                value={draft.artists.join(' / ')}
                onChange={(event) => set('artists', parseList(event.target.value))}
              />
              <small>使用 / 分隔多位艺术家</small>
            </label>
            <label className="field-row">
              <span>专辑</span>
              <input value={draft.album} onChange={(event) => set('album', event.target.value)} />
            </label>
            <label className="field-row">
              <span>专辑艺术家</span>
              <input
                value={draft.albumArtists.join(' / ')}
                onChange={(event) => set('albumArtists', parseList(event.target.value))}
              />
            </label>

            <div className="form-section-head"><span>发行信息</span></div>
            <div className="field-grid two">
              <label className="field-row">
                <span>音轨号</span>
                <div className="split-number">
                  <input
                    type="number"
                    min="0"
                    value={draft.trackNumber ?? ''}
                    onChange={(event) => set('trackNumber', inputNumber(event.target.value))}
                  />
                  <i>/</i>
                  <input
                    type="number"
                    min="0"
                    value={draft.trackTotal ?? ''}
                    onChange={(event) => set('trackTotal', inputNumber(event.target.value))}
                  />
                </div>
              </label>
              <label className="field-row">
                <span>光盘号</span>
                <div className="split-number">
                  <input
                    type="number"
                    min="0"
                    value={draft.discNumber ?? ''}
                    onChange={(event) => set('discNumber', inputNumber(event.target.value))}
                  />
                  <i>/</i>
                  <input
                    type="number"
                    min="0"
                    value={draft.discTotal ?? ''}
                    onChange={(event) => set('discTotal', inputNumber(event.target.value))}
                  />
                </div>
              </label>
            </div>
            <div className="field-grid two">
              <label className="field-row">
                <span>发行年份</span>
                <input
                  type="number"
                  value={draft.year ?? ''}
                  onChange={(event) => set('year', inputNumber(event.target.value))}
                />
              </label>
              <label className="field-row">
                <span>风格</span>
                <input
                  value={draft.genres.join(', ')}
                  onChange={(event) => set('genres', parseList(event.target.value))}
                />
              </label>
            </div>

            <button className="raw-tag-link">
              <FileAudio2 size={15} />
              查看 14 个原始标签
              <ChevronRight size={14} />
            </button>
          </div>
        )}

        {tab === 'artwork' && (
          <div className="inspector-pane artwork-pane">
            <div className="artwork-stage">
              <CoverArt
                title={track.title}
                artist={track.artists[0]}
                tone={track.coverTone}
                missing={track.artworkCount === 0}
                size="hero"
				imageUrl={artworkURL(track)}
              />
              <span className="artwork-index">01 / {Math.max(track.artworkCount, 1)}</span>
            </div>
            <dl className="meta-list">
              <div><dt>类型</dt><dd>Front Cover</dd></div>
			  <div><dt>规格</dt><dd>{track.artworkCount ? '从文件实时读取' : '—'}</dd></div>
			  <div><dt>数量</dt><dd>{track.artworkCount} 张嵌入图片</dd></div>
			  <div><dt>描述</dt><dd>{track.artworkCount ? 'Front Cover' : '尚未嵌入封面'}</dd></div>
            </dl>
            <div className="button-pair">
			  <input
				ref={artworkInput}
				className="visually-hidden"
				type="file"
				accept="image/jpeg,image/png,image/webp"
				onChange={async (event) => {
				  const file = event.target.files?.[0];
				  if (file) await onArtworkChange(file);
				  event.target.value = '';
				}}
			  />
			  <button className="secondary-button" disabled={saving} onClick={() => artworkInput.current?.click()}>
				<Upload size={15} /> {saving ? '写入中…' : '上传封面'}
			  </button>
			  <button className="secondary-button" onClick={onSearch}><Search size={15} /> 在线查找</button>
            </div>
			{track.artworkCount > 0 && (
			  <button
				className="danger-link"
				disabled={saving}
				onClick={async () => {
				  if (!deleteArtworkArmed) {
					setDeleteArtworkArmed(true);
					return;
				  }
				  await onArtworkChange(null);
				  setDeleteArtworkArmed(false);
				}}
			  >
				<Trash2 size={14} /> {deleteArtworkArmed ? '再次点击确认删除' : '删除当前封面'}
			  </button>
			)}
          </div>
        )}

        {tab === 'lyrics' && (
          <div className="inspector-pane lyrics-pane">
            <div className="lyrics-head">
              <div>
                <span>同步歌词 / LRC</span>
                <small>{draft.lyrics ? '检测到时间轴' : '当前没有歌词'}</small>
              </div>
              <button><Sparkles size={14} /> 查找歌词</button>
            </div>
            <textarea
              value={draft.lyrics}
              onChange={(event) => set('lyrics', event.target.value)}
              placeholder="[00:00.00] 在这里输入歌词，或从 LRCLIB / 网易云获取…"
              spellCheck={false}
            />
            <div className="lyrics-options">
              <label><input type="checkbox" defaultChecked /> 写入音频标签</label>
              <label><input type="checkbox" defaultChecked /> 同时保存 .lrc</label>
            </div>
            <p className="format-note">保存时会写入同目录临时副本，重读验证成功后再原子替换原文件。</p>
          </div>
        )}

        {tab === 'technical' && (
          <div className="inspector-pane technical-pane">
            <div className="technical-hero">
              <strong>{track.properties.sampleRateHz / 1000}<small>kHz</small></strong>
              <span>{track.properties.bitDepth ? `${track.properties.bitDepth}-BIT LOSSLESS` : 'LOSSY AUDIO'}</span>
            </div>
            <dl className="technical-grid">
              <div><dt>容器</dt><dd>{track.properties.container}</dd></div>
              <div><dt>编码</dt><dd>{track.properties.codec}</dd></div>
              <div><dt>码率</dt><dd>{track.properties.bitrateKbps} kbps</dd></div>
              <div><dt>声道</dt><dd>{track.properties.channels === 2 ? 'Stereo / 2ch' : `${track.properties.channels}ch`}</dd></div>
              <div><dt>时长</dt><dd>{formatDuration(track.durationSeconds)}</dd></div>
              <div><dt>文件大小</dt><dd>{formatBytes(track.sizeBytes)}</dd></div>
            </dl>
            <div className="path-block">
              <span>相对路径</span>
              <code>{track.relativePath}</code>
              <button title="复制路径"><Copy size={14} /></button>
            </div>
            <div className="revision-block">
              <span>当前修订</span>
              <code>{track.revision}</code>
              <small>最后修改 {track.modifiedAt}</small>
            </div>
          </div>
        )}

        {tab === 'history' && (
          <div className="inspector-pane history-pane">
            <div className="history-line is-current">
              <i><Check size={14} /></i>
              <div><strong>当前版本</strong><span>{track.modifiedAt} · 标签快照</span></div>
            </div>
            <div className="history-line">
              <i><History size={14} /></i>
              <div><strong>扫描检测</strong><span>8 月 18 日 · 读取 14 个标签</span></div>
              <button>比较</button>
            </div>
            <div className="history-line">
              <i><RotateCcw size={14} /></i>
              <div><strong>初次导入</strong><span>8 月 17 日 · 原始文件</span></div>
              <button>恢复</button>
            </div>
          </div>
        )}
      </div>

      <div className="inspector-actions">
        <div>
          <span className={cn('dirty-indicator', dirty && 'is-dirty')} />
          {dirty ? `${changedFields.length} 项未保存` : '所有修改已保存'}
        </div>
        {dirty && (
          <button className="ghost-button" onClick={() => setDraft(toPatch(track))}>
            放弃
          </button>
        )}
        <button className="primary-button" disabled={!dirty || saving} onClick={() => setShowPreview(true)}>
          {saving ? <LoaderCircle size={15} className="spin" /> : <Save size={15} />}
          保存修改
        </button>
      </div>

      {showPreview && (
        <div className="diff-overlay">
          <div className="diff-card">
            <div className="diff-head">
              <div>
                <span className="eyebrow">WRITE PREVIEW</span>
                <h3>确认写入 {changedFields.length} 项修改</h3>
              </div>
              <button title="关闭预览" onClick={() => setShowPreview(false)}><X size={18} /></button>
            </div>
            <div className="diff-list">
              {changedFields.map(([key, label]) => (
                <div key={String(key)}>
                  <span>{label}</span>
                  <del>{String(Array.isArray(original?.[key]) ? (original?.[key] as string[]).join(' / ') : original?.[key] ?? '空')}</del>
                  <ChevronRight size={13} />
                  <ins>{String(Array.isArray(draft[key]) ? (draft[key] as string[]).join(' / ') : draft[key] ?? '空')}</ins>
                </div>
              ))}
            </div>
            <p>真实后端会先写入同目录临时副本，重读验证后再原子替换原文件。</p>
            <div className="diff-actions">
              <button className="secondary-button" onClick={() => setShowPreview(false)}>继续编辑</button>
              <button
                className="primary-button"
                disabled={saving}
                onClick={async () => {
                  await onSave(draft);
                  setShowPreview(false);
                }}
              >
                {saving ? <LoaderCircle size={15} className="spin" /> : <Check size={15} />}
                确认写入
              </button>
            </div>
          </div>
        </div>
      )}
    </aside>
  );
}
