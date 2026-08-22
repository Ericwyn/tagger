import {useEffect, useMemo, useState} from 'react';
import {
  ArrowLeft,
  Check,
  ChevronDown,
  ChevronRight,
  CircleAlert,
  FileCheck2,
  LoaderCircle,
  Search,
  Sparkles,
  X,
} from 'lucide-react';
import {CoverArt} from '@/components/CoverArt';
import {cn, formatDuration} from '@/lib/utils';
import {candidatesFor} from '@/mock/data';
import {apiReadMode, candidateArtworkURL, createMatchJob, createWriteJob, getJob, listMatchItems, listTracks, updateMatchItem, waitForJob} from '@/api';
import type {Job, MatchCandidate, MatchItem, Track} from '@/types';

interface ReviewPageProps {
  trackIds: string[];
  matchJobId?: string;
  showGeneratedCovers?: boolean;
  onBack: () => void;
  onComplete: () => void;
}

type ReviewState = 'accepted' | 'review' | 'skipped';

interface ReviewItem {
  track: Track;
  candidate?: MatchCandidate;
  candidates: MatchCandidate[];
  state: ReviewState;
  fields: string[];
  includeArtwork: boolean;
  error?: string;
}

const standardFields = [
  'title',
  'artists',
  'album',
  'albumArtists',
  'trackNumber',
  'trackTotal',
  'discNumber',
  'discTotal',
  'year',
  'genres',
  'comment',
  'composers',
  'conductor',
  'lyricists',
  'copyright',
  'bpm',
  'isrc',
  'musicbrainzTrackId',
  'musicbrainzReleaseId',
  'musicbrainzArtistIds',
  'acoustidId',
  'acoustidFingerprint',
  'lyrics',
] as const;

function availableFields(candidate: MatchCandidate): string[] {
  return standardFields.filter((field) => {
    switch (field) {
      case 'title': return Boolean(candidate.title.value);
      case 'artists': return candidate.artists.value.length > 0;
      case 'album': return Boolean(candidate.album.value);
      case 'albumArtists': return candidate.albumArtists.value.length > 0;
      case 'trackNumber': return candidate.trackNumber.value > 0;
      case 'trackTotal': return candidate.trackTotal.value > 0;
      case 'discNumber': return candidate.discNumber.value > 0;
      case 'discTotal': return candidate.discTotal.value > 0;
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
      case 'lyrics': return Boolean(candidate.lyrics?.value);
    }
  });
}

export function ReviewPage({trackIds, matchJobId, showGeneratedCovers = false, onBack, onComplete}: ReviewPageProps) {
  const [items, setItems] = useState<ReviewItem[]>([]);
  const [activeId, setActiveId] = useState<string>();
  const [loading, setLoading] = useState(true);
  const [applying, setApplying] = useState(false);
  const [query, setQuery] = useState('');
	const [job, setJob] = useState<Job>();

  useEffect(() => {
    let active = true;
    void listTracks().then(async (allTracks) => {
      const ids = trackIds.length ? new Set(trackIds) : new Set(allTracks.filter((track) => track.album === '安泊猜想').map((track) => track.id));
      let next = allTracks.filter((track) => ids.has(track.id));
      let candidateOptionsByTrack = new Map<string, MatchCandidate[]>();
      let errorByTrack = new Map<string, string>();
      let matchStateByTrack = new Map<string, MatchItem['state']>();
      let selectedCandidateByTrack = new Map<string, string>();
      let reviewFieldsByTrack = new Map<string, string[] | null | undefined>();
      let reviewArtworkByTrack = new Map<string, boolean>();
      if (apiReadMode === 'real') {
        const created = matchJobId ? await getJob(matchJobId) : await createMatchJob(next.map((track) => track.id));
        if (created) {
          if (active) setJob(created);
          const completed = matchJobId ? created : await waitForJob(created.id);
          if (active) setJob(completed);
          const matchItems = await listMatchItems(created.id);
          if (matchJobId && trackIds.length === 0) {
            const persistedIds = new Set(matchItems.map((item) => item.trackId));
            next = allTracks.filter((track) => persistedIds.has(track.id));
          }
          candidateOptionsByTrack = new Map(matchItems.map((item) => [item.trackId, item.candidates] as const));
          errorByTrack = new Map(matchItems.filter((item) => item.error).map((item) => [item.trackId, item.error!]));
          matchStateByTrack = new Map(matchItems
            .map((item) => [item.trackId, item.state] as const));
          selectedCandidateByTrack = new Map(matchItems
            .filter((item) => item.selectedCandidateId)
            .map((item) => [item.trackId, item.selectedCandidateId!] as const));
          reviewFieldsByTrack = new Map(matchItems.map((item) => [item.trackId, item.reviewFields] as const));
          reviewArtworkByTrack = new Map(matchItems.map((item) => [item.trackId, Boolean(item.reviewArtwork)] as const));
        }
      }
      const reviewed = next.map((track) => {
        const candidates = apiReadMode === 'mock' ? candidatesFor(track) : (candidateOptionsByTrack.get(track.id) ?? []);
        const itemState = matchStateByTrack.get(track.id);
        const candidate = candidates.find((item) => item.id === selectedCandidateByTrack.get(track.id)) ?? candidates[0];
        const noMatch = !candidate || itemState === 'no_match' || itemState === 'failed';
        const persistedFields = reviewFieldsByTrack.get(track.id);
        return {
          track,
          candidate,
          candidates,
          fields: candidate ? (persistedFields == null ? availableFields(candidate) : persistedFields) : [],
          includeArtwork: reviewArtworkByTrack.get(track.id) ?? false,
          error: errorByTrack.get(track.id),
          state: noMatch || itemState === 'skipped' ? 'skipped' as const : itemState === 'accepted' ? 'accepted' as const : candidate && candidate.score >= 0.92 && availableFields(candidate).length > 0 ? 'accepted' as const : 'review' as const,
        };
      });
      if (!active) return;
      setItems(reviewed);
      setActiveId(reviewed[0]?.track.id);
      setLoading(false);
    }).catch(() => {
      if (active) setLoading(false);
    });
    return () => { active = false; };
  }, [matchJobId, trackIds]);

  const visibleItems = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase();
    if (!normalized) return items;
    return items.filter(({track}) => [track.title, track.fileName, track.artists.join(' ')].some((value) => value.toLocaleLowerCase().includes(normalized)));
  }, [items, query]);

  const active = items.find(({track}) => track.id === activeId) ?? items[0];
  const accepted = items.filter((item) => item.state === 'accepted').length;
  const needsReview = items.filter((item) => item.state === 'review').length;
  const skipped = items.filter((item) => item.state === 'skipped').length;

  const setItemState = (trackId: string, state: ReviewState) => {
    setItems((current) => current.map((item) => item.track.id === trackId ? {...item, state} : item));
  };

  const persistReviewState = (trackId: string, state: 'review' | 'accepted' | 'skipped', candidateId?: string, fields?: string[], artwork?: boolean) => {
    if (apiReadMode !== 'real' || !job) return;
    void updateMatchItem(job.id, trackId, state, candidateId, fields, artwork).catch(() => undefined);
  };

  const toggleFields = (trackId: string, fields: string[]) => {
    const item = items.find((entry) => entry.track.id === trackId);
    if (!item) return;
    const next = new Set(item.fields);
    const shouldEnable = fields.some((field) => !next.has(field));
    fields.forEach((field) => shouldEnable ? next.add(field) : next.delete(field));
    const nextFields = [...next];
    const nextState = next.size === 0 && item.state === 'accepted' ? 'review' : item.state;
    setItems((current) => current.map((entry) => entry.track.id === trackId ? {...entry, fields: nextFields, state: nextState} : entry));
    persistReviewState(trackId, nextState, item.candidate?.id, nextFields, item.includeArtwork);
  };

  const toggleField = (trackId: string, field: string) => toggleFields(trackId, [field]);

  const moveToNext = (trackId: string) => {
    const index = visibleItems.findIndex((item) => item.track.id === trackId);
    if (index < 0) return;
    const next = visibleItems.slice(index + 1).find((item) => item.state !== 'skipped') ?? visibleItems[index + 1] ?? visibleItems[0];
    if (next && next.track.id !== trackId) setActiveId(next.track.id);
  };

  const changeCandidate = (trackId: string) => {
    const item = items.find((entry) => entry.track.id === trackId);
    if (!item || item.candidates.length < 2) return;
    const currentIndex = item.candidates.findIndex((candidate) => candidate.id === item.candidate?.id);
    const nextCandidate = item.candidates[(currentIndex + 1) % item.candidates.length];
    setItems((current) => current.map((entry) => entry.track.id === trackId
      ? {...entry, candidate: nextCandidate, fields: availableFields(nextCandidate), state: 'review'}
      : entry));
    persistReviewState(trackId, 'review', nextCandidate.id, availableFields(nextCandidate), false);
  };

  const toggleArtwork = (trackId: string) => {
    const item = items.find((entry) => entry.track.id === trackId);
    if (!item) return;
    const includeArtwork = !item.includeArtwork;
    setItems((current) => current.map((entry) => entry.track.id === trackId ? {...entry, includeArtwork} : entry));
    persistReviewState(trackId, item.state, item.candidate?.id, item.fields, includeArtwork);
  };

  if (loading) {
    return <div className="app-loading"><LoaderCircle className="spin" size={22} /> 正在整理候选结果…</div>;
  }

  return (
    <div className="review-page">
      <header className="review-header">
        <button className="back-button" onClick={onBack}><ArrowLeft size={17} /> 返回曲库</button>
        <div>
          <div className="eyebrow">BATCH REVIEW / {job ? job.id.toUpperCase() : 'JOB DRAFT'}</div>
          <h1>审核抓取结果</h1>
          <p>自动分析已完成。确认字段差异后才会创建写入任务。</p>
        </div>
        <div className="review-stats">
          <div><strong>{accepted}</strong><span>高置信 / 已接受</span></div>
          <div><strong>{needsReview}</strong><span>需要人工确认</span></div>
          <div><strong>{skipped}</strong><span>已跳过</span></div>
        </div>
      </header>

      <div className="review-toolbar">
        <label className="search-box">
          <Search size={16} />
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="在本批次中搜索…" />
          {query && <button title="清除搜索" onClick={() => setQuery('')}><X size={14} /></button>}
        </label>
        <button className="toolbar-button">状态：全部 <ChevronDown size={13} /></button>
        <button className="toolbar-button">来源：全部 <ChevronDown size={13} /></button>
        <span />
        <button
          className="secondary-button"
          onClick={() => {
            const highConfidence = items.filter((item) => item.candidate && item.candidate.score >= 0.92);
            setItems((current) => current.map((item) => item.candidate && item.candidate.score >= 0.92 ? {...item, state: 'accepted'} : item));
            highConfidence.forEach((item) => persistReviewState(item.track.id, 'accepted', item.candidate?.id, item.fields, item.includeArtwork));
          }}
        >
          <Check size={15} /> 接受所有高置信
        </button>
      </div>

      <div className="review-layout">
        <section className="review-list">
          <div className="review-list-head">
            <span>本地曲目</span>
            <span>最佳候选</span>
            <span>状态</span>
          </div>
          {visibleItems.map((item) => (
            <button
              key={item.track.id}
              className={cn('review-row', active?.track.id === item.track.id && 'is-active')}
              onClick={() => setActiveId(item.track.id)}
            >
              <CoverArt title={item.track.title} artist={item.track.artists[0]} tone={item.track.coverTone} missing={!showGeneratedCovers && item.track.artworkCount === 0} size="xs" />
              <span className="review-track-copy">
                <strong>{item.track.title}</strong>
                <small>{item.track.fileName}</small>
              </span>
              <span className="review-arrow"><ChevronRight size={15} /></span>
              {item.candidate ? (
                <>
                  <CoverArt title={item.candidate.title.value} artist={item.candidate.artists.value[0]} tone={item.candidate.coverTone} missing={!showGeneratedCovers && !candidateArtworkURL(item.candidate)} imageUrl={candidateArtworkURL(item.candidate)} blankOnImageError={!showGeneratedCovers} size="xs" />
                  <span className="review-candidate-copy">
                    <strong>{item.candidate.title.value}</strong>
                    <small>{item.candidate.providerName} · {Math.round(item.candidate.score * 100)}%</small>
                  </span>
                </>
              ) : (
                <>
                  <CoverArt title="" tone="charcoal" size="xs" missing={!showGeneratedCovers} />
                  <span className="review-candidate-copy">
                    <strong>未找到匹配</strong>
                    <small>{item.error || '没有可用的数据源候选'}</small>
                  </span>
                </>
              )}
              <span className={cn('review-state', `is-${item.state}`)}>
                {item.state === 'accepted' && <Check size={13} />}
                {item.state === 'review' && <CircleAlert size={13} />}
                {item.state === 'skipped' && <X size={13} />}
                {item.state === 'accepted' ? '已接受' : item.state === 'review' ? '待确认' : '已跳过'}
              </span>
            </button>
          ))}
        </section>

        {active && active.candidate && (
          <section className="review-detail">
            <div className="review-detail-head">
              <div className="record-comparison">
                <CoverArt title={active.track.title} artist={active.track.artists[0]} tone={active.track.coverTone} missing={!showGeneratedCovers && active.track.artworkCount === 0} size="md" />
                <div className="comparison-line"><span /><Sparkles size={16} /><span /></div>
                <CoverArt title={active.candidate.title.value} artist={active.candidate.artists.value[0]} tone={active.candidate.coverTone} missing={!showGeneratedCovers && !candidateArtworkURL(active.candidate)} imageUrl={candidateArtworkURL(active.candidate)} blankOnImageError={!showGeneratedCovers} size="md" />
              </div>
              <div>
                <span className="confidence-badge"><Check size={13} /> {active.candidate.scoreLabel} · {Math.round(active.candidate.score * 100)}%</span>
                <h2>{active.track.title}</h2>
                <p>{active.track.artists.join(' / ')} · {formatDuration(active.track.durationSeconds)}</p>
                <small>候选来源：{active.candidate.providerName} / {active.candidate.externalId}</small>
              </div>
            </div>

            <div className="review-policy-note">
              <FileCheck2 size={18} />
              <div>
                <strong>当前策略：采用所选字段</strong>
                <span>不会删除候选中缺失的标签；评论和未知标签保持不变。</span>
              </div>
              <button>修改策略</button>
            </div>

            <div className="review-diff-table">
              <div className="review-diff-head"><span>采用</span><span>字段</span><span>当前值</span><span>候选值</span><span>来源</span></div>
              <ReviewDiff field="title" label="标题" current={active.track.title} next={active.candidate.title.value} source={active.candidate.providerName} checked={active.fields.includes('title')} onToggle={() => toggleField(active.track.id, 'title')} />
              <ReviewDiff field="artists" label="艺术家" current={active.track.artists.join(' / ')} next={active.candidate.artists.value.join(' / ')} source={active.candidate.providerName} checked={active.fields.includes('artists')} onToggle={() => toggleField(active.track.id, 'artists')} />
              <ReviewDiff field="album" label="专辑" current={active.track.album || '空'} next={active.candidate.album.value} source={active.candidate.providerName} changed={!active.track.album} checked={active.fields.includes('album')} onToggle={() => toggleField(active.track.id, 'album')} />
              <ReviewDiff field="albumArtists" label="专辑艺术家" current={active.track.albumArtists.join(' / ') || '空'} next={active.candidate.albumArtists.value.join(' / ') || '来源未提供'} source={active.candidate.providerName} checked={active.fields.includes('albumArtists')} onToggle={() => toggleField(active.track.id, 'albumArtists')} />
              <ReviewDiff
                field="trackNumber"
                label="音轨"
                current={active.track.trackNumber ? `${active.track.trackNumber} / ${active.track.trackTotal || '—'}` : '空'}
                next={`${active.candidate.trackNumber.value} / ${active.candidate.trackTotal.value}`}
                source={active.candidate.providerName}
                changed={!active.track.trackNumber}
                checked={active.fields.includes('trackNumber') || active.fields.includes('trackTotal')}
                onToggle={() => toggleFields(active.track.id, ['trackNumber', 'trackTotal', 'discNumber', 'discTotal'])}
              />
              <ReviewDiff field="year" label="年份" current={String(active.track.year || '空')} next={String(active.candidate.year.value)} source={active.candidate.providerName} checked={active.fields.includes('year')} onToggle={() => toggleField(active.track.id, 'year')} />
              <ReviewDiff field="genres" label="风格" current={active.track.genres.join(', ') || '空'} next={active.candidate.genres.value.join(', ')} source={active.candidate.providerName} checked={active.fields.includes('genres')} onToggle={() => toggleField(active.track.id, 'genres')} />
              {active.candidate.comment?.value && <ReviewDiff field="comment" label="注释" current={active.track.comment || '空'} next={active.candidate.comment.value} source={active.candidate.providerName} checked={active.fields.includes('comment')} onToggle={() => toggleField(active.track.id, 'comment')} />}
              {active.candidate.composers?.value.length ? <ReviewDiff field="composers" label="作曲家" current={active.track.composers.join(' / ') || '空'} next={active.candidate.composers.value.join(' / ')} source={active.candidate.providerName} checked={active.fields.includes('composers')} onToggle={() => toggleField(active.track.id, 'composers')} /> : null}
              {active.candidate.conductor?.value && <ReviewDiff field="conductor" label="指挥" current={active.track.conductor || '空'} next={active.candidate.conductor.value} source={active.candidate.providerName} checked={active.fields.includes('conductor')} onToggle={() => toggleField(active.track.id, 'conductor')} />}
              {active.candidate.lyricists?.value.length ? <ReviewDiff field="lyricists" label="作词家" current={active.track.lyricists.join(' / ') || '空'} next={active.candidate.lyricists.value.join(' / ')} source={active.candidate.providerName} checked={active.fields.includes('lyricists')} onToggle={() => toggleField(active.track.id, 'lyricists')} /> : null}
              {active.candidate.copyright?.value && <ReviewDiff field="copyright" label="版权" current={active.track.copyright || '空'} next={active.candidate.copyright.value} source={active.candidate.providerName} checked={active.fields.includes('copyright')} onToggle={() => toggleField(active.track.id, 'copyright')} />}
              {active.candidate.bpm?.value ? <ReviewDiff field="bpm" label="BPM" current={String(active.track.bpm || '空')} next={String(active.candidate.bpm.value)} source={active.candidate.providerName} checked={active.fields.includes('bpm')} onToggle={() => toggleField(active.track.id, 'bpm')} /> : null}
              {active.candidate.isrc?.value && <ReviewDiff field="isrc" label="ISRC" current={active.track.isrc || '空'} next={active.candidate.isrc.value} source={active.candidate.providerName} checked={active.fields.includes('isrc')} onToggle={() => toggleField(active.track.id, 'isrc')} />}
              {active.candidate.musicbrainzTrackId?.value && <ReviewDiff field="musicbrainzTrackId" label="MB Track ID" current={active.track.musicbrainzTrackId || '空'} next={active.candidate.musicbrainzTrackId.value} source={active.candidate.providerName} checked={active.fields.includes('musicbrainzTrackId')} onToggle={() => toggleField(active.track.id, 'musicbrainzTrackId')} />}
              {active.candidate.musicbrainzReleaseId?.value && <ReviewDiff field="musicbrainzReleaseId" label="MB Release ID" current={active.track.musicbrainzReleaseId || '空'} next={active.candidate.musicbrainzReleaseId.value} source={active.candidate.providerName} checked={active.fields.includes('musicbrainzReleaseId')} onToggle={() => toggleField(active.track.id, 'musicbrainzReleaseId')} />}
              {active.candidate.musicbrainzArtistIds?.value.length ? <ReviewDiff field="musicbrainzArtistIds" label="MB Artist ID" current={active.track.musicbrainzArtistIds.join(' / ') || '空'} next={active.candidate.musicbrainzArtistIds.value.join(' / ')} source={active.candidate.providerName} checked={active.fields.includes('musicbrainzArtistIds')} onToggle={() => toggleField(active.track.id, 'musicbrainzArtistIds')} /> : null}
              {active.candidate.acoustidId?.value && <ReviewDiff field="acoustidId" label="AcoustID" current={active.track.acoustidId || '空'} next={active.candidate.acoustidId.value} source={active.candidate.providerName} checked={active.fields.includes('acoustidId')} onToggle={() => toggleField(active.track.id, 'acoustidId')} />}
              {active.candidate.acoustidFingerprint?.value && <ReviewDiff field="acoustidFingerprint" label="AcoustID 指纹" current={active.track.acoustidFingerprint || '空'} next={active.candidate.acoustidFingerprint.value} source={active.candidate.providerName} checked={active.fields.includes('acoustidFingerprint')} onToggle={() => toggleField(active.track.id, 'acoustidFingerprint')} />}
              {active.candidate.lyrics?.value && <ReviewDiff field="lyrics" label="歌词" current={active.track.lyrics ? '已有歌词' : '空'} next="来源提供歌词" source={active.candidate.providerName} checked={active.fields.includes('lyrics')} onToggle={() => toggleField(active.track.id, 'lyrics')} />}
              {active.candidate.hasArtwork && <ReviewDiff field="artwork" label="封面" current={active.track.artworkCount > 0 ? '已有封面' : '空'} next="来源提供封面" source={active.candidate.providerName} checked={active.includeArtwork} onToggle={() => toggleArtwork(active.track.id)} />}
            </div>

            <div className="review-detail-actions">
              <button className="danger-quiet" onClick={() => { setItemState(active.track.id, 'skipped'); persistReviewState(active.track.id, 'skipped', active.candidate?.id, active.fields, active.includeArtwork); moveToNext(active.track.id); }}><X size={15} /> 跳过此曲</button>
              <button className="secondary-button" disabled={active.candidates.length < 2} onClick={() => changeCandidate(active.track.id)}><ChevronRight size={15} /> 更换候选</button>
              <button className="primary-button" onClick={() => { setItemState(active.track.id, 'accepted'); persistReviewState(active.track.id, 'accepted', active.candidate?.id, active.fields, active.includeArtwork); moveToNext(active.track.id); }}><Check size={15} /> 接受候选</button>
            </div>
          </section>
        )}
        {active && !active.candidate && (
          <section className="review-detail">
            <div className="empty-state">
              <span>∅</span>
              <strong>没有可审核的候选</strong>
              <p>{active.error || '当前启用的数据源没有返回匹配结果。该曲目已安全跳过，不会创建写入任务。'}</p>
            </div>
          </section>
        )}
      </div>

      <footer className="review-footer">
        <div>
          <strong>{accepted}</strong> 首将写入，<strong>{needsReview}</strong> 首仍需确认，<strong>{skipped}</strong> 首跳过
        </div>
        <div>
          <button className="secondary-button" onClick={onBack}>保存草稿并返回</button>
          <button
            className="primary-button"
            disabled={accepted === 0 || applying}
			onClick={async () => {
			  setApplying(true);
			  if (apiReadMode === 'real' && job) {
				await createWriteJob(job.id, items.filter((item) => item.state === 'accepted').map((item) => ({
				  trackId: item.track.id, candidateId: item.candidate!.id, baseRevision: item.track.revision,
				  fields: item.fields,
				  artwork: item.includeArtwork,
				})));
			  } else {
				await new Promise((resolve) => window.setTimeout(resolve, 700));
			  }
			  onComplete();
			}}
          >
            {applying ? <LoaderCircle className="spin" size={15} /> : <FileCheck2 size={15} />}
            确认并创建写入任务
          </button>
        </div>
      </footer>
    </div>
  );
}

function ReviewDiff({
  field,
  checked,
  onToggle,
  label,
  current,
  next,
  source,
  changed = false,
}: {
  field: string;
  checked: boolean;
  onToggle: () => void;
  label: string;
  current: string;
  next: string;
  source: string;
  changed?: boolean;
}) {
  return (
    <div className={cn('review-diff-row', !checked && 'is-disabled')} data-field={field}>
      <button type="button" aria-label={`${checked ? '取消采用' : '采用'}${label}`} className={cn('square-check', checked && 'is-checked')} onClick={onToggle}>
        {checked && <Check size={12} strokeWidth={3} />}
      </button>
      <strong>{label}</strong>
      <span>{current}</span>
      <span className={cn(changed && 'is-changed')}>{next}</span>
      <em>{source}</em>
    </div>
  );
}
