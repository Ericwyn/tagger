import {useEffect, useMemo, useState} from 'react';
import {
  Check,
  ChevronLeft,
  ChevronRight,
  CircleAlert,
  Image,
  LoaderCircle,
  Music2,
  Search,
  Sparkles,
  X,
} from 'lucide-react';
import {CoverArt} from '@/components/CoverArt';
import {candidateArtworkURL, listQueryHistory} from '@/api';
import {cn, formatDuration} from '@/lib/utils';
import type {CandidateSearchQuery, MatchCandidate, Track, TrackPatch} from '@/types';

interface CandidateDrawerProps {
  open: boolean;
  track: Track | null;
  candidates: MatchCandidate[];
  loading: boolean;
  focus?: 'metadata' | 'lyrics';
  showGeneratedCovers?: boolean;
  onSearchQuery?: (query: CandidateSearchQuery) => Promise<void>;
  onClose: () => void;
  onApply: (patch: TrackPatch, candidate: MatchCandidate, options: {artwork: boolean; artworkMaxSize?: number}) => Promise<void>;
}

function suspiciousAlbumArtist(candidate: MatchCandidate): boolean {
  if (!candidate.album.value || candidate.albumArtists.value.length !== 1) return false;
  const album = candidate.album.value.trim().toLocaleLowerCase();
  const albumArtist = candidate.albumArtists.value[0].trim().toLocaleLowerCase();
  if (!album || album !== albumArtist) return false;
  return !candidate.artists.value.some((artist) => artist.trim().toLocaleLowerCase() === album);
}

type CandidateFieldDescriptor = {
  id: string;
  label: string;
  available: (candidate: MatchCandidate) => boolean;
  currentText: (track: Track) => string;
  candidateText: (candidate: MatchCandidate) => string;
  apply: (patch: TrackPatch, candidate: MatchCandidate) => void;
  warning?: (candidate: MatchCandidate) => string | undefined;
};

const fieldOptions = [
  {
    id: 'title', label: '标题',
    available: (candidate) => Boolean(candidate.title.value),
    currentText: (track) => track.title || '空',
    candidateText: (candidate) => candidate.title.value || '来源未提供',
    apply: (patch, candidate) => { patch.title = candidate.title.value; },
  },
  {
    id: 'artists', label: '艺术家',
    available: (candidate) => candidate.artists.value.length > 0,
    currentText: (track) => track.artists.join(' / ') || '空',
    candidateText: (candidate) => candidate.artists.value.join(' / ') || '来源未提供',
    apply: (patch, candidate) => { patch.artists = [...candidate.artists.value]; },
  },
  {
    id: 'album', label: '专辑',
    available: (candidate) => Boolean(candidate.album.value),
    currentText: (track) => track.album || '空',
    candidateText: (candidate) => candidate.album.value || '来源未提供',
    apply: (patch, candidate) => { patch.album = candidate.album.value; },
  },
  {
    id: 'albumArtists', label: '专辑艺术家',
    available: (candidate) => candidate.albumArtists.value.length > 0,
    currentText: (track) => track.albumArtists.join(' / ') || '空',
    candidateText: (candidate) => candidate.albumArtists.value.join(' / ') || '来源未提供',
    apply: (patch, candidate) => { patch.albumArtists = [...candidate.albumArtists.value]; },
    warning: (candidate) => suspiciousAlbumArtist(candidate)
      ? '候选专辑艺术家与专辑名相同、但与曲目艺术家不同，已默认取消勾选'
      : undefined,
  },
  {
    id: 'track', label: '音轨 / 光盘',
    available: (candidate) => candidate.trackNumber.value > 0 || candidate.trackTotal.value > 0 || candidate.discNumber.value > 0 || candidate.discTotal.value > 0,
    currentText: (track) => {
      const trackValue = track.trackNumber ? `${track.trackNumber} / ${track.trackTotal || '—'}` : '空';
      const discValue = track.discNumber ? `${track.discNumber} / ${track.discTotal || '—'}` : '空';
      return `音轨 ${trackValue} · 光盘 ${discValue}`;
    },
    candidateText: (candidate) => {
      const trackValue = candidate.trackNumber.value > 0 ? `${candidate.trackNumber.value} / ${candidate.trackTotal.value || '—'}` : '来源未提供';
      const discValue = candidate.discNumber.value > 0 ? `${candidate.discNumber.value} / ${candidate.discTotal.value || '—'}` : '来源未提供';
      return `音轨 ${trackValue} · 光盘 ${discValue}`;
    },
    apply: (patch, candidate) => {
      if (candidate.trackNumber.value > 0) patch.trackNumber = candidate.trackNumber.value;
      if (candidate.trackTotal.value > 0) patch.trackTotal = candidate.trackTotal.value;
      if (candidate.discNumber.value > 0) patch.discNumber = candidate.discNumber.value;
      if (candidate.discTotal.value > 0) patch.discTotal = candidate.discTotal.value;
    },
  },
  {
    id: 'year', label: '年份',
    available: (candidate) => candidate.year.value > 0,
    currentText: (track) => String(track.year || '空'),
    candidateText: (candidate) => String(candidate.year.value || '来源未提供'),
    apply: (patch, candidate) => { patch.year = candidate.year.value; },
  },
  {
    id: 'genres', label: '风格',
    available: (candidate) => candidate.genres.value.length > 0,
    currentText: (track) => track.genres.join(', ') || '空',
    candidateText: (candidate) => candidate.genres.value.join(', ') || '来源未提供',
    apply: (patch, candidate) => { patch.genres = [...candidate.genres.value]; },
  },
  {
    id: 'comment', label: '注释',
    available: (candidate) => Boolean(candidate.comment?.value),
    currentText: (track) => track.comment || '空',
    candidateText: (candidate) => candidate.comment?.value || '来源未提供',
    apply: (patch, candidate) => { patch.comment = candidate.comment?.value ?? ''; },
  },
  {
    id: 'composers', label: '作曲家',
    available: (candidate) => Boolean(candidate.composers?.value.length),
    currentText: (track) => track.composers.join(' / ') || '空',
    candidateText: (candidate) => candidate.composers?.value.join(' / ') || '来源未提供',
    apply: (patch, candidate) => { patch.composers = [...(candidate.composers?.value ?? [])]; },
  },
  {
    id: 'conductor', label: '指挥',
    available: (candidate) => Boolean(candidate.conductor?.value),
    currentText: (track) => track.conductor || '空',
    candidateText: (candidate) => candidate.conductor?.value || '来源未提供',
    apply: (patch, candidate) => { patch.conductor = candidate.conductor?.value ?? ''; },
  },
  {
    id: 'lyricists', label: '作词家',
    available: (candidate) => Boolean(candidate.lyricists?.value.length),
    currentText: (track) => track.lyricists.join(' / ') || '空',
    candidateText: (candidate) => candidate.lyricists?.value.join(' / ') || '来源未提供',
    apply: (patch, candidate) => { patch.lyricists = [...(candidate.lyricists?.value ?? [])]; },
  },
  {
    id: 'copyright', label: '版权',
    available: (candidate) => Boolean(candidate.copyright?.value),
    currentText: (track) => track.copyright || '空',
    candidateText: (candidate) => candidate.copyright?.value || '来源未提供',
    apply: (patch, candidate) => { patch.copyright = candidate.copyright?.value ?? ''; },
  },
  {
    id: 'bpm', label: 'BPM',
    available: (candidate) => Boolean(candidate.bpm?.value),
    currentText: (track) => String(track.bpm || '空'),
    candidateText: (candidate) => String(candidate.bpm?.value || '来源未提供'),
    apply: (patch, candidate) => { patch.bpm = candidate.bpm?.value; },
  },
  {
    id: 'isrc', label: 'ISRC',
    available: (candidate) => Boolean(candidate.isrc?.value),
    currentText: (track) => track.isrc || '空',
    candidateText: (candidate) => candidate.isrc?.value || '来源未提供',
    apply: (patch, candidate) => { patch.isrc = candidate.isrc?.value ?? ''; },
  },
  {
    id: 'musicbrainzTrackId', label: 'MB Track ID',
    available: (candidate) => Boolean(candidate.musicbrainzTrackId?.value),
    currentText: (track) => track.musicbrainzTrackId || '空',
    candidateText: (candidate) => candidate.musicbrainzTrackId?.value || '来源未提供',
    apply: (patch, candidate) => { patch.musicbrainzTrackId = candidate.musicbrainzTrackId?.value ?? ''; },
  },
  {
    id: 'musicbrainzReleaseId', label: 'MB Release ID',
    available: (candidate) => Boolean(candidate.musicbrainzReleaseId?.value),
    currentText: (track) => track.musicbrainzReleaseId || '空',
    candidateText: (candidate) => candidate.musicbrainzReleaseId?.value || '来源未提供',
    apply: (patch, candidate) => { patch.musicbrainzReleaseId = candidate.musicbrainzReleaseId?.value ?? ''; },
  },
  {
    id: 'musicbrainzArtistIds', label: 'MB Artist ID',
    available: (candidate) => Boolean(candidate.musicbrainzArtistIds?.value.length),
    currentText: (track) => track.musicbrainzArtistIds.join(' / ') || '空',
    candidateText: (candidate) => candidate.musicbrainzArtistIds?.value.join(' / ') || '来源未提供',
    apply: (patch, candidate) => { patch.musicbrainzArtistIds = [...(candidate.musicbrainzArtistIds?.value ?? [])]; },
  },
  {
    id: 'acoustidId', label: 'AcoustID',
    available: (candidate) => Boolean(candidate.acoustidId?.value),
    currentText: (track) => track.acoustidId || '空',
    candidateText: (candidate) => candidate.acoustidId?.value || '来源未提供',
    apply: (patch, candidate) => { patch.acoustidId = candidate.acoustidId?.value ?? ''; },
  },
  {
    id: 'acoustidFingerprint', label: 'AcoustID 指纹',
    available: (candidate) => Boolean(candidate.acoustidFingerprint?.value),
    currentText: (track) => track.acoustidFingerprint || '空',
    candidateText: (candidate) => candidate.acoustidFingerprint?.value || '来源未提供',
    apply: (patch, candidate) => { patch.acoustidFingerprint = candidate.acoustidFingerprint?.value ?? ''; },
  },
] as const satisfies readonly CandidateFieldDescriptor[];

type FieldID = typeof fieldOptions[number]['id'];

function fieldWarning(field: CandidateFieldDescriptor, candidate: MatchCandidate): string | undefined {
  return field.warning?.(candidate);
}

function defaultCandidateFields(candidate: MatchCandidate): Set<FieldID> {
  return new Set(fieldOptions
    .filter((field) => field.available(candidate))
    .filter((field) => !fieldWarning(field, candidate))
    .map((field) => field.id));
}

const queryHistoryLimit = 8;

function queryHistoryKey(trackID: string): string {
  return `tagger-query-history:${trackID}`;
}

function sameQuery(left: CandidateSearchQuery, right: CandidateSearchQuery): boolean {
  return left.title === right.title && left.album === right.album && left.durationSeconds === right.durationSeconds
    && left.artists.length === right.artists.length && left.artists.every((artist, index) => artist === right.artists[index]);
}

function readQueryHistory(trackID: string): CandidateSearchQuery[] {
  try {
    const payload = JSON.parse(localStorage.getItem(queryHistoryKey(trackID)) || '[]') as unknown;
    if (!Array.isArray(payload)) return [];
    return payload.filter((entry): entry is CandidateSearchQuery => {
      if (!entry || typeof entry !== 'object') return false;
      const query = entry as Partial<CandidateSearchQuery>;
      return typeof query.title === 'string' && typeof query.album === 'string'
        && typeof query.durationSeconds === 'number' && Array.isArray(query.artists)
        && query.artists.every((artist) => typeof artist === 'string');
    }).slice(0, queryHistoryLimit);
  } catch {
    return [];
  }
}

function writeQueryHistory(trackID: string, queries: CandidateSearchQuery[]): void {
  try {
    localStorage.setItem(queryHistoryKey(trackID), JSON.stringify(queries.slice(0, queryHistoryLimit)));
  } catch {
    // Private browsing or a disabled storage quota should not block searching.
  }
}

function mergeQueryHistory(...lists: CandidateSearchQuery[][]): CandidateSearchQuery[] {
  const result: CandidateSearchQuery[] = [];
  lists.flat().forEach((query) => {
    if (!query || !Array.isArray(query.artists)) return;
    if (!result.some((entry) => sameQuery(entry, query))) result.push(query);
  });
  return result.slice(0, queryHistoryLimit);
}

export function CandidateDrawer({
  open,
  track,
  candidates,
  loading,
  focus = 'metadata',
  showGeneratedCovers = false,
  onSearchQuery,
  onClose,
  onApply,
}: CandidateDrawerProps) {
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [fields, setFields] = useState<Set<FieldID>>(new Set());
  const [applying, setApplying] = useState(false);
  const [includeLyrics, setIncludeLyrics] = useState(false);
  const [lyricsDraft, setLyricsDraft] = useState('');
  const [selectedArtworkInfo, setSelectedArtworkInfo] = useState<{width: number; height: number}>();
  const [includeArtwork, setIncludeArtwork] = useState(false);
  const [artworkMaxSize, setArtworkMaxSize] = useState(0);
  const [queryEditing, setQueryEditing] = useState(false);
  const [queryDraft, setQueryDraft] = useState<CandidateSearchQuery>({title: '', artists: [], album: '', durationSeconds: 0});
  const [queryArtistsDraft, setQueryArtistsDraft] = useState('');
  const [queryHistory, setQueryHistory] = useState<CandidateSearchQuery[]>([]);
  const [historySelection, setHistorySelection] = useState('');

  useEffect(() => {
    setSelectedId(candidates[0]?.id ?? null);
    const first = candidates[0];
    setIncludeLyrics(focus === 'lyrics' && Boolean(first?.hasLyrics && first.lyrics?.value));
    setLyricsDraft(first?.lyrics?.value ?? '');
    setSelectedArtworkInfo(undefined);
    setIncludeArtwork(false);
    setArtworkMaxSize(0);
  }, [candidates, focus]);

  useEffect(() => {
    if (!track) return;
    setQueryDraft({title: track.title, artists: [...track.artists], album: track.album, durationSeconds: track.durationSeconds});
    setQueryArtistsDraft(track.artists.join(' / '));
    const localHistory = readQueryHistory(track.id);
    setQueryHistory(localHistory);
    setHistorySelection('');
    setQueryEditing(false);
	let active = true;
	void listQueryHistory(track.id).then((remoteHistory) => {
	  if (!active || remoteHistory.length === 0) return;
	  const merged = mergeQueryHistory(remoteHistory.map((entry) => entry.query), localHistory);
	  setQueryHistory(merged);
	  writeQueryHistory(track.id, merged);
	}).catch(() => undefined);
	return () => { active = false; };
  }, [track?.id]);

  useEffect(() => {
    if (!open) setSelectedId(null);
  }, [open]);

  const runQuery = async (nextQuery: CandidateSearchQuery) => {
    if (!onSearchQuery) return;
    setQueryDraft(nextQuery);
    setQueryArtistsDraft(nextQuery.artists.join(' / '));
    await onSearchQuery(nextQuery);
    const nextHistory = [nextQuery, ...queryHistory.filter((entry) => !sameQuery(entry, nextQuery))];
    setQueryHistory(nextHistory);
    if (track) writeQueryHistory(track.id, nextHistory);
    setQueryEditing(false);
  };

  const submitQuery = async () => {
    const artists = queryArtistsDraft.split(/[,，/]/).map((item) => item.trim()).filter(Boolean);
    await runQuery({...queryDraft, artists});
  };

  const selectHistoryQuery = async (value: string) => {
    setHistorySelection('');
    const selectedHistory = queryHistory[Number(value)];
    if (selectedHistory) await runQuery(selectedHistory);
  };

  const selected = useMemo(
    () => candidates.find((candidate) => candidate.id === selectedId) ?? candidates[0],
    [candidates, selectedId],
  );

  const availableFieldIDs = useMemo(
    () => selected ? fieldOptions.filter((field) => field.available(selected)).map((field) => field.id) : [],
    [selected],
  );
  const allAvailableFieldsSelected = availableFieldIDs.length > 0 && availableFieldIDs.every((field) => fields.has(field));

  useEffect(() => {
    if (!selected) return;
    setFields(defaultCandidateFields(selected));
    setIncludeLyrics(focus === 'lyrics' && Boolean(selected.hasLyrics && selected.lyrics?.value));
    setLyricsDraft(selected.lyrics?.value ?? '');
    setSelectedArtworkInfo(undefined);
    setIncludeArtwork(false);
    setArtworkMaxSize(0);
  }, [focus, selected]);

  if (!open || !track) return null;

  const buildPatch = (): TrackPatch => {
    const patch: TrackPatch = {
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
      lyrics: includeLyrics ? lyricsDraft : track.lyrics,
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
    if (selected) {
      fieldOptions.forEach((field) => {
        if (fields.has(field.id) && field.available(selected)) field.apply(patch, selected);
      });
    }
    return patch;
  };

  const toggleField = (id: FieldID) => {
    setFields((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  return (
    <div className="candidate-layer">
      <button className="candidate-backdrop" aria-label="关闭搜索结果" onClick={onClose} />
      <aside className="candidate-drawer">
        <div className="candidate-head">
          <div>
            <div className="eyebrow">METADATA MATCHER</div>
            <h2>为「{track.title}」查找资料</h2>
            <p>{track.artists.join(' / ')} · {formatDuration(track.durationSeconds)} · {track.format.toUpperCase()}</p>
          </div>
          <button className="icon-button" title="关闭候选结果" onClick={onClose}><X size={19} /></button>
        </div>

        <div className="query-strip">
          <Search size={15} />
          {!queryEditing ? (
            <>
              <span>{queryDraft.title}　{queryDraft.artists.join(' ')}</span>
              <button onClick={() => setQueryEditing(true)}>修改查询</button>
              {queryHistory.length > 0 && (
                <select aria-label="查询历史" value={historySelection} onChange={(event) => void selectHistoryQuery(event.target.value)}>
                  <option value="">查询历史</option>
                  {queryHistory.map((history, index) => (
                    <option key={`${history.title}-${index}`} value={index}>
                      {history.title} · {history.artists.join(' / ') || '未填写艺术家'}
                    </option>
                  ))}
                </select>
              )}
            </>
          ) : (
            <div className="query-editor">
              <input aria-label="查询标题" value={queryDraft.title} onChange={(event) => setQueryDraft((current) => ({...current, title: event.target.value}))} />
              <input aria-label="查询艺术家" value={queryArtistsDraft} onChange={(event) => setQueryArtistsDraft(event.target.value)} />
              <input aria-label="查询专辑" value={queryDraft.album} onChange={(event) => setQueryDraft((current) => ({...current, album: event.target.value}))} />
              <button disabled={loading} onClick={() => void submitQuery()}>重新查询</button>
            </div>
          )}
        </div>

        {loading ? (
          <div className="candidate-loading">
            <div className="radar-loader">
              <i />
              <i />
              <Sparkles size={24} />
            </div>
            <strong>正在查询已启用数据源</strong>
            <p>MusicBrainz · LRCLIB · Apple；网易云 / 酷狗可在设置中启用</p>
          </div>
        ) : (
          <div className="candidate-layout">
            <section className="candidate-list-pane">
              <div className="pane-head">
                <span>找到 {candidates.length} 个候选</span>
                <small>按匹配度排序</small>
              </div>
              <div className="candidate-list">
                {candidates.map((candidate) => (
                  <button
                    key={candidate.id}
                    className={cn('candidate-item', selected?.id === candidate.id && 'is-active')}
                    onClick={() => setSelectedId(candidate.id)}
                  >
                    <CoverArt
                      title={candidate.title.value}
                      artist={candidate.artists.value[0]}
                      tone={candidate.coverTone}
                      missing={!showGeneratedCovers && !candidateArtworkURL(candidate)}
                      imageUrl={candidateArtworkURL(candidate)}
                      blankOnImageError={!showGeneratedCovers}
                      size="sm"
                    />
                    <span className="candidate-copy">
                      <strong>{candidate.title.value}</strong>
                      <span>{candidate.artists.value.join(' / ')}</span>
                      <small>{candidate.album.value || '专辑未知'} · {candidate.year.value || '年份未知'}</small>
                      <em>{candidate.providerName}</em>
                    </span>
                    <span className={cn('score-ring', candidate.score < 0.8 && 'is-low')}>
                      {Math.round(candidate.score * 100)}
                      <small>%</small>
                    </span>
                    <ChevronRight size={15} />
                  </button>
                ))}
              </div>
            </section>

            {selected && (
              <section className="candidate-detail-pane">
                <div className="candidate-summary">
                  <div className="candidate-cover-stack">
                    <CoverArt
                      title={selected.title.value}
                      artist={selected.artists.value[0]}
                      tone={selected.coverTone}
                      missing={!showGeneratedCovers && !candidateArtworkURL(selected)}
                      imageUrl={candidateArtworkURL(selected)}
                      blankOnImageError={!showGeneratedCovers}
                      size="md"
                      onImageInfo={setSelectedArtworkInfo}
                    />
                    <small className="cover-dimension">
                      {selectedArtworkInfo
                        ? `候选封面 · ${selectedArtworkInfo.width}×${selectedArtworkInfo.height}`
                        : selected.hasArtwork ? '候选封面 · 加载后显示尺寸' : '候选未提供封面'}
                    </small>
                  </div>
                  <div>
                    <span className={cn('confidence-badge', selected.score < 0.8 && 'is-warning')}>
                      {selected.score < 0.8 ? <CircleAlert size={13} /> : <Check size={13} />}
                      {selected.scoreLabel} · {Math.round(selected.score * 100)}%
                    </span>
                    <h3>{selected.title.value}</h3>
                    <p>{selected.artists.value.join(' / ')}</p>
                    <small>{selected.providerName} / {selected.externalId}</small>
                  </div>
                </div>

                <div className="reason-list">
                  {selected.matchReasons.map((reason) => <span key={reason}><Check size={12} /> {reason}</span>)}
                </div>

                <div className="field-policy">
                  <div>
                    <strong>选择要采用的字段</strong>
                    <button
                      type="button"
                      aria-label={allAvailableFieldsSelected ? '取消全选字段' : '全选可用字段'}
                      disabled={availableFieldIDs.length === 0}
                      onClick={() => setFields((current) => {
                        const next = new Set(current);
                        if (allAvailableFieldsSelected) availableFieldIDs.forEach((field) => next.delete(field));
                        else availableFieldIDs.forEach((field) => next.add(field));
                        return next;
                      })}
                    >{allAvailableFieldsSelected ? '全不选' : '全选'}</button>
                  </div>
                  <div className="field-chips">
                    {fieldOptions.map((field) => {
                      const available = field.available(selected);
                      return (
                        <button
                          key={field.id}
                          className={cn(available && fields.has(field.id) && 'is-active')}
                          disabled={!available}
                          onClick={() => toggleField(field.id)}
                        >
                          <span>{available && fields.has(field.id) && <Check size={11} />}</span>
                          {field.label}
                        </button>
                      );
                    })}
                  </div>
                </div>

                <div className="candidate-diff">
                  <div className="diff-column-head"><span>字段</span><span>当前文件</span><span>候选值</span></div>
                  {fieldOptions.filter((field) => field.available(selected)).map((field) => (
                    <DiffRow
                      key={field.id}
                      label={field.label}
                      current={field.currentText(track)}
                      candidate={field.candidateText(selected)}
                      active={fields.has(field.id)}
                      warning={fieldWarning(field, selected)}
                    />
                  ))}
                </div>

                {selected.lyrics?.value && (
                  <div className="lyrics-candidate-editor">
                    <div className="lyrics-candidate-editor-head">
                      <div>
                        <strong>歌词内容</strong>
                        <span>{selected.lyrics.source} · 写入前可编辑</span>
                      </div>
                      {includeLyrics && <em>将写入内嵌标签</em>}
                    </div>
                    <textarea
                      aria-label="远程歌词内容"
                      value={lyricsDraft}
                      onChange={(event) => setLyricsDraft(event.target.value)}
                      spellCheck={false}
                    />
                    <small>可以修正错字、时间轴或补充内容；勾选下方“同时写入歌词”后才会保存到音频文件。</small>
                  </div>
                )}

                <div className="asset-options" aria-label="附加资源写入选项">
                  <label className={cn(selected.hasArtwork && 'is-available')}>
					<input
					  type="checkbox"
					  disabled={!selected.hasArtwork}
					  checked={includeArtwork}
					  onChange={(event) => setIncludeArtwork(event.target.checked)}
					/>
                    <Image size={16} />
                    <span><strong>同时写入封面</strong><small>{selected.hasArtwork ? '勾选后把候选图片嵌入当前音频；不勾选则保留现有封面' : '当前来源不提供图片'}</small></span>
                  </label>
                  <label className={cn(selected.hasLyrics && 'is-available')}>
                    <input
                      type="checkbox"
                      disabled={!selected.hasLyrics || !selected.lyrics?.value}
                      checked={includeLyrics}
					  onChange={(event) => setIncludeLyrics(event.target.checked)}
					/>
                    <Music2 size={16} />
                    <span><strong>同时写入歌词</strong><small>{selected.hasLyrics ? '勾选后把上方编辑后的歌词写入音频标签；不勾选则保留现有歌词' : '当前来源不提供歌词'}</small></span>
                  </label>
                </div>
                <label className="artwork-size-control">
                  <span>封面写入尺寸</span>
                  <select aria-label="封面写入尺寸" value={artworkMaxSize} disabled={!includeArtwork} onChange={(event) => setArtworkMaxSize(Number(event.target.value))}>
                    <option value={0}>保留原图</option>
                    <option value={1000}>居中裁剪至 1000×1000</option>
                    <option value={500}>居中裁剪至 500×500</option>
                  </select>
                  <small>只在勾选写入封面时生效，原图不会被修改。</small>
                </label>
              </section>
            )}
          </div>
        )}

        <div className="candidate-footer">
          <button className="secondary-button" onClick={onClose}><ChevronLeft size={15} /> 返回编辑</button>
          <div>
            <span>采用 {fields.size} 组字段 · 附加资源勾选后会写入音频内嵌数据</span>
            <button
              className="primary-button"
              disabled={!selected || applying || loading}
              onClick={async () => {
                if (!selected) return;
                setApplying(true);
                try {
                  await onApply(buildPatch(), selected, {artwork: includeArtwork, ...(includeArtwork && artworkMaxSize > 0 ? {artworkMaxSize} : {})});
                  onClose();
                } finally {
                  setApplying(false);
                }
              }}
            >
              {applying ? <LoaderCircle className="spin" size={15} /> : <Sparkles size={15} />}
              采用所选资料
            </button>
          </div>
        </div>
      </aside>
    </div>
  );
}

function DiffRow({label, current, candidate, active, warning}: {label: string; current: string; candidate: string; active: boolean; warning?: string}) {
  const same = current === candidate;
  return (
    <div className={cn('candidate-diff-row', !active && 'is-muted', warning && 'has-warning')}>
      <span>{label}</span>
      <span>{current}</span>
      <span className={cn(!same && active && 'is-changed')}>{candidate}{warning && <small>{warning}</small>}</span>
    </div>
  );
}
