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
  onApply: (patch: TrackPatch, candidate: MatchCandidate, options: {artwork: boolean}) => Promise<void>;
}

const fieldOptions = [
  {id: 'title', label: '标题'},
  {id: 'artists', label: '艺术家'},
  {id: 'album', label: '专辑'},
  {id: 'albumArtists', label: '专辑艺术家'},
  {id: 'track', label: '音轨 / 光盘'},
  {id: 'year', label: '年份'},
  {id: 'genres', label: '风格'},
  {id: 'comment', label: '注释'},
  {id: 'composers', label: '作曲家'},
  {id: 'conductor', label: '指挥'},
  {id: 'lyricists', label: '作词家'},
  {id: 'copyright', label: '版权'},
  {id: 'bpm', label: 'BPM'},
  {id: 'isrc', label: 'ISRC'},
  {id: 'musicbrainzTrackId', label: 'MB Track ID'},
  {id: 'musicbrainzReleaseId', label: 'MB Release ID'},
  {id: 'musicbrainzArtistIds', label: 'MB Artist ID'},
  {id: 'acoustidId', label: 'AcoustID'},
  {id: 'acoustidFingerprint', label: 'AcoustID 指纹'},
] as const;

type FieldID = typeof fieldOptions[number]['id'];

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
  const [fields, setFields] = useState<Set<FieldID>>(new Set(fieldOptions.map((item) => item.id)));
  const [applying, setApplying] = useState(false);
  const [includeLyrics, setIncludeLyrics] = useState(false);
  const [includeArtwork, setIncludeArtwork] = useState(false);
  const [queryEditing, setQueryEditing] = useState(false);
  const [queryDraft, setQueryDraft] = useState<CandidateSearchQuery>({title: '', artists: [], album: '', durationSeconds: 0});
  const [queryArtistsDraft, setQueryArtistsDraft] = useState('');
  const [queryHistory, setQueryHistory] = useState<CandidateSearchQuery[]>([]);
  const [historySelection, setHistorySelection] = useState('');

  useEffect(() => {
    setSelectedId(candidates[0]?.id ?? null);
	const first = candidates[0];
	setIncludeLyrics(focus === 'lyrics' && Boolean(first?.hasLyrics && first.lyrics?.value));
	setIncludeArtwork(false);
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

  useEffect(() => {
    if (!selected) return;
    setFields(new Set(fieldOptions.filter((field) => candidateHasField(selected, field.id)).map((field) => field.id)));
	setIncludeLyrics(focus === 'lyrics' && Boolean(selected.hasLyrics && selected.lyrics?.value));
	setIncludeArtwork(false);
  }, [focus, selected]);

  if (!open || !track) return null;

  const buildPatch = (): TrackPatch => {
    if (!selected) {
      return {
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
    }
    return {
      title: fields.has('title') && selected.title.value ? selected.title.value : track.title,
      artists: fields.has('artists') && selected.artists.value.length > 0 ? selected.artists.value : track.artists,
      album: fields.has('album') && selected.album.value ? selected.album.value : track.album,
      albumArtists: fields.has('albumArtists') && selected.albumArtists.value.length > 0 ? selected.albumArtists.value : track.albumArtists,
      trackNumber: fields.has('track') && selected.trackNumber.value > 0 ? selected.trackNumber.value : track.trackNumber,
      trackTotal: fields.has('track') && selected.trackTotal.value > 0 ? selected.trackTotal.value : track.trackTotal,
      discNumber: fields.has('track') && selected.discNumber.value > 0 ? selected.discNumber.value : track.discNumber,
	  discTotal: fields.has('track') && selected.discTotal.value > 0 ? selected.discTotal.value : track.discTotal,
      year: fields.has('year') && selected.year.value > 0 ? selected.year.value : track.year,
      genres: fields.has('genres') && selected.genres.value.length > 0 ? selected.genres.value : track.genres,
      lyrics: includeLyrics && selected.hasLyrics && selected.lyrics?.value ? selected.lyrics.value : track.lyrics,
      comment: fields.has('comment') && selected.comment?.value ? selected.comment.value : track.comment,
      composers: fields.has('composers') && selected.composers?.value.length ? selected.composers.value : track.composers,
      conductor: fields.has('conductor') && selected.conductor?.value ? selected.conductor.value : track.conductor,
      lyricists: fields.has('lyricists') && selected.lyricists?.value.length ? selected.lyricists.value : track.lyricists,
      copyright: fields.has('copyright') && selected.copyright?.value ? selected.copyright.value : track.copyright,
      bpm: fields.has('bpm') && selected.bpm?.value ? selected.bpm.value : track.bpm,
      isrc: fields.has('isrc') && selected.isrc?.value ? selected.isrc.value : track.isrc,
      musicbrainzTrackId: fields.has('musicbrainzTrackId') && selected.musicbrainzTrackId?.value ? selected.musicbrainzTrackId.value : track.musicbrainzTrackId,
      musicbrainzReleaseId: fields.has('musicbrainzReleaseId') && selected.musicbrainzReleaseId?.value ? selected.musicbrainzReleaseId.value : track.musicbrainzReleaseId,
      musicbrainzArtistIds: fields.has('musicbrainzArtistIds') && selected.musicbrainzArtistIds?.value.length ? selected.musicbrainzArtistIds.value : track.musicbrainzArtistIds,
      acoustidId: fields.has('acoustidId') && selected.acoustidId?.value ? selected.acoustidId.value : track.acoustidId,
      acoustidFingerprint: fields.has('acoustidFingerprint') && selected.acoustidFingerprint?.value ? selected.acoustidFingerprint.value : track.acoustidFingerprint,
    };
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
                  <CoverArt
                    title={selected.title.value}
                    artist={selected.artists.value[0]}
                    tone={selected.coverTone}
                    missing={!showGeneratedCovers && !candidateArtworkURL(selected)}
                    imageUrl={candidateArtworkURL(selected)}
                    blankOnImageError={!showGeneratedCovers}
                    size="md"
                  />
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
                    <button onClick={() => setFields(new Set(
                      fieldOptions.filter((item) => candidateHasField(selected, item.id)).map((item) => item.id),
                    ))}>全选</button>
                  </div>
                  <div className="field-chips">
                    {fieldOptions.map((field) => {
                      const available = candidateHasField(selected, field.id);
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
                  <DiffRow label="标题" current={track.title} candidate={selected.title.value} active={fields.has('title')} />
                  <DiffRow label="艺术家" current={track.artists.join(' / ')} candidate={selected.artists.value.join(' / ')} active={fields.has('artists')} />
                  <DiffRow label="专辑" current={track.album || '空'} candidate={selected.album.value} active={fields.has('album')} />
                  <DiffRow
                    label="音轨"
                    current={track.trackNumber ? `${track.trackNumber} / ${track.trackTotal || '—'}` : '空'}
                    candidate={selected.trackNumber.value > 0
                      ? `${selected.trackNumber.value} / ${selected.trackTotal.value || '—'}`
                      : '来源未提供'}
                    active={fields.has('track')}
                  />
                  <DiffRow label="年份" current={String(track.year || '空')} candidate={String(selected.year.value || '来源未提供')} active={fields.has('year')} />
                  <DiffRow label="风格" current={track.genres.join(', ') || '空'} candidate={selected.genres.value.join(', ')} active={fields.has('genres')} />
                  {selected.comment?.value && <DiffRow label="注释" current={track.comment || '空'} candidate={selected.comment.value} active={fields.has('comment')} />}
                  {selected.composers?.value.length ? <DiffRow label="作曲家" current={track.composers.join(' / ') || '空'} candidate={selected.composers.value.join(' / ')} active={fields.has('composers')} /> : null}
                  {selected.conductor?.value && <DiffRow label="指挥" current={track.conductor || '空'} candidate={selected.conductor.value} active={fields.has('conductor')} />}
                  {selected.lyricists?.value.length ? <DiffRow label="作词家" current={track.lyricists.join(' / ') || '空'} candidate={selected.lyricists.value.join(' / ')} active={fields.has('lyricists')} /> : null}
                  {selected.copyright?.value && <DiffRow label="版权" current={track.copyright || '空'} candidate={selected.copyright.value} active={fields.has('copyright')} />}
                  {selected.bpm?.value ? <DiffRow label="BPM" current={String(track.bpm || '空')} candidate={String(selected.bpm.value)} active={fields.has('bpm')} /> : null}
                  {selected.isrc?.value && <DiffRow label="ISRC" current={track.isrc || '空'} candidate={selected.isrc.value} active={fields.has('isrc')} />}
                  {selected.musicbrainzTrackId?.value && <DiffRow label="MB Track ID" current={track.musicbrainzTrackId || '空'} candidate={selected.musicbrainzTrackId.value} active={fields.has('musicbrainzTrackId')} />}
                  {selected.musicbrainzReleaseId?.value && <DiffRow label="MB Release ID" current={track.musicbrainzReleaseId || '空'} candidate={selected.musicbrainzReleaseId.value} active={fields.has('musicbrainzReleaseId')} />}
                  {selected.musicbrainzArtistIds?.value.length ? <DiffRow label="MB Artist ID" current={track.musicbrainzArtistIds.join(' / ') || '空'} candidate={selected.musicbrainzArtistIds.value.join(' / ')} active={fields.has('musicbrainzArtistIds')} /> : null}
                  {selected.acoustidId?.value && <DiffRow label="AcoustID" current={track.acoustidId || '空'} candidate={selected.acoustidId.value} active={fields.has('acoustidId')} />}
                  {selected.acoustidFingerprint?.value && <DiffRow label="AcoustID 指纹" current={track.acoustidFingerprint || '空'} candidate={selected.acoustidFingerprint.value} active={fields.has('acoustidFingerprint')} />}
                </div>

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
                    <span><strong>同时写入歌词</strong><small>{selected.hasLyrics ? '勾选后把候选歌词写入音频标签；不勾选则保留现有歌词' : '当前来源不提供歌词'}</small></span>
                  </label>
                </div>
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
				  await onApply(buildPatch(), selected, {artwork: includeArtwork});
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

function DiffRow({label, current, candidate, active}: {label: string; current: string; candidate: string; active: boolean}) {
  const same = current === candidate;
  return (
    <div className={cn('candidate-diff-row', !active && 'is-muted')}>
      <span>{label}</span>
      <span>{current}</span>
      <span className={cn(!same && active && 'is-changed')}>{candidate}</span>
    </div>
  );
}

function candidateHasField(candidate: MatchCandidate, field: FieldID): boolean {
  switch (field) {
    case 'title': return Boolean(candidate.title.value);
    case 'artists': return candidate.artists.value.length > 0;
    case 'album': return Boolean(candidate.album.value);
    case 'albumArtists': return candidate.albumArtists.value.length > 0;
    case 'track': return candidate.trackNumber.value > 0 || candidate.trackTotal.value > 0 || candidate.discNumber.value > 0;
    case 'year': return candidate.year.value > 0;
    case 'genres': return candidate.genres.value.length > 0;
    case 'comment': return Boolean(candidate.comment?.value);
    case 'composers': return Boolean(candidate.composers?.value.length);
    case 'conductor': return Boolean(candidate.conductor?.value);
    case 'lyricists': return Boolean(candidate.lyricists?.value.length);
    case 'copyright': return Boolean(candidate.copyright?.value);
    case 'bpm': return Boolean(candidate.bpm?.value);
    case 'isrc': return Boolean(candidate.isrc?.value);
    case 'musicbrainzTrackId': return Boolean(candidate.musicbrainzTrackId?.value);
    case 'musicbrainzReleaseId': return Boolean(candidate.musicbrainzReleaseId?.value);
    case 'musicbrainzArtistIds': return Boolean(candidate.musicbrainzArtistIds?.value.length);
    case 'acoustidId': return Boolean(candidate.acoustidId?.value);
    case 'acoustidFingerprint': return Boolean(candidate.acoustidFingerprint?.value);
  }
}
