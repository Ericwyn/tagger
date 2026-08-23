import {useEffect, useMemo, useState} from 'react';
import {createPortal} from 'react-dom';
import {
  ArrowLeft,
  Check,
  ChevronRight,
  CircleAlert,
  FileCheck2,
  LoaderCircle,
  RefreshCw,
  Search,
  Sparkles,
  X,
} from 'lucide-react';
import {CoverArt} from '@/components/CoverArt';
import {cn, formatDuration} from '@/lib/utils';
import {candidatesFor} from '@/mock/data';
import {apiReadMode, artworkURL, candidateArtworkURL, createMatchJob, createWriteJob, getJob, listMatchItems, rematchMatchItem, resolveTracks, subscribeJobEvents, updateMatchItem, waitForJob} from '@/api';
import type {Job, MatchCandidate, MatchItem, Track} from '@/types';

interface ReviewPageProps {
  trackIds: string[];
  matchJobId?: string;
  showGeneratedCovers?: boolean;
  onBack: () => void;
  onComplete: () => void;
  onJobQueued?: (jobId: string) => void;
}

type ReviewState = 'accepted' | 'review' | 'skipped';
type ReviewStatusFilter = 'all' | ReviewState;

interface ReviewItem {
  track: Track;
  candidate?: MatchCandidate;
  candidates: MatchCandidate[];
  state: ReviewState;
  fields: string[];
  includeArtwork: boolean;
  artworkTouched: boolean;
  artworkMaxSize: number;
  error?: string;
}

interface ReviewAssetPreview {
  kind: 'lyrics' | 'artwork';
  track: Track;
  candidate: MatchCandidate;
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

function candidateAssetSummary(candidate: MatchCandidate): string {
  const assets = [candidate.hasArtwork ? '封面' : '', candidate.hasLyrics ? '歌词' : ''].filter(Boolean);
  return assets.length > 0 ? assets.join(' + ') : '仅元数据';
}

function candidateFieldValue(candidate: MatchCandidate, field: string): unknown {
  switch (field) {
    case 'title': return candidate.title.value;
    case 'artists': return candidate.artists.value;
    case 'album': return candidate.album.value;
    case 'albumArtists': return candidate.albumArtists.value;
    case 'trackNumber': return candidate.trackNumber.value;
    case 'trackTotal': return candidate.trackTotal.value;
    case 'discNumber': return candidate.discNumber.value;
    case 'discTotal': return candidate.discTotal.value;
    case 'year': return candidate.year.value;
    case 'genres': return candidate.genres.value;
    case 'comment': return candidate.comment?.value ?? '';
    case 'composers': return candidate.composers?.value ?? [];
    case 'conductor': return candidate.conductor?.value ?? '';
    case 'lyricists': return candidate.lyricists?.value ?? [];
    case 'copyright': return candidate.copyright?.value ?? '';
    case 'bpm': return candidate.bpm?.value ?? 0;
    case 'isrc': return candidate.isrc?.value ?? '';
    case 'musicbrainzTrackId': return candidate.musicbrainzTrackId?.value ?? '';
    case 'musicbrainzReleaseId': return candidate.musicbrainzReleaseId?.value ?? '';
    case 'musicbrainzArtistIds': return candidate.musicbrainzArtistIds?.value ?? [];
    case 'acoustidId': return candidate.acoustidId?.value ?? '';
    case 'acoustidFingerprint': return candidate.acoustidFingerprint?.value ?? '';
    case 'lyrics': return candidate.lyrics?.value ?? '';
    default: return undefined;
  }
}

function trackFieldValue(track: Track, field: string): unknown {
  switch (field) {
    case 'title': return track.title;
    case 'artists': return track.artists;
    case 'album': return track.album;
    case 'albumArtists': return track.albumArtists;
    case 'trackNumber': return track.trackNumber ?? 0;
    case 'trackTotal': return track.trackTotal ?? 0;
    case 'discNumber': return track.discNumber ?? 0;
    case 'discTotal': return track.discTotal ?? 0;
    case 'year': return track.year ?? 0;
    case 'genres': return track.genres;
    case 'comment': return track.comment;
    case 'composers': return track.composers;
    case 'conductor': return track.conductor;
    case 'lyricists': return track.lyricists;
    case 'copyright': return track.copyright;
    case 'bpm': return track.bpm ?? 0;
    case 'isrc': return track.isrc;
    case 'musicbrainzTrackId': return track.musicbrainzTrackId;
    case 'musicbrainzReleaseId': return track.musicbrainzReleaseId;
    case 'musicbrainzArtistIds': return track.musicbrainzArtistIds;
    case 'acoustidId': return track.acoustidId;
    case 'acoustidFingerprint': return track.acoustidFingerprint;
    case 'lyrics': return track.lyrics;
    default: return undefined;
  }
}

function changedFields(track: Track, candidate: MatchCandidate): string[] {
  return availableFields(candidate).filter((field) => JSON.stringify(candidateFieldValue(candidate, field)) !== JSON.stringify(trackFieldValue(track, field)));
}

export function ReviewPage({trackIds, matchJobId, showGeneratedCovers = false, onBack, onComplete, onJobQueued}: ReviewPageProps) {
  const [items, setItems] = useState<ReviewItem[]>([]);
  const [activeId, setActiveId] = useState<string>();
  const [loading, setLoading] = useState(true);
  const [applying, setApplying] = useState(false);
  const [query, setQuery] = useState('');
	const [statusFilter, setStatusFilter] = useState<ReviewStatusFilter>('all');
	const [sourceFilter, setSourceFilter] = useState('all');
	const [rematching, setRematching] = useState(false);
	const [rematchError, setRematchError] = useState('');
	const [writeError, setWriteError] = useState('');
	const [job, setJob] = useState<Job>();
	const [candidatePickerOpen, setCandidatePickerOpen] = useState(false);
	const [assetPreview, setAssetPreview] = useState<ReviewAssetPreview>();
	const [assetArtworkInfo, setAssetArtworkInfo] = useState<{width: number; height: number}>();

  useEffect(() => {
    let active = true;
    let stopJobEvents: (() => void) | undefined;
    void (async () => {
      let next: Track[] = [];
      let persistedMatchItems: MatchItem[] | undefined;
      if (trackIds.length > 0) {
        next = (await resolveTracks({ids: trackIds})).tracks;
      } else if (apiReadMode === 'real' && matchJobId) {
        persistedMatchItems = await listMatchItems(matchJobId);
        const persistedIds = persistedMatchItems.map((item) => item.trackId);
        next = persistedIds.length > 0 ? (await resolveTracks({ids: persistedIds})).tracks : [];
      } else {
        // Mock review links historically opened the demo album when no explicit
        // selection was supplied. Keep that behavior without loading the full
        // library into the review page.
        next = (await resolveTracks({query: {q: '安泊猜想'}})).tracks;
      }
      let candidateOptionsByTrack = new Map<string, MatchCandidate[]>();
      let errorByTrack = new Map<string, string>();
      let matchStateByTrack = new Map<string, MatchItem['state']>();
      let selectedCandidateByTrack = new Map<string, string>();
	  let reviewFieldsByTrack = new Map<string, string[] | null | undefined>();
	  let reviewArtworkByTrack = new Map<string, boolean>();
      let reviewArtworkTouchedByTrack = new Map<string, boolean>();
	  let reviewArtworkMaxSizeByTrack = new Map<string, number>();
      if (apiReadMode === 'real') {
        const created = matchJobId ? await getJob(matchJobId) : await createMatchJob(next.map((track) => track.id));
        if (created) {
          if (active) setJob(created);
          // A newly queued batch belongs in the durable task center. Leave the
          // review route immediately so progress and cancellation are visible
          // instead of keeping the user on a waiting screen.
          if (!matchJobId && onJobQueued) {
            onJobQueued(created.id);
            return;
          }
          if (apiReadMode === 'real' && !matchJobId) {
            stopJobEvents = subscribeJobEvents(created.id, (next) => {
              if (active) setJob(next);
            });
          }
          const completed = matchJobId ? created : await waitForJob(created.id);
          stopJobEvents?.();
          stopJobEvents = undefined;
          if (active) setJob(completed);
          const matchItems = persistedMatchItems ?? await listMatchItems(created.id);
          if (matchJobId && trackIds.length === 0 && persistedMatchItems == null) {
            const persistedIds = matchItems.map((item) => item.trackId);
            next = persistedIds.length > 0 ? (await resolveTracks({ids: persistedIds})).tracks : [];
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
		  reviewArtworkTouchedByTrack = new Map(matchItems.map((item) => [item.trackId, item.reviewFields != null || Boolean(item.reviewArtwork) || (item.reviewArtworkMaxSize ?? 0) > 0] as const));
		  reviewArtworkMaxSizeByTrack = new Map(matchItems.map((item) => [item.trackId, item.reviewArtworkMaxSize ?? 0] as const));
        }
      }
      const reviewed = next.map((track) => {
        const candidates = apiReadMode === 'mock' ? candidatesFor(track) : (candidateOptionsByTrack.get(track.id) ?? []);
        const itemState = matchStateByTrack.get(track.id);
        const candidate = candidates.find((item) => item.id === selectedCandidateByTrack.get(track.id)) ?? candidates[0];
        const noMatch = !candidate || itemState === 'no_match' || itemState === 'failed';
        const persistedFields = reviewFieldsByTrack.get(track.id);
        const artworkTouched = reviewArtworkTouchedByTrack.get(track.id) ?? false;
        const defaultIncludeArtwork = Boolean(candidate?.hasArtwork && track.artworkCount === 0);
        return {
          track,
          candidate,
          candidates,
          fields: candidate ? (persistedFields == null ? changedFields(track, candidate) : persistedFields) : [],
	      artworkTouched,
          includeArtwork: artworkTouched ? (reviewArtworkByTrack.get(track.id) ?? false) : defaultIncludeArtwork,
		  artworkMaxSize: reviewArtworkMaxSizeByTrack.get(track.id) ?? 0,
          error: errorByTrack.get(track.id),
          state: noMatch || itemState === 'skipped' ? 'skipped' as const : itemState === 'accepted' ? 'accepted' as const : candidate && candidate.score >= 0.92 && availableFields(candidate).length > 0 ? 'accepted' as const : 'review' as const,
        };
      });
      if (!active) return;
      setItems(reviewed);
      setActiveId(reviewed[0]?.track.id);
      setLoading(false);
    })().catch(() => {
      if (active) setLoading(false);
    });
    return () => { active = false; stopJobEvents?.(); };
  }, [matchJobId, trackIds]);

  const sourceOptions = useMemo(() => {
    const providers = new Map<string, string>();
    items.forEach((item) => item.candidates.forEach((candidate) => providers.set(candidate.providerId, candidate.providerName)));
    return [...providers.entries()].sort((left, right) => left[1].localeCompare(right[1]));
  }, [items]);

  const visibleItems = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase();
    return items.filter(({track, candidates, state}) => {
      if (statusFilter !== 'all' && state !== statusFilter) return false;
      if (sourceFilter !== 'all' && !candidates.some((candidate) => candidate.providerId === sourceFilter)) return false;
      if (!normalized) return true;
      return [track.title, track.fileName, track.artists.join(' ')].some((value) => value.toLocaleLowerCase().includes(normalized));
    });
  }, [items, query, sourceFilter, statusFilter]);

  const hasQueueFilter = Boolean(query.trim()) || statusFilter !== 'all' || sourceFilter !== 'all';
  const active = visibleItems.find(({track}) => track.id === activeId) ?? visibleItems[0] ?? (hasQueueFilter ? undefined : items[0]);
  const accepted = items.filter((item) => item.state === 'accepted').length;
  const needsReview = items.filter((item) => item.state === 'review').length;
  const skipped = items.filter((item) => item.state === 'skipped').length;
	const activeAvailableFields = active?.candidate ? availableFields(active.candidate) : [];
	const allActiveFieldsSelected = activeAvailableFields.length > 0 && activeAvailableFields.every((field) => active.fields.includes(field));

	useEffect(() => {
	  setCandidatePickerOpen(false);
	  setAssetPreview(undefined);
	  setAssetArtworkInfo(undefined);
	}, [active?.track.id]);

	useEffect(() => {
	  if (!candidatePickerOpen && !assetPreview) return;
	  const onKeyDown = (event: KeyboardEvent) => {
	    if (event.key !== 'Escape') return;
	    setCandidatePickerOpen(false);
	    closeAssetPreview();
	  };
	  window.addEventListener('keydown', onKeyDown);
	  return () => window.removeEventListener('keydown', onKeyDown);
	}, [candidatePickerOpen, assetPreview]);

	const closeAssetPreview = () => {
	  setAssetPreview(undefined);
	  setAssetArtworkInfo(undefined);
	};

  const setItemState = (trackId: string, state: ReviewState) => {
    setItems((current) => current.map((item) => item.track.id === trackId ? {...item, state} : item));
  };

  const persistReviewState = (trackId: string, state: 'review' | 'accepted' | 'skipped', candidateId?: string, fields?: string[], artwork?: boolean, artworkMaxSize = 0) => {
    if (apiReadMode !== 'real' || !job) return;
    void updateMatchItem(job.id, trackId, state, candidateId, fields, artwork, artworkMaxSize).catch(() => undefined);
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
    persistReviewState(trackId, nextState, item.candidate?.id, nextFields, item.includeArtwork, item.includeArtwork ? item.artworkMaxSize : 0);
  };

  const toggleField = (trackId: string, field: string) => toggleFields(trackId, [field]);

	const toggleAllFields = (trackId: string) => {
	  const item = items.find((entry) => entry.track.id === trackId);
	  if (!item?.candidate) return;
	  const available = availableFields(item.candidate);
	  const next = new Set(item.fields);
	  const allSelected = available.length > 0 && available.every((field) => next.has(field));
	  if (allSelected) available.forEach((field) => next.delete(field));
	  else available.forEach((field) => next.add(field));
	  const nextFields = [...next];
	  const nextState = next.size === 0 && item.state === 'accepted' ? 'review' : item.state;
	  setItems((current) => current.map((entry) => entry.track.id === trackId ? {...entry, fields: nextFields, state: nextState} : entry));
	  persistReviewState(trackId, nextState, item.candidate.id, nextFields, item.includeArtwork, item.includeArtwork ? item.artworkMaxSize : 0);
	};

  const moveToNext = (trackId: string) => {
    const index = visibleItems.findIndex((item) => item.track.id === trackId);
    if (index < 0) return;
    const next = visibleItems.slice(index + 1).find((item) => item.state !== 'skipped') ?? visibleItems[index + 1] ?? visibleItems[0];
    if (next && next.track.id !== trackId) setActiveId(next.track.id);
  };

  const selectCandidate = (trackId: string, nextCandidate: MatchCandidate) => {
    const item = items.find((entry) => entry.track.id === trackId);
    if (!item) return;
    const includeArtwork = item.artworkTouched
      ? item.includeArtwork && nextCandidate.hasArtwork
      : Boolean(nextCandidate.hasArtwork && item.track.artworkCount === 0);
    const nextFields = changedFields(item.track, nextCandidate);
    setItems((current) => current.map((entry) => entry.track.id === trackId
      ? {...entry, candidate: nextCandidate, fields: nextFields, includeArtwork, state: 'review'}
      : entry));
	 persistReviewState(trackId, 'review', nextCandidate.id, nextFields, includeArtwork, includeArtwork ? item.artworkMaxSize : 0);
	setCandidatePickerOpen(false);
  };

  const toggleArtwork = (trackId: string) => {
    const item = items.find((entry) => entry.track.id === trackId);
    if (!item) return;
    const includeArtwork = !item.includeArtwork;
    const artworkMaxSize = includeArtwork ? item.artworkMaxSize : 0;
    setItems((current) => current.map((entry) => entry.track.id === trackId ? {...entry, includeArtwork, artworkTouched: true, artworkMaxSize} : entry));
    persistReviewState(trackId, item.state, item.candidate?.id, item.fields, includeArtwork, artworkMaxSize);
  };

  const setArtworkMaxSize = (trackId: string, artworkMaxSize: number) => {
    const item = items.find((entry) => entry.track.id === trackId);
    if (!item || !item.includeArtwork) return;
    setItems((current) => current.map((entry) => entry.track.id === trackId ? {...entry, artworkTouched: true, artworkMaxSize} : entry));
    persistReviewState(trackId, item.state, item.candidate?.id, item.fields, true, artworkMaxSize);
  };

  const rematchCurrent = async () => {
    if (!active || rematching) return;
    setRematching(true);
    setRematchError('');
    try {
      const result = await rematchMatchItem(job?.id ?? '', active.track.id, active.track);
      if (!result) return;
      const candidates = result.candidates ?? [];
      const candidate = candidates[0];
      setItems((current) => current.map((entry) => entry.track.id === active.track.id
        ? {
          ...entry,
          candidate,
          candidates,
          fields: candidate ? changedFields(active.track, candidate) : [],
          includeArtwork: Boolean(candidate?.hasArtwork && active.track.artworkCount === 0),
          artworkTouched: false,
          artworkMaxSize: 0,
          state: candidate ? 'review' : 'skipped',
          error: result.error,
        }
        : entry));
      setStatusFilter('all');
      setSourceFilter('all');
    } catch (error) {
      setRematchError(error instanceof Error ? error.message : '重新匹配失败');
    } finally {
      setRematching(false);
    }
  };

  const skipCurrent = () => {
    if (!active) return;
    setItemState(active.track.id, 'skipped');
    persistReviewState(active.track.id, 'skipped', active.candidate?.id, active.fields, active.includeArtwork, active.includeArtwork ? active.artworkMaxSize : 0);
    moveToNext(active.track.id);
  };

  const acceptCurrent = () => {
    if (!active?.candidate) return;
    setItemState(active.track.id, 'accepted');
    persistReviewState(active.track.id, 'accepted', active.candidate.id, active.fields, active.includeArtwork, active.includeArtwork ? active.artworkMaxSize : 0);
    moveToNext(active.track.id);
  };

  const confirmWrite = async () => {
    if (accepted === 0 || applying) return;
    setApplying(true);
    setWriteError('');
    try {
      if (apiReadMode === 'real' && job) {
        await createWriteJob(job.id, items.filter((item) => item.state === 'accepted' && item.candidate).map((item) => ({
          trackId: item.track.id, candidateId: item.candidate!.id, baseRevision: item.track.revision,
          fields: item.fields,
          artwork: item.includeArtwork,
          artworkMaxSize: item.includeArtwork ? item.artworkMaxSize : 0,
        })));
      } else {
        await new Promise((resolve) => window.setTimeout(resolve, 700));
      }
      onComplete();
    } catch (error) {
      setWriteError(error instanceof Error ? error.message : '写入任务创建失败');
    } finally {
      setApplying(false);
    }
  };

  if (loading) {
    const progress = job?.total ? Math.round((job.processed / job.total) * 100) : 0;
    return (
      <div className="review-loading">
        <LoaderCircle className="spin" size={24} />
        <div>
          <strong>正在抓取候选结果…</strong>
          <p>{job?.detail || '正在准备批量补全任务'}</p>
          {job && <><div className="review-loading-bar"><span style={{width: `${progress}%`}} /></div><small>{job.processed} / {job.total} 首 · {progress}%</small></>}
        </div>
      </div>
    );
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
        <label className="toolbar-filter">
          <span>状态</span>
          <select aria-label="审核状态筛选" value={statusFilter} onChange={(event) => setStatusFilter(event.target.value as ReviewStatusFilter)}>
            <option value="all">全部</option>
            <option value="review">待确认</option>
            <option value="accepted">已接受</option>
            <option value="skipped">已跳过</option>
          </select>
        </label>
        <label className="toolbar-filter">
          <span>来源</span>
          <select aria-label="候选来源筛选" value={sourceFilter} onChange={(event) => setSourceFilter(event.target.value)}>
            <option value="all">全部</option>
            {sourceOptions.map(([id, name]) => <option key={id} value={id}>{name}</option>)}
          </select>
        </label>
        <span />
        <div className="review-toolbar-actions">
          <button
            className="secondary-button"
            onClick={() => {
              const highConfidence = items.filter((item) => item.candidate && item.candidate.score >= 0.92);
              setItems((current) => current.map((item) => item.candidate && item.candidate.score >= 0.92 ? {...item, state: 'accepted'} : item));
              highConfidence.forEach((item) => persistReviewState(item.track.id, 'accepted', item.candidate?.id, item.fields, item.includeArtwork, item.includeArtwork ? item.artworkMaxSize : 0));
            }}
          >
            <Check size={15} /> 接受所有高置信
          </button>
          <button className="secondary-button" onClick={onBack} disabled={applying}>保存草稿并返回</button>
          <button className="primary-button" disabled={accepted === 0 || applying} onClick={() => void confirmWrite()}>
            {applying ? <LoaderCircle className="spin" size={15} /> : <FileCheck2 size={15} />}
            确认并创建写入任务
          </button>
          {writeError && <small className="review-write-error">{writeError}</small>}
        </div>
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
              <CoverArt title={item.track.title} artist={item.track.artists[0]} tone={item.track.coverTone} missing={!showGeneratedCovers && item.track.artworkCount === 0} imageUrl={artworkURL(item.track)} blankOnImageError={!showGeneratedCovers} size="xs" />
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
          {visibleItems.length === 0 && <div className="review-empty-filter">没有符合当前筛选条件的曲目</div>}
        </section>

        {active && active.candidate && (
          <section className="review-detail">
            <div className="review-detail-action-bar">
              <span><strong>当前曲目操作</strong><small>{active.state === 'accepted' ? '已接受，可继续调整字段' : active.state === 'skipped' ? '已跳过，可重新选择' : '确认字段后进入下一首'}</small></span>
              <div className="review-detail-actions">
                <button className="danger-quiet" onClick={skipCurrent}><X size={15} /> 跳过此曲</button>
                <button className="secondary-button" disabled={active.candidates.length < 2} onClick={() => setCandidatePickerOpen((value) => !value)}><ChevronRight size={15} /> 更换候选</button>
                <button className="primary-button" onClick={acceptCurrent}><Check size={15} /> 接受候选</button>
              </div>
            </div>
            <div className="review-detail-head">
              <div className="record-comparison">
                <CoverArt title={active.track.title} artist={active.track.artists[0]} tone={active.track.coverTone} missing={!showGeneratedCovers && active.track.artworkCount === 0} imageUrl={artworkURL(active.track)} blankOnImageError={!showGeneratedCovers} size="md" />
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
              <button onClick={() => void rematchCurrent()} disabled={rematching}>
                {rematching ? <LoaderCircle className="spin" size={14} /> : <RefreshCw size={14} />}
                重新匹配此曲
              </button>
            </div>
			{rematchError && <div className="review-rematch-error">{rematchError}</div>}

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
              {active.candidate.lyrics?.value && <ReviewDiff field="lyrics" label="内嵌歌词" current={active.track.lyrics ? '已有歌词' : '空'} next="来源提供歌词" source={active.candidate.providerName} checked={active.fields.includes('lyrics')} onToggle={() => toggleField(active.track.id, 'lyrics')} inspectLabel="查看歌词" onInspect={() => setAssetPreview({kind: 'lyrics', track: active.track, candidate: active.candidate!})} />}
              {active.candidate.hasArtwork && <ReviewDiff field="artwork" label="替换封面" current={active.track.artworkCount > 0 ? '已有封面' : '空'} next="来源提供封面" source={active.candidate.providerName} checked={active.includeArtwork} onToggle={() => toggleArtwork(active.track.id)} inspectLabel="查看封面" onInspect={() => setAssetPreview({kind: 'artwork', track: active.track, candidate: active.candidate!})} />}
            </div>
			<div className="review-fields-actions">
			  <span>已选择 {active.fields.filter((field) => activeAvailableFields.includes(field)).length} / {activeAvailableFields.length} 个可用字段</span>
			  <button
				type="button"
				aria-label={allActiveFieldsSelected ? '全部取消字段' : '全选字段'}
				disabled={activeAvailableFields.length === 0}
				onClick={() => toggleAllFields(active.track.id)}
			  >{allActiveFieldsSelected ? '全部取消' : '全选字段'}</button>
			</div>

            {active.candidate.hasArtwork && (
              <div className="review-artwork-size-control">
                <label className="review-artwork-size-select">
                  <span>封面写入尺寸</span>
                  <select
                    aria-label="审核封面写入尺寸"
                    value={active.artworkMaxSize}
                    disabled={!active.includeArtwork}
                    onChange={(event) => setArtworkMaxSize(active.track.id, Number(event.target.value))}
                  >
                    <option value={0}>保留原图</option>
                    <option value={1000}>居中裁剪至 1000×1000</option>
                    <option value={500}>居中裁剪至 500×500</option>
                  </select>
                </label>
                <small>请先在上方“替换封面”行勾选采用；小于目标尺寸的图片不会被放大。</small>
              </div>
            )}

          </section>
        )}
		{active && !active.candidate && (
          <section className="review-detail">
            <div className="empty-state">
              <span>∅</span>
              <strong>没有可审核的候选</strong>
              <p>{active.error || '当前启用的数据源没有返回匹配结果。该曲目已安全跳过，不会创建写入任务。'}</p>
              <button className="secondary-button" onClick={() => void rematchCurrent()} disabled={rematching}>
                {rematching ? <LoaderCircle className="spin" size={14} /> : <RefreshCw size={14} />}
                重新匹配此曲
              </button>
              {rematchError && <small className="review-rematch-error">{rematchError}</small>}
            </div>
          </section>
		)}
	      </div>

      {active && active.candidate && active.candidates.length > 1 && candidatePickerOpen && typeof document !== 'undefined' && createPortal(
		  <div className="candidate-picker-overlay">
			<button className="candidate-picker-backdrop" type="button" aria-label="关闭候选列表" onClick={() => setCandidatePickerOpen(false)} />
			<section className="candidate-picker-modal" role="dialog" aria-modal="true" aria-label="候选列表">
			  <header className="candidate-picker-modal-head">
				<div>
				  <span className="eyebrow">CHOOSE A MATCH</span>
				  <h3>选择候选来源</h3>
				  <p>{active.track.title} · {active.track.artists.join(' / ') || '未知歌手'}</p>
				</div>
				<button className="icon-button" type="button" title="关闭候选列表" aria-label="关闭候选列表" onClick={() => setCandidatePickerOpen(false)}><X size={17} /></button>
			  </header>
			  <div className="candidate-picker" role="listbox" aria-label="候选列表">
				{active.candidates.map((candidate) => (
				  <button
					key={candidate.id}
					role="option"
					aria-selected={candidate.id === active.candidate?.id}
					className={cn('candidate-picker-option', candidate.id === active.candidate?.id && 'is-selected')}
					onClick={() => selectCandidate(active.track.id, candidate)}
				  >
					<CoverArt title={candidate.title.value} artist={candidate.artists.value[0]} tone={candidate.coverTone} missing={!showGeneratedCovers && !candidateArtworkURL(candidate)} imageUrl={candidateArtworkURL(candidate)} blankOnImageError={!showGeneratedCovers} size="xs" />
					<span><strong>{candidate.title.value}</strong><small>{candidate.providerName} · {candidate.externalId}</small><em>{Math.round(candidate.score * 100)}% · {candidate.scoreLabel} · {candidateAssetSummary(candidate)}</em></span>
					{candidate.id === active.candidate?.id && <Check size={15} />}
				  </button>
				))}
			  </div>
			</section>
		  </div>,
		  document.body,
		)}

	      {assetPreview && typeof document !== 'undefined' && createPortal(
		  <div className="review-asset-overlay">
			<button className="review-asset-backdrop" type="button" aria-label="关闭详情预览" onClick={closeAssetPreview} />
			<section className="review-asset-modal" role="dialog" aria-modal="true" aria-label={assetPreview.kind === 'lyrics' ? '歌词详情' : '封面详情'}>
			  <header className="review-asset-head">
				<div>
				  <span className="eyebrow">{assetPreview.kind === 'lyrics' ? 'LYRICS PREVIEW' : 'ARTWORK PREVIEW'}</span>
				  <h3>{assetPreview.kind === 'lyrics' ? '候选歌词详情' : '候选封面详情'}</h3>
				  <p>{assetPreview.track.title} · {assetPreview.candidate.providerName} / {assetPreview.candidate.externalId}</p>
				</div>
				<button className="icon-button" type="button" title="关闭详情预览" aria-label="关闭详情预览" onClick={closeAssetPreview}><X size={17} /></button>
			  </header>
			  {assetPreview.kind === 'lyrics' ? (
				<div className="review-lyrics-preview">
				  <section><strong>当前内嵌歌词</strong><pre>{assetPreview.track.lyrics || '当前没有歌词'}</pre></section>
				  <section><strong>候选歌词 · {assetPreview.candidate.lyrics?.value.length ?? 0} 字符</strong><pre>{assetPreview.candidate.lyrics?.value || '来源没有返回歌词'}</pre></section>
				</div>
			  ) : (
				<div className="review-artwork-preview">
				  <div><strong>当前封面</strong><CoverArt title={assetPreview.track.title} artist={assetPreview.track.artists[0]} tone={assetPreview.track.coverTone} missing={!showGeneratedCovers && assetPreview.track.artworkCount === 0} imageUrl={artworkURL(assetPreview.track)} blankOnImageError={!showGeneratedCovers} size="lg" /></div>
				  <div>
					<strong>候选封面</strong>
					<CoverArt title={assetPreview.candidate.title.value} artist={assetPreview.candidate.artists.value[0]} tone={assetPreview.candidate.coverTone} missing={!showGeneratedCovers && !candidateArtworkURL(assetPreview.candidate)} imageUrl={candidateArtworkURL(assetPreview.candidate)} blankOnImageError={!showGeneratedCovers} size="lg" onImageInfo={setAssetArtworkInfo} />
					{assetArtworkInfo ? <small>{assetArtworkInfo.width} × {assetArtworkInfo.height}px</small> : <small>{candidateArtworkURL(assetPreview.candidate) ? '正在读取图片尺寸…' : '来源声明有封面，但当前没有可预览地址'}</small>}
				  </div>
				</div>
			  )}
			  <footer className="review-asset-foot"><span>详情预览不会自动写入文件。</span><button className="secondary-button" type="button" onClick={closeAssetPreview}>关闭</button></footer>
			</section>
		  </div>,
		  document.body,
		)}
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
  inspectLabel,
  onInspect,
}: {
  field: string;
  checked: boolean;
  onToggle: () => void;
  label: string;
  current: string;
  next: string;
  source: string;
  changed?: boolean;
  inspectLabel?: string;
  onInspect?: () => void;
}) {
  return (
    <div className={cn('review-diff-row', !checked && 'is-disabled')} data-field={field}>
      <button type="button" aria-label={`${checked ? '取消采用' : '采用'}${label}`} className={cn('square-check', checked && 'is-checked')} onClick={onToggle}>
        {checked && <Check size={12} strokeWidth={3} />}
      </button>
      <strong>{label}</strong>
      <span>{current}</span>
      <span className={cn(changed && 'is-changed')}>
        {next}
        {onInspect && <button type="button" className="review-diff-inspect" onClick={onInspect}>{inspectLabel || '查看详情'}</button>}
      </span>
      <em>{source}</em>
    </div>
  );
}
