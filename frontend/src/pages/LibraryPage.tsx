import {useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent} from 'react';
import {
  ArrowDownUp,
  Archive,
  FolderTree,
  LoaderCircle,
  MoreHorizontal,
  PanelRightOpen,
  RefreshCw,
  Search,
  SlidersHorizontal,
  Sparkles,
  Tags,
  Undo2,
  X,
} from 'lucide-react';
import {CandidateDrawer} from '@/components/library/CandidateDrawer';
import {BatchEditPanel, buildBatchPatch, type BatchOperation} from '@/components/library/BatchEditPanel';
import {OrganizePanel} from '@/components/library/OrganizePanel';
import {LibrarySidebar, type SidebarFilter} from '@/components/library/LibrarySidebar';
import {TrackInspector} from '@/components/library/TrackInspector';
import {TrackList} from '@/components/library/TrackList';
import {TagSnapshotPanel, trackToPatch, type SnapshotUpdate} from '@/components/library/TagSnapshotPanel';
import {cn} from '@/lib/utils';
import {
  apiReadMode,
  applyCandidateArtwork,
  createBatchEditJob,
	  createOrganizeJob,
	  getSystem,
	  listLibraries,
	  listTrackPage,
	  reconcileLibrary,
	  resolveTracks,
  rescanLibrary,
  rescanTrack,
  searchCandidates,
	  switchLibrary,
	  subscribeLibraryEvents,
  updateArtwork,
	updateTrack,
	waitForJob,
} from '@/api';
import {defaultBatchTrackLimit, type BatchArtworkInput, type CandidateSearchQuery, type LibrarySummary, type MatchCandidate, type RestoreDraftRequest, type Track, type TrackFormat, type TrackPatch, type TrackQuery, type TrackSort, type UpdateProvenance} from '@/types';
import type {StateSnapshot} from 'react-virtuoso';
import {clearLibraryViewSnapshot, getLibraryViewSnapshot, setLibraryViewSnapshot} from '@/pages/libraryViewCache';

interface LyricsSaveOptions {
  writeTag?: boolean;
}

interface LibraryPageProps {
  onOpenReview: (ids: string[]) => void;
  onOpenSettings?: () => void;
  onNotice: (message: string) => void;
  playerTrackId?: string;
  playerPlaying: boolean;
  onPlayTrack: (track: Track) => void;
  onTogglePlayer: () => void;
  showGeneratedCovers?: boolean;
  restoreDraft?: RestoreDraftRequest;
  onRestoreDraftConsumed?: () => void;
  onDiscardRestoreDraft?: () => void;
}

const filterLabels: Record<SidebarFilter, string> = {
  all: '全部音乐',
  complete: '资料完整',
  'tag-compatibility': '标签兼容问题',
  'missing-artwork': '缺少封面',
  'missing-lyrics': '缺少歌词',
  'needs-review': '需要确认',
  'parse-error': '解析失败',
  missing: '文件缺失',
};

type FormatFilter = 'all' | TrackFormat;
type SortMode = 'album' | 'title' | 'modified' | 'format';

const formatLabels: Record<FormatFilter, string> = {
  all: '全部格式',
  flac: 'FLAC 无损',
  mp3: 'MP3',
  wav: 'WAV',
  ogg: 'OGG / Opus',
};

const sortLabels: Record<SortMode, string> = {
  album: '专辑顺序',
  title: '标题顺序',
  modified: '最近修改',
  format: '格式顺序',
};

export const libraryBrowseStateKey = 'tagger-library-browse-state-v1';
export const librarySidebarWidthKey = 'tagger-library-sidebar-width-v1';
const minLibrarySidebarWidth = 210;
const maxLibrarySidebarWidth = 420;

function readLibrarySidebarWidth(): number | undefined {
  if (typeof localStorage === 'undefined') return undefined;
  const value = Number(localStorage.getItem(librarySidebarWidthKey));
  return Number.isFinite(value) && value >= minLibrarySidebarWidth && value <= maxLibrarySidebarWidth ? value : undefined;
}

function clampLibrarySidebarWidth(value: number): number {
  return Math.round(Math.max(minLibrarySidebarWidth, Math.min(maxLibrarySidebarWidth, value)));
}

interface LibraryBrowseState {
  folderId?: string;
  folderPath?: string;
  includeSubfolders?: boolean;
  search?: string;
  health?: SidebarFilter;
  format?: FormatFilter;
  sort?: SortMode;
  scrollTop?: number;
}

function readBrowseState(libraryId: string): LibraryBrowseState {
  if (typeof localStorage === 'undefined') return {};
  try {
    const payload = JSON.parse(localStorage.getItem(libraryBrowseStateKey) ?? '{}') as Record<string, LibraryBrowseState>;
    const value = payload?.[libraryId];
    return value && typeof value === 'object' ? value : {};
  } catch {
    return {};
  }
}

function writeBrowseState(libraryId: string, state: LibraryBrowseState): void {
  if (typeof localStorage === 'undefined') return;
  try {
    const payload = JSON.parse(localStorage.getItem(libraryBrowseStateKey) ?? '{}') as Record<string, LibraryBrowseState>;
    payload[libraryId] = state;
    localStorage.setItem(libraryBrowseStateKey, JSON.stringify(payload));
  } catch {
    // Private browsing or a full storage quota must not interrupt navigation.
  }
}

function clearBrowseFolder(libraryId: string): void {
  const state = readBrowseState(libraryId);
  writeBrowseState(libraryId, {...state, folderId: undefined, folderPath: undefined, includeSubfolders: false});
}

function trackQueryFromState(state: Pick<LibraryBrowseState, 'folderId' | 'folderPath' | 'includeSubfolders' | 'search' | 'health' | 'format' | 'sort'>): TrackQuery {
  return {
    q: state.search?.trim() || undefined,
    // Exact folder selections use the stable folder ID. Recursive selections
    // intentionally switch to the display path so the backend can match all
    // descendants without requiring a separate folder-tree query.
    folderId: state.includeSubfolders ? undefined : state.folderId || undefined,
    folderPath: state.folderId && !state.includeSubfolders ? undefined : state.folderPath || undefined,
    includeSubfolders: Boolean(state.includeSubfolders),
    health: state.health && state.health !== 'all' ? state.health : undefined,
    format: state.format && state.format !== 'all' ? state.format : undefined,
    sort: state.sort as TrackSort | undefined,
  };
}

function folderPathOf(folder: LibrarySummary['folders'][number]): string {
  if (typeof folder.path === 'string') return folder.path;
  return folder.id === 'folder-root' ? '' : folder.name.split(' · ').join('/');
}

function isTrackIndexed(track: Track | undefined | null): boolean {
  return Boolean(track && (!track.syncState || track.syncState === 'indexed'));
}

export function shouldPollLibrary(library: Pick<LibrarySummary, 'watchMode' | 'watchState'>): boolean {
  return library.watchMode === 'poll' || library.watchState === 'polling' || (library.watchState === 'degraded' && library.watchMode !== 'events');
}

function mergeTrackPages(current: Track[], incoming: Track[]): Track[] {
  const updates = new Map(incoming.map((track) => [track.id, track]));
  const merged = current.map((track) => updates.get(track.id) ?? track);
  const existing = new Set(current.map((track) => track.id));
  incoming.forEach((track) => {
    if (!existing.has(track.id)) merged.push(track);
  });
  return merged;
}

function trackOperationError(error: unknown, fallback: string): string {
  const code = error && typeof error === 'object' && 'code' in error ? String((error as {code?: unknown}).code ?? '') : '';
  if (code === 'track_selection_too_large' || (error instanceof Error && error.message === 'track_selection_too_large')) {
    return error instanceof Error && error.message !== 'track_selection_too_large'
      ? error.message
      : '当前结果超过单次批量曲目上限，请缩小搜索、目录或筛选范围后再操作';
  }
  return error instanceof Error ? error.message : fallback;
}

function tagWriteNotice(track: Track, success: string): string {
  if (track.tagIssues.length === 0) return success;
  return `标签已写入，但文件仍有 ${track.tagIssues.length} 项内嵌标签兼容问题，请继续复核`;
}

export function LibraryPage({onOpenReview, onOpenSettings, onNotice, playerTrackId, playerPlaying, onPlayTrack, onTogglePlayer, showGeneratedCovers = false, restoreDraft, onRestoreDraftConsumed, onDiscardRestoreDraft}: LibraryPageProps) {
  const pageSize = 100;
  const [library, setLibrary] = useState<LibrarySummary | null>(null);
  const [librarySidebarWidth, setLibrarySidebarWidth] = useState<number | undefined>(readLibrarySidebarWidth);
  const [resizingLibrarySidebar, setResizingLibrarySidebar] = useState(false);
  const [batchTrackLimit, setBatchTrackLimit] = useState(defaultBatchTrackLimit);
  const [libraries, setLibraries] = useState<LibrarySummary[]>([]);
  const [tracks, setTracks] = useState<Track[]>([]);
  const [pageTotal, setPageTotal] = useState(0);
  const [nextCursor, setNextCursor] = useState('');
  const [hasMore, setHasMore] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [scanning, setScanning] = useState(false);
  const [switchingLibraryId, setSwitchingLibraryId] = useState<string>();
  const [activeTrackId, setActiveTrackId] = useState<string>();
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [selectedDetails, setSelectedDetails] = useState<Map<string, Track>>(new Map());
  const [resultSelectionActive, setResultSelectionActive] = useState(false);
  const [activeFolder, setActiveFolder] = useState<string | null>(null);
  const [activeFolderPath, setActiveFolderPath] = useState<string | null>(null);
  const [activeFilter, setActiveFilter] = useState<SidebarFilter>('all');
  const [includeSubfolders, setIncludeSubfolders] = useState(false);
  const [search, setSearch] = useState('');
  const [formatFilter, setFormatFilter] = useState<FormatFilter>('all');
  const [sortMode, setSortMode] = useState<SortMode>('album');
  const [saving, setSaving] = useState(false);
  const [candidateOpen, setCandidateOpen] = useState(false);
  const [candidateLoading, setCandidateLoading] = useState(false);
  const [candidates, setCandidates] = useState<MatchCandidate[]>([]);
  const [candidateFocus, setCandidateFocus] = useState<'metadata' | 'lyrics'>('metadata');
  const [batchEditOpen, setBatchEditOpen] = useState(false);
  const [organizeOpen, setOrganizeOpen] = useState(false);
  const [mobileSidebar, setMobileSidebar] = useState(false);
  const [mobileInspector, setMobileInspector] = useState(false);
  const [filterMenuOpen, setFilterMenuOpen] = useState(false);
  const [snapshotOpen, setSnapshotOpen] = useState(false);
  const [snapshotUndo, setSnapshotUndo] = useState<{before: Track[]; afterRevisions: Map<string, string>; afterTracks: Map<string, Track>}>();
  const [restoreStateFrom, setRestoreStateFrom] = useState<StateSnapshot>();
  const [initialScrollTop, setInitialScrollTop] = useState<number>();
  const initializedRef = useRef(false);
  const hydratedQueryKeyRef = useRef<string | undefined>(undefined);
  const loadingMoreRef = useRef(false);
  const requestIDRef = useRef(0);
  const abortRef = useRef<AbortController | undefined>(undefined);
	  const viewportStateRef = useRef<StateSnapshot | undefined>(undefined);
	  const liveRefreshTimerRef = useRef<number | undefined>(undefined);
	  const reconcileInFlightRef = useRef(false);
	  const libraryGenerationRef = useRef(0);
	  const liveRefreshInFlightRef = useRef(false);
	  const liveRefreshQueuedRef = useRef(false);
  const viewRef = useRef<{
    library?: LibrarySummary;
    tracks: Track[];
    selectedDetails: Map<string, Track>;
    query: TrackQuery;
    nextCursor: string;
    hasMore: boolean;
    pageTotal: number;
    selectedIds: Set<string>;
    activeTrackId?: string;
  }>({tracks: [], selectedDetails: new Map(), query: {}, nextCursor: '', hasMore: false, pageTotal: 0, selectedIds: new Set()});

  const query = useMemo<TrackQuery>(() => trackQueryFromState({
    folderId: activeFolder ?? undefined,
    folderPath: activeFolderPath ?? undefined,
    includeSubfolders,
    search,
    health: activeFilter,
    format: formatFilter,
    sort: sortMode,
  }), [activeFilter, activeFolder, activeFolderPath, formatFilter, includeSubfolders, search, sortMode]);
  const queryKey = JSON.stringify(query);

  useEffect(() => {
    const applySidebarWidth = () => {
      if (window.innerWidth > 1180 && librarySidebarWidth) {
        document.documentElement.style.setProperty('--library-sidebar-width', `${librarySidebarWidth}px`);
      } else {
        document.documentElement.style.removeProperty('--library-sidebar-width');
      }
    };
    applySidebarWidth();
    window.addEventListener('resize', applySidebarWidth);
    return () => {
      window.removeEventListener('resize', applySidebarWidth);
      document.documentElement.style.removeProperty('--library-sidebar-width');
    };
  }, [librarySidebarWidth]);

  const persistLibrarySidebarWidth = (value: number) => {
    const next = clampLibrarySidebarWidth(value);
    setLibrarySidebarWidth(next);
    localStorage.setItem(librarySidebarWidthKey, String(next));
  };

  const resetLibrarySidebarWidth = () => {
    setLibrarySidebarWidth(undefined);
    localStorage.removeItem(librarySidebarWidthKey);
  };

  const resizeLibrarySidebar = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (window.innerWidth <= 1180) return;
    event.preventDefault();
    const startX = event.clientX;
    const startWidth = document.querySelector<HTMLElement>('.library-sidebar')?.getBoundingClientRect().width || librarySidebarWidth || 260;
    let nextWidth = clampLibrarySidebarWidth(startWidth);
    let moved = false;
    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    setResizingLibrarySidebar(true);
    const move = (moveEvent: PointerEvent) => {
      moved = moved || Math.abs(moveEvent.clientX - startX) >= 1;
      nextWidth = clampLibrarySidebarWidth(startWidth + moveEvent.clientX - startX);
      setLibrarySidebarWidth(nextWidth);
    };
    const stop = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', stop);
      window.removeEventListener('pointercancel', stop);
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
      setResizingLibrarySidebar(false);
      if (moved) persistLibrarySidebarWidth(nextWidth);
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', stop);
    window.addEventListener('pointercancel', stop);
  };

  const resizeLibrarySidebarWithKeyboard = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    event.preventDefault();
    const current = document.querySelector<HTMLElement>('.library-sidebar')?.getBoundingClientRect().width || librarySidebarWidth || 260;
    persistLibrarySidebarWidth(current + (event.key === 'ArrowLeft' ? -10 : 10));
  };

  viewRef.current = {
    library: library ?? undefined,
    tracks,
    selectedDetails,
    query,
    nextCursor,
    hasMore,
    pageTotal,
    selectedIds,
    activeTrackId,
  };

  const fetchPage = async (requestedQuery: TrackQuery, cursor = '', append = false, background = false, rebuild = false): Promise<void> => {
    if (append && loadingMoreRef.current) return;
    if (!append) {
      abortRef.current?.abort();
      loadingMoreRef.current = false;
      setLoadingMore(false);
    }
    const controller = new AbortController();
    abortRef.current = controller;
    const requestID = ++requestIDRef.current;
    if (append) {
      loadingMoreRef.current = true;
      setLoadingMore(true);
    }
    try {
      const page = await listTrackPage(requestedQuery, cursor, pageSize, controller.signal);
      if (requestID !== requestIDRef.current) return;
      if (background) {
        setTracks((current) => rebuild ? page.tracks : mergeTrackPages(current, page.tracks));
        setSelectedDetails((current) => {
          const next = new Map(current);
          page.tracks.forEach((track) => { if (next.has(track.id)) next.set(track.id, track); });
          return next;
        });
        setPageTotal(page.total);
        setNextCursor(page.nextCursor ?? '');
        setHasMore(page.hasMore);
        if (rebuild) {
          setActiveTrackId((current) => current && page.tracks.some((track) => track.id === current) ? current : page.tracks[0]?.id);
        }
        return;
      }
      setTracks((current) => append ? mergeTrackPages(current, page.tracks) : page.tracks);
      setPageTotal(page.total);
      setNextCursor(page.nextCursor ?? '');
      setHasMore(page.hasMore);
      if (!append) {
        setRestoreStateFrom(undefined);
        setActiveTrackId((current) => current && page.tracks.some((track) => track.id === current) ? current : page.tracks[0]?.id);
      }
    } catch (error) {
      if (controller.signal.aborted || (error instanceof DOMException && error.name === 'AbortError')) return;
      if (append && ((error as {code?: string})?.code === 'track_cursor_stale' || (error instanceof Error && error.message === 'track_cursor_stale'))) {
        // A scan or write invalidates opaque cursors. Keep the rows already on
        // screen while rebuilding the first page, then continue from its new
        // cursor instead of showing a blank list or jumping to a new route.
        loadingMoreRef.current = false;
        setLoadingMore(false);
        setNextCursor('');
        setHasMore(false);
        await fetchPage(requestedQuery, '', false, true, true);
        return;
      }
      throw error;
    } finally {
      if (requestID === requestIDRef.current && append) {
        loadingMoreRef.current = false;
        setLoadingMore(false);
      }
    }
  };

  const refreshLiveView = async (): Promise<void> => {
    const currentLibrary = viewRef.current.library;
	if (!currentLibrary) return;
	if (liveRefreshInFlightRef.current) {
	  liveRefreshQueuedRef.current = true;
	  return;
	}
    liveRefreshInFlightRef.current = true;
    try {
      const nextLibraries = await listLibraries();
      const nextLibrary = nextLibraries.find((item) => item.id === currentLibrary.id) ?? nextLibraries.find((item) => item.active);
      if (!nextLibrary || nextLibrary.id !== currentLibrary.id) return;
      setLibraries(nextLibraries.map((item) => ({...item, active: item.id === nextLibrary.id})));
      setLibrary({...nextLibrary, active: true});
	  const targetCount = Math.max(pageSize, viewRef.current.tracks.length);
	  const refreshed: Track[] = [];
	  let cursor = '';
	  let page = await listTrackPage(viewRef.current.query, '', pageSize);
	  refreshed.push(...page.tracks);
	  while (page.hasMore && page.nextCursor && refreshed.length < targetCount) {
		cursor = page.nextCursor;
		page = await listTrackPage(viewRef.current.query, cursor, pageSize);
		page.tracks.forEach((track) => {
		  if (!refreshed.some((item) => item.id === track.id)) refreshed.push(track);
		});
	  }
	  const pinnedIDs = [...new Set([...(viewRef.current.selectedIds ?? []), ...(viewRef.current.activeTrackId ? [viewRef.current.activeTrackId] : [])])].slice(0, batchTrackLimit);
	  let validPinned: Track[] = [];
	  let pinnedResolved = false;
	  if (pinnedIDs.length > 0) {
		try {
		  const resolved = await resolveTracks({ids: pinnedIDs});
		  pinnedResolved = true;
		  validPinned = resolved.tracks.filter((track) => !track.missing);
		  const existing = new Set(refreshed.map((track) => track.id));
		  validPinned.forEach((track) => { if (!existing.has(track.id) && track.id === viewRef.current.activeTrackId) refreshed.push(track); });
		} catch {
		  validPinned = [];
		}
	  }
	  setTracks(refreshed);
	  setPageTotal(page.total);
	  setNextCursor(page.nextCursor ?? '');
	  setHasMore(page.hasMore);
	  if (pinnedIDs.length > 0 && pinnedResolved) {
		const validSelected = validPinned.filter((track) => viewRef.current.selectedIds.has(track.id));
		setSelectedIds(new Set(validSelected.map((track) => track.id)));
		setSelectedDetails(new Map(validSelected.map((track) => [track.id, track])));
		setResultSelectionActive(validSelected.length > 0 && validSelected.length === page.total);
	  }
	  setActiveTrackId((current) => current && refreshed.some((track) => track.id === current) ? current : refreshed[0]?.id);
      clearLibraryViewSnapshot(currentLibrary.id);
    } finally {
      liveRefreshInFlightRef.current = false;
	  if (liveRefreshQueuedRef.current) {
		liveRefreshQueuedRef.current = false;
		window.setTimeout(() => void refreshLiveView().catch(() => undefined), 0);
	  }
    }
  };

  const scheduleLiveRefresh = () => {
    if (liveRefreshTimerRef.current !== undefined) window.clearTimeout(liveRefreshTimerRef.current);
    liveRefreshTimerRef.current = window.setTimeout(() => {
      liveRefreshTimerRef.current = undefined;
      void refreshLiveView().catch(() => undefined);
    }, 150);
  };

  const loadData = async (preserveSelection = false) => {
    setLoading(true);
    setLoadError('');
    initializedRef.current = false;
    if (preserveSelection && library?.id) clearLibraryViewSnapshot(library.id);
    try {
      const nextLibraries = await listLibraries();
      // An explicit false/absent active flag means the process is waiting for
      // the user to choose a root. Do not silently display an unrelated
      // previously indexed library as the active empty page.
      const nextLibrary = nextLibraries.find((item) => item.active);
      if (!nextLibrary) throw new Error('尚未配置音乐曲库');
      setLibraries(nextLibraries.map((item) => ({...item, active: item.id === nextLibrary.id})));
      setLibrary({...nextLibrary, active: true});

      const cached = preserveSelection ? undefined : getLibraryViewSnapshot(nextLibrary.id);
      const storedBrowseState = cached ? {} : (preserveSelection ? {} : readBrowseState(nextLibrary.id));
      const cachedQuery = cached?.query;
	      const requestedFolderID = (cachedQuery?.folderId ?? storedBrowseState.folderId ?? (preserveSelection ? activeFolder : undefined)) || '';
	      const nextFolder = requestedFolderID && nextLibrary.folders.some((folder) => folder.id === requestedFolderID) ? requestedFolderID : null;
	      const storedFolderPath = typeof storedBrowseState.folderPath === 'string' ? storedBrowseState.folderPath.trim() : '';
	      const requestedFolderPath = (cachedQuery?.folderPath ?? (preserveSelection ? activeFolderPath : storedFolderPath))?.trim() ?? '';
	      const exactPathFolder = requestedFolderPath
	        ? nextLibrary.folders.find((folder) => folderPathOf(folder) === requestedFolderPath || folder.name === requestedFolderPath)
	        : undefined;
	      const normalizedRequestedPath = exactPathFolder ? folderPathOf(exactPathFolder) : requestedFolderPath;
	      const validFolderPath = normalizedRequestedPath && nextLibrary.folders.some((folder) => {
	        const path = folderPathOf(folder);
	        return path === normalizedRequestedPath || path.startsWith(`${normalizedRequestedPath}/`);
	      }) ? normalizedRequestedPath : '';
	      const nextFolderPath = validFolderPath || (nextFolder ? folderPathOf(nextLibrary.folders.find((folder) => folder.id === nextFolder)!) : null);
      const nextIncludeSubfolders = cachedQuery?.includeSubfolders ?? (preserveSelection ? includeSubfolders : Boolean(storedBrowseState.includeSubfolders));
      const nextSearch = cachedQuery?.q ?? (preserveSelection ? search : storedBrowseState.search ?? '');
      const nextFilter = (cachedQuery?.health ?? (preserveSelection ? activeFilter : storedBrowseState.health ?? 'all')) as SidebarFilter;
      const nextFormat = (cachedQuery?.format ?? (preserveSelection ? formatFilter : storedBrowseState.format ?? 'all')) as FormatFilter;
      const nextSort = (cachedQuery?.sort ?? (preserveSelection ? sortMode : storedBrowseState.sort ?? 'album')) as SortMode;
      const requestedQuery = trackQueryFromState({folderId: nextFolder ?? undefined, folderPath: nextFolderPath ?? undefined, includeSubfolders: nextIncludeSubfolders, search: nextSearch, health: nextFilter, format: nextFormat, sort: nextSort});
      hydratedQueryKeyRef.current = JSON.stringify(requestedQuery);

      setActiveFolder(nextFolder);
      setActiveFolderPath(nextFolderPath);
      setIncludeSubfolders(nextIncludeSubfolders);
      setSearch(nextSearch);
      setActiveFilter(nextFilter);
      setFormatFilter(nextFormat);
      setSortMode(nextSort);
      setSelectedIds(cached ? new Set(cached.selectedIds) : new Set());
      setSelectedDetails(cached ? new Map(cached.loadedTracks.filter((track) => cached.selectedIds.includes(track.id)).map((track) => [track.id, track])) : new Map());
      setResultSelectionActive(Boolean(cached && cached.selectedIds.length > 0 && cached.selectedIds.length === cached.total));
      setRestoreStateFrom(cached?.virtuosoState);
      const storedScrollTop = typeof storedBrowseState.scrollTop === 'number' && Number.isFinite(storedBrowseState.scrollTop)
        ? Math.max(0, storedBrowseState.scrollTop)
        : undefined;
      setInitialScrollTop(cached ? undefined : storedScrollTop);

      if (cached) {
        setTracks(cached.loadedTracks);
        setPageTotal(cached.total);
        setNextCursor(cached.nextCursor ?? '');
        setHasMore(cached.hasMore);
        setActiveTrackId((current) => restoreDraft?.trackId && cached.loadedTracks.some((track) => track.id === restoreDraft.trackId)
          ? restoreDraft.trackId
          : cached.activeTrackId ?? current ?? cached.loadedTracks[0]?.id);
        setLoading(false);
        initializedRef.current = true;
        if (restoreDraft?.trackId && !cached.loadedTracks.some((track) => track.id === restoreDraft.trackId)) {
          void resolveTracks({ids: [restoreDraft.trackId]}).then((resolved) => {
            if (resolved.tracks.length === 0) return;
            setTracks((current) => mergeTrackPages(current, resolved.tracks));
            setActiveTrackId(restoreDraft.trackId);
          }).catch(() => undefined);
        }
        // Refresh only the first page in the background. Existing loaded pages
        // and the viewport remain visible while the index is revalidated.
        void fetchPage(requestedQuery, '', false, true).catch(() => undefined);
        return;
      }

      setTracks([]);
      setPageTotal(0);
      setNextCursor('');
      setHasMore(false);
      await fetchPage(requestedQuery);
      if (restoreDraft?.trackId) {
        try {
          const resolved = await resolveTracks({ids: [restoreDraft.trackId]});
          if (resolved.tracks.length > 0) {
            setTracks((current) => mergeTrackPages(current, resolved.tracks));
            setActiveTrackId(restoreDraft.trackId);
          }
        } catch {
          setActiveTrackId(restoreDraft.trackId);
        }
      }
      initializedRef.current = true;
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return;
      setLoadError(error instanceof Error ? error.message : '曲库加载失败');
    } finally {
      setLoading(false);
    }
  };

  const switchActiveLibrary = async (target: LibrarySummary) => {
    if (target.active || switchingLibraryId || !target.rootPath) return;
    setSwitchingLibraryId(target.id);
    try {
      const job = await switchLibrary(target.id, target.rootPath);
      if (job && apiReadMode === 'real') {
        const completed = await waitForJob(job.id);
        if (completed.state !== 'succeeded') throw new Error(completed.detail || `曲库切换${completed.state}`);
      }
      clearLibraryViewSnapshot(target.id);
      if (library?.id) clearLibraryViewSnapshot(library.id);
      await loadData();
      onNotice(`已切换到曲库：${target.name}`);
    } catch (error) {
      onNotice(error instanceof Error ? error.message : '曲库切换失败');
    } finally {
      setSwitchingLibraryId(undefined);
    }
  };

  useEffect(() => {
    void loadData();
    void getSystem().then((info) => {
      if (typeof info.batchTrackLimit === 'number') setBatchTrackLimit(info.batchTrackLimit);
    }).catch(() => undefined);
  }, []);

  useEffect(() => {
    if (apiReadMode === 'mock' || !library?.id) return;
    libraryGenerationRef.current = 0;
    return subscribeLibraryEvents(library.id, (event) => {
      if (event.libraryId !== library.id || event.generation <= libraryGenerationRef.current) return;
      libraryGenerationRef.current = event.generation;
      if (event.watchMode || event.watchState) {
        setLibrary((current) => current ? {
          ...current,
          watchMode: event.watchMode ?? current.watchMode,
          watchState: event.watchState ?? current.watchState,
        } : current);
      }
      if (event.kind !== 'snapshot') scheduleLiveRefresh();
    });
  }, [library?.id]);

  useEffect(() => {
    if (apiReadMode === 'mock' || !library?.id) return;
    let disposed = false;
    const reconcileCurrentDirectory = async () => {
      if (disposed || document.hidden || reconcileInFlightRef.current) return;
      reconcileInFlightRef.current = true;
      try {
        const result = await reconcileLibrary(library.id, activeFolderPath ?? '');
        if (!disposed && result.changed) scheduleLiveRefresh();
      } catch {
        // Watcher/SSE remain the primary path. A later focus or polling tick retries.
      } finally {
        reconcileInFlightRef.current = false;
      }
    };
    const onFocus = () => void reconcileCurrentDirectory();
    const onVisibility = () => { if (!document.hidden) void reconcileCurrentDirectory(); };
    void reconcileCurrentDirectory();
    window.addEventListener('focus', onFocus);
    document.addEventListener('visibilitychange', onVisibility);
	const shouldPoll = shouldPollLibrary(library);
    const timer = shouldPoll ? window.setInterval(() => void reconcileCurrentDirectory(), 5000) : undefined;
    return () => {
      disposed = true;
      window.removeEventListener('focus', onFocus);
      document.removeEventListener('visibilitychange', onVisibility);
      if (timer !== undefined) window.clearInterval(timer);
    };
  }, [library?.id, library?.watchMode, library?.watchState, activeFolderPath]);

  useEffect(() => {
    if (!initializedRef.current || hydratedQueryKeyRef.current === queryKey) return;
    const timer = window.setTimeout(() => {
      if (library?.id) clearLibraryViewSnapshot(library.id);
      setSelectedIds(new Set());
      setSelectedDetails(new Map());
      setResultSelectionActive(false);
      setRestoreStateFrom(undefined);
      setInitialScrollTop(undefined);
      setActiveTrackId(undefined);
      if (library) {
        writeBrowseState(library.id, {
          folderId: activeFolder ?? undefined,
          folderPath: activeFolderPath ?? undefined,
          includeSubfolders,
          search,
          health: activeFilter,
          format: formatFilter,
          sort: sortMode,
        });
      }
      hydratedQueryKeyRef.current = queryKey;
      void fetchPage(query).catch((error) => onNotice(trackOperationError(error, '曲目列表加载失败')));
    }, 250);
    return () => window.clearTimeout(timer);
  }, [queryKey]);

  useEffect(() => () => {
    abortRef.current?.abort();
	if (liveRefreshTimerRef.current !== undefined) window.clearTimeout(liveRefreshTimerRef.current);
    const current = viewRef.current;
    if (!current.library) return;
    const viewportState = viewportStateRef.current;
    setLibraryViewSnapshot({
      libraryId: current.library.id,
      query: current.query,
      loadedTracks: mergeTrackPages(current.tracks, [...current.selectedDetails.values()]),
      nextCursor: current.nextCursor || undefined,
      hasMore: current.hasMore,
      total: current.pageTotal,
      selectedIds: [...current.selectedIds],
      activeTrackId: current.activeTrackId,
      virtuosoState: viewportState,
      updatedAt: Date.now(),
    });
    writeBrowseState(current.library.id, {
      folderId: current.query.folderId,
      folderPath: current.query.folderPath,
      includeSubfolders: current.query.includeSubfolders,
      search: current.query.q,
      health: current.query.health ?? 'all',
      format: current.query.format ?? 'all',
      sort: current.query.sort as SortMode,
      scrollTop: viewportState?.scrollTop,
    });
  }, []);

  const runRescan = async () => {
    if (!library || scanning) return;
    setScanning(true);
    try {
	  const queued = await rescanLibrary(library.id);
	  const result = queued ? await waitForJob(queued.id) : null;
	  await loadData(true);
	  onNotice(result
		? result.state === 'succeeded' ? `扫描完成：索引 ${result.succeeded} 首曲目` : `扫描任务失败：${result.error || result.detail}`
		: 'Mock 扫描已完成');
    } catch (error) {
      onNotice(error instanceof Error ? error.message : '扫描失败');
    } finally {
      setScanning(false);
    }
  };

  const activeTrack = tracks.find((track) => track.id === activeTrackId) ?? null;
  const visibleTracks = tracks;
	const hasVisibleUnindexed = visibleTracks.some((track) => !isTrackIndexed(track));
  const selectedTracks = useMemo(() => {
    const loaded = new Map(tracks.map((track) => [track.id, track]));
    return [...selectedIds].map((id) => selectedDetails.get(id) ?? loaded.get(id)).filter((track): track is Track => Boolean(track));
  }, [selectedDetails, selectedIds, tracks]);
	const activeTrackIndexed = isTrackIndexed(activeTrack);
	const hasUnindexedSelection = selectedTracks.some((track) => !isTrackIndexed(track));
	const organizableTracks = selectedTracks.filter((track) => track.writable && isTrackIndexed(track));
  const snapshotTracks = selectedIds.size > 0 ? selectedTracks : visibleTracks;
  const hasActiveToolbarFilters = formatFilter !== 'all' || activeFilter !== 'all' || includeSubfolders || sortMode !== 'album';

  const updateTrackState = (updated: Track) => {
    setTracks((current) => current.map((item) => item.id === updated.id ? updated : item));
    setSelectedDetails((current) => {
      if (!current.has(updated.id)) return current;
      const next = new Map(current);
      next.set(updated.id, updated);
      return next;
    });
  };

  const loadMore = () => {
    if (!hasMore || !nextCursor || loadingMore) return;
    void fetchPage(query, nextCursor, true).catch((error) => onNotice(trackOperationError(error, '更多曲目加载失败')));
  };

  const saveTrack = async (
    patch: TrackPatch,
    options: LyricsSaveOptions = {},
    notice = apiReadMode === 'real' ? '标签已通过安全写入流程保存到音乐文件' : '标签草稿已写入 Mock 数据层',
	provenance?: UpdateProvenance,
	  ): Promise<Track | undefined> => {
		if (!activeTrack) return undefined;
		if (!activeTrackIndexed) {
		  onNotice('当前文件仍在索引，完成后才能编辑标签');
		  return undefined;
		}
    const writeTag = options.writeTag ?? true;
    if (!writeTag) {
      onNotice('未选择“写入音频标签”，文件未修改');
      return activeTrack;
    }
    setSaving(true);
    let updated = activeTrack;
	  try {
	  updated = await updateTrack(activeTrack.id, patch, provenance);
	  updateTrackState(updated);
	  if (restoreDraft?.trackId === updated.id) onRestoreDraftConsumed?.();
	  onNotice(tagWriteNotice(updated, notice));
	  return updated;
	} catch (error) {
	  onNotice(error instanceof Error ? error.message : '标签保存失败');
	  return undefined;
    } finally {
      setSaving(false);
    }
  };

		const applyCandidate = async (patch: TrackPatch, candidate: MatchCandidate, options: {artwork: boolean; artworkMaxSize?: number}) => {
		  if (!activeTrack) return;
		  if (!activeTrackIndexed) {
			onNotice('当前文件仍在索引，完成后才能应用候选资料');
			return;
		  }
	  setSaving(true);
	  let tagsApplied = false;
	  try {
		let updated = await updateTrack(activeTrack.id, patch, {providerId: candidate.providerId});
		tagsApplied = true;
		updateTrackState(updated);
		if (options.artwork) {
			  updated = await applyCandidateArtwork(updated.id, candidate.artworkRefId || candidate.id, options.artworkMaxSize ?? 0);
		  updateTrackState(updated);
		}
		onNotice(tagWriteNotice(updated, `已采用 ${candidate.providerName} 候选并安全写入${options.artwork ? '标签与封面' : '音乐标签'}`));
	  } catch (error) {
		const message = error instanceof Error ? error.message : '候选资料应用失败';
		onNotice(tagsApplied && options.artwork ? `标签已写入，但候选封面应用失败：${message}` : message);
	  } finally {
		setSaving(false);
	  }
	};

	  const openCandidateSearch = async (focus: 'metadata' | 'lyrics' = 'metadata', query?: CandidateSearchQuery) => {
	    if (!activeTrack) return;
	    if (!activeTrackIndexed) {
	      onNotice('当前文件仍在索引，完成后才能抓取元数据');
	      return;
	    }
    setCandidateFocus(focus);
    setCandidateOpen(true);
    setCandidateLoading(true);
    setCandidates([]);
    try {
      const nextCandidates = await searchCandidates(activeTrack, query);
      if (focus === 'lyrics') {
        // Lyrics search is an asset lookup, so records that actually contain
        // lyrics should be immediately visible even when a metadata-only
        // provider scored a little higher on title similarity.
        nextCandidates.sort((left, right) => Number(right.hasLyrics) - Number(left.hasLyrics) || right.score - left.score);
      }
      setCandidates(nextCandidates);
    } finally {
      setCandidateLoading(false);
    }
  };

		const changeArtwork = async (file: File | null, maxSize = 0) => {
		  if (!activeTrack) return;
		  if (!activeTrackIndexed) {
			onNotice('当前文件仍在索引，完成后才能修改封面');
			return;
		  }
	  setSaving(true);
	  try {
		const updated = await updateArtwork(activeTrack.id, file, maxSize);
		updateTrackState(updated);
		onNotice(file ? '封面已验证并安全写入音乐文件' : '当前封面已安全删除并记录历史');
	  } catch (error) {
		onNotice(error instanceof Error ? error.message : '封面操作失败');
	  } finally {
		setSaving(false);
	  }
	};

	const refreshActiveTrack = async () => {
		if (!activeTrack || saving) return;
		setSaving(true);
		try {
			const updated = await rescanTrack(activeTrack.id);
			updateTrackState(updated);
			onNotice('已重新读取当前音乐文件的标签、封面和技术信息');
		} catch (error) {
			onNotice(error instanceof Error ? error.message : '单曲扫描失败');
		} finally {
			setSaving(false);
		}
	};

  const toggleTrack = (id: string) => {
    const currentlySelected = selectedIds.has(id);
    const track = tracks.find((item) => item.id === id);
    setSelectedIds((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
    setSelectedDetails((details) => {
      const next = new Map(details);
      if (currentlySelected) next.delete(id);
      else if (track) next.set(id, track);
      return next;
    });
    setResultSelectionActive(false);
  };

  const toggleAll = async () => {
    if (resultSelectionActive || (pageTotal > 0 && selectedIds.size === pageTotal)) {
      setSelectedIds(new Set());
      setSelectedDetails(new Map());
      setResultSelectionActive(false);
      return;
    }
    try {
      const resolved = await resolveTracks({query});
      const nextIDs = new Set(resolved.tracks.map((track) => track.id));
      setSelectedIds(nextIDs);
      setSelectedDetails(new Map(resolved.tracks.map((track) => [track.id, track])));
      setResultSelectionActive(resolved.total > 0);
      onNotice(`已选择当前结果集 ${resolved.total} 首曲目`);
    } catch (error) {
      onNotice(trackOperationError(error, '全选当前结果集失败'));
    }
  };

  const openReviewSelection = async () => {
    if (selectedIds.size > batchTrackLimit) {
	  onNotice(`当前选择超过 ${batchTrackLimit} 首，请缩小范围后再批量补全`);
      return;
    }
	    try {
	      const resolved = selectedIds.size > 0 ? await resolveTracks({ids: [...selectedIds]}) : await resolveTracks({query});
	      if (resolved.tracks.length === 0) {
        onNotice('当前筛选没有可补全的曲目');
	        return;
	      }
	      if (resolved.tracks.some((track) => !isTrackIndexed(track))) {
	        onNotice('选择中包含正在索引的文件，请等待索引完成后再批量补全');
	        return;
	      }
      setSelectedIds(new Set(resolved.tracks.map((track) => track.id)));
      setSelectedDetails(new Map(resolved.tracks.map((track) => [track.id, track])));
      onOpenReview(resolved.tracks.map((track) => track.id));
    } catch (error) {
      onNotice(trackOperationError(error, '批量补全曲目解析失败'));
    }
  };

  const applyBatchEdit = async (operations: BatchOperation[], sequenceTracks: boolean, artwork?: BatchArtworkInput) => {
	    // Match the drawer's "应用到 N 首" contract: tracks already known to
	    // be read-only are excluded, while the backend still performs an
	    // authoritative effective-permission preflight before enqueueing.
	    const selectedTracksForJob = selectedTracks.filter((track) => track.writable);
	    if (selectedTracksForJob.some((track) => !isTrackIndexed(track))) {
	      onNotice('选择中包含正在索引的文件，请等待完成后再批量编辑');
	      return;
	    }
    const failed = new Set<string>();
    let succeeded = 0;
    setSaving(true);
    if (apiReadMode === 'real') {
      try {
		const job = await createBatchEditJob(selectedTracksForJob.map((track) => ({trackId: track.id, baseRevision: track.revision})), operations, sequenceTracks, artwork);
        setBatchEditOpen(false);
        setSelectedIds(new Set());
        onNotice(job ? `批量编辑任务已创建：${job.id}` : '批量编辑任务已创建');
        if (job) {
	          void waitForJob(job.id).then(async (completed) => {
	            await loadData(true);
            onNotice(completed.state === 'succeeded'
              ? `批量编辑已完成，曲库已刷新${artwork ? '封面尺寸' : '标签'}`
              : `批量编辑结束：${completed.detail || completed.state}`);
          }).catch((error) => onNotice(error instanceof Error ? `批量编辑完成后刷新失败：${error.message}` : '批量编辑完成后刷新失败'));
        }
      } catch (error) {
        onNotice(error instanceof Error ? error.message : '批量编辑任务创建失败');
      } finally {
        setSaving(false);
      }
      return;
    }
    try {
	      for (const [index, track] of selectedTracksForJob.entries()) {
        if (!track.writable) {
          failed.add(track.id);
          continue;
        }
		try {
		  let updated = track;
		  if (operations.length > 0 || sequenceTracks) {
			  updated = await updateTrack(track.id, buildBatchPatch(track, operations, sequenceTracks ? {index, total: selectedTracksForJob.length} : undefined));
		  }
		  if (artwork) {
			updated = await updateArtwork(track.id, artwork.action === 'delete' ? null : artwork.file ?? null, artwork.maxSize ?? 0);
		  }
		  updateTrackState(updated);
          succeeded += 1;
        } catch {
          failed.add(track.id);
        }
      }
    } finally {
      setSaving(false);
    }
    setBatchEditOpen(false);
    setSelectedIds(failed);
	  onNotice(failed.size === 0
	    ? `已安全写入 ${succeeded} 首曲目的批量${artwork ? '标签与封面' : '标签'}修改`
	    : `已写入 ${succeeded} 首，${failed.size} 首失败并保留选择，请检查后重试`);
	};

  const applyOrganize = async () => {
    const selectedTracksForJob = selectedTracks.filter((track) => track.writable && isTrackIndexed(track));
    if (selectedTracksForJob.length === 0) {
      onNotice('没有可整理的可写曲目');
      return;
    }
    setSaving(true);
    try {
      const job = await createOrganizeJob(selectedTracksForJob.map((track) => ({trackId: track.id, baseRevision: track.revision})));
      setOrganizeOpen(false);
      setSelectedIds(new Set());
      setSelectedDetails(new Map());
      setResultSelectionActive(false);
      if (apiReadMode === 'real' && job) {
        onNotice(`文件整理任务已创建：${job.id}`);
        void waitForJob(job.id).then(async (completed) => {
          await loadData(true);
          onNotice(completed.state === 'succeeded'
            ? `文件整理已完成，共处理 ${completed.succeeded} 首`
            : `文件整理结束：${completed.detail || completed.state}`);
        }).catch((error) => onNotice(error instanceof Error ? `文件整理完成后刷新失败：${error.message}` : '文件整理完成后刷新失败'));
      } else {
        await loadData(true);
        onNotice('文件位置已整理');
      }
    } catch (error) {
      onNotice(error instanceof Error ? error.message : '文件整理任务创建失败');
    } finally {
      setSaving(false);
    }
  };

  const applySnapshot = async (updates: SnapshotUpdate[]) => {
    if (saving || updates.length === 0) return;
    setSaving(true);
    let nextTracks = selectedIds.size > 0 ? selectedTracks : tracks;
    const afterRevisions = new Map<string, string>();
    const afterTracks = new Map<string, Track>();
    let applied = 0;
    try {
      for (const update of updates) {
        const current = nextTracks.find((track) => track.id === update.track.id);
        if (!current) continue;
        const updated = await updateTrack(current.id, update.patch);
        nextTracks = nextTracks.map((track) => track.id === updated.id ? updated : track);
        afterRevisions.set(updated.id, updated.revision);
        afterTracks.set(updated.id, updated);
        updateTrackState(updated);
        applied += 1;
      }
      setSnapshotUndo({before: updates.map((update) => update.track), afterRevisions, afterTracks});
      setSnapshotOpen(false);
      onNotice(`已导入 ${applied} 首曲目的内嵌标签；可在曲库中撤销本次导入`);
    } catch (error) {
      onNotice(applied > 0
        ? `已写入 ${applied} 首，后续导入中断：${error instanceof Error ? error.message : '未知错误'}`
        : error instanceof Error ? error.message : '标签快照导入失败');
    } finally {
      setSaving(false);
    }
  };

  const undoSnapshot = async () => {
    if (!snapshotUndo || saving) return;
    const conflicted = [...snapshotUndo.afterRevisions.entries()].some(([id, revision]) => (selectedDetails.get(id) ?? tracks.find((track) => track.id === id) ?? snapshotUndo.afterTracks.get(id))?.revision !== revision);
    if (conflicted) {
      onNotice('撤销已停止：部分曲目在导入后又发生了其他修改，请通过修改历史逐曲恢复');
      return;
    }
    setSaving(true);
    let nextTracks = selectedIds.size > 0 ? selectedTracks : mergeTrackPages(tracks, [...snapshotUndo.afterTracks.values()]);
    let restored = 0;
    try {
      for (const before of snapshotUndo.before) {
        const current = nextTracks.find((track) => track.id === before.id);
        if (!current) continue;
        const updated = await updateTrack(current.id, trackToPatch(before));
        nextTracks = nextTracks.map((track) => track.id === updated.id ? updated : track);
        updateTrackState(updated);
        restored += 1;
      }
      setSnapshotUndo(undefined);
      onNotice(`已撤销 ${restored} 首曲目的快照导入`);
    } catch (error) {
      onNotice(`撤销已写入 ${restored} 首后中断：${error instanceof Error ? error.message : '未知错误'}`);
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="app-loading">
        <LoaderCircle className="spin" size={22} />
        <span>正在装载音乐档案…</span>
      </div>
    );
  }

  if (loadError || !library) {
    return (
      <div className="app-loading app-error-state">
        <strong>无法装载音乐档案</strong>
        <span>{loadError || '尚未配置音乐曲库'}</span>
        <button className="secondary-button" onClick={() => void loadData()}><RefreshCw size={15} /> 重试</button>
        {!library && onOpenSettings && <button className="primary-button" onClick={onOpenSettings}><FolderTree size={15} /> 打开设置添加曲库</button>}
      </div>
    );
  }

  const currentLabel = activeFolderPath
    ? activeFolderPath.split('/').join(' / ')
    : (activeFolder ? library.folders.find((folder) => folder.id === activeFolder)?.name : undefined)
      || filterLabels[activeFilter];

  return (
    <div className={cn('library-page', resizingLibrarySidebar && 'is-resizing-sidebar')}>
      <LibrarySidebar
        library={library}
        libraries={libraries}
        switchingLibraryId={switchingLibraryId}
        activeFolder={activeFolder}
        activeFolderPath={activeFolderPath}
        activeFilter={activeFilter}
        sourceLabel={apiReadMode === 'real' ? '真实索引' : 'Mock 模式'}
          indexedSizeBytes={tracks.reduce((total, track) => total + track.sizeBytes, 0)}
        mobileOpen={mobileSidebar}
        onCloseMobile={() => setMobileSidebar(false)}
        onSwitchLibrary={(target) => void switchActiveLibrary(target)}
        onRescan={() => void runRescan()}
        scanning={scanning}
        onOpenSettings={onOpenSettings}
        onSelectFolder={(id) => {
          setActiveFolder(id);
          const selectedFolder = id ? library.folders.find((folder) => folder.id === id) : undefined;
          const nextFolderPath = selectedFolder ? folderPathOf(selectedFolder) : null;
          setActiveFolderPath(nextFolderPath);
          setActiveFilter('all');
          setIncludeSubfolders(false);
          if (library) {
            if (id) writeBrowseState(library.id, {folderId: id, folderPath: nextFolderPath ?? undefined, includeSubfolders: false});
            else clearBrowseFolder(library.id);
          }
          setActiveTrackId(undefined);
          setMobileSidebar(false);
        }}
        onSelectFolderPath={(path) => {
          setActiveFolder(null);
          setActiveFolderPath(path);
          setActiveFilter('all');
          setIncludeSubfolders(false);
          if (library) writeBrowseState(library.id, {folderPath: path, includeSubfolders: false});
          setActiveTrackId(undefined);
          setMobileSidebar(false);
        }}
      />

      <div
        className="library-sidebar-resizer"
        role="separator"
        aria-label="调整目录栏宽度"
        aria-orientation="vertical"
        aria-valuemin={minLibrarySidebarWidth}
        aria-valuemax={maxLibrarySidebarWidth}
        aria-valuenow={librarySidebarWidth ?? 260}
        tabIndex={0}
        title="拖动调整目录栏宽度；双击恢复默认"
        onPointerDown={resizeLibrarySidebar}
        onKeyDown={resizeLibrarySidebarWithKeyboard}
        onDoubleClick={resetLibrarySidebarWidth}
      />

      <section className="library-workspace">
        <div className="workspace-titlebar">
          <button className="mobile-panel-button mobile-sidebar-button" title="打开目录" aria-label="打开目录" onClick={() => { setMobileSidebar(true); setMobileInspector(false); }}>
            <FolderTree size={18} />
          </button>
          <div>
            <div className="breadcrumb">
              <span>{library.name.toLocaleUpperCase()}</span>
              <i>/</i>
              <strong title={currentLabel}>{currentLabel}</strong>
            </div>
            <h1 title={currentLabel}>{currentLabel}</h1>
            <p>{pageTotal} 首匹配曲目 · 已加载 {visibleTracks.length} 首</p>
          </div>
          <div className="workspace-actions">
            <div className="workspace-actions-inline">
              <button className="secondary-button" disabled={scanning} onClick={() => void runRescan()}>
                <RefreshCw size={15} className={scanning ? 'spin' : undefined} /> {scanning ? '扫描中…' : '快速扫描'}
              </button>
              {snapshotUndo && <button className="secondary-button" disabled={saving} onClick={() => void undoSnapshot()}><Undo2 size={15} /> 撤销导入</button>}
			<button className="primary-button" disabled={pageTotal === 0 || hasVisibleUnindexed} title={hasVisibleUnindexed ? '索引完成后可批量补全' : undefined} onClick={() => void openReviewSelection()}>
                <Sparkles size={15} /> 批量补全
              </button>
            </div>
            <details className="workspace-actions-menu">
              <summary className="workspace-actions-trigger" aria-label="更多曲库操作" title="更多曲库操作">
                <MoreHorizontal size={18} /> <span>操作</span>
              </summary>
              <div className="workspace-actions-popover">
                <button className="secondary-button" disabled={scanning} onClick={(event) => { event.currentTarget.closest('details')?.removeAttribute('open'); void runRescan(); }}>
                  <RefreshCw size={15} className={scanning ? 'spin' : undefined} /> {scanning ? '扫描中…' : '快速扫描'}
                </button>
                {snapshotUndo && <button className="secondary-button" disabled={saving} onClick={(event) => { event.currentTarget.closest('details')?.removeAttribute('open'); void undoSnapshot(); }}><Undo2 size={15} /> 撤销导入</button>}
				<button className="primary-button" disabled={pageTotal === 0 || hasVisibleUnindexed} title={hasVisibleUnindexed ? '索引完成后可批量补全' : undefined} onClick={(event) => { event.currentTarget.closest('details')?.removeAttribute('open'); void openReviewSelection(); }}>
                  <Sparkles size={15} /> 批量补全
                </button>
              </div>
            </details>
          </div>
        </div>

        <div className="track-toolbar">
          <label className="search-box">
            <Search size={16} />
            <input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="搜索标题、艺术家、专辑或文件名…"
            />
            {search && <button title="清除搜索" onClick={() => setSearch('')}><X size={14} /></button>}
          </label>
          <div className="toolbar-spacer" />
          <div className={cn('track-filter-shell', filterMenuOpen && 'is-open')}>
            <button
              className={cn('track-filter-trigger', hasActiveToolbarFilters && 'has-active')}
              aria-expanded={filterMenuOpen}
              aria-controls="track-filter-controls"
              aria-label="打开曲目筛选"
              title="打开曲目筛选"
              onClick={() => setFilterMenuOpen((value) => !value)}
            >
              <SlidersHorizontal size={15} /> <span>筛选</span>
            </button>
            <div className="track-filter-controls" id="track-filter-controls">
			  <button className="toolbar-button" aria-label="打开标签快照" title={snapshotTracks.some((track) => !isTrackIndexed(track)) ? '索引完成后可使用标签快照' : '打开标签快照'} disabled={snapshotTracks.length === 0 || snapshotTracks.some((track) => !isTrackIndexed(track))} onClick={() => setSnapshotOpen(true)}><Archive size={15} /> 标签快照</button>
              <label className="toolbar-filter">
                <SlidersHorizontal size={15} />
                <span>格式</span>
                <select aria-label="曲目格式筛选" value={formatFilter} onChange={(event) => setFormatFilter(event.target.value as FormatFilter)}>
                  {Object.entries(formatLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
                </select>
              </label>
              <label className="toolbar-filter">
                <Tags size={15} />
                <span>状态</span>
                <select
                  aria-label="曲目状态筛选"
                  value={activeFilter}
                  onChange={(event) => {
                    const nextFilter = event.target.value as SidebarFilter;
                    setActiveFilter(nextFilter);
                    setActiveTrackId(undefined);
                  }}
                >
                  {Object.entries(filterLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
                </select>
              </label>
              <label className={cn('toolbar-toggle', includeSubfolders && 'is-checked', !activeFolder && !activeFolderPath && 'is-disabled')} title={activeFolder || activeFolderPath ? '同时显示当前目录下的所有子目录曲目' : '选择一个目录后可递归显示子目录曲目'}>
                <input
                  type="checkbox"
                  aria-label="包含子目录"
                  checked={includeSubfolders}
                  disabled={!activeFolder && !activeFolderPath}
                  onChange={(event) => {
                    const nextValue = event.target.checked;
                    setIncludeSubfolders(nextValue);
                    if (library && (activeFolder || activeFolderPath)) writeBrowseState(library.id, {folderId: activeFolder ?? undefined, folderPath: activeFolderPath ?? undefined, includeSubfolders: nextValue});
                    setActiveTrackId(undefined);
                  }}
                />
                <span>含子目录</span>
              </label>
              <label className="toolbar-filter">
                <ArrowDownUp size={15} />
                <span>排序</span>
                <select aria-label="曲目排序" value={sortMode} onChange={(event) => setSortMode(event.target.value as SortMode)}>
                  {Object.entries(sortLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
                </select>
              </label>
            </div>
          </div>
          <button className="mobile-panel-button mobile-inspector-button" title="打开曲目详情" aria-label="打开曲目详情" onClick={() => { setMobileInspector(true); setMobileSidebar(false); }}>
            <PanelRightOpen size={18} />
          </button>
        </div>

        <TrackList
          tracks={visibleTracks}
          showGeneratedCovers={showGeneratedCovers}
          activeTrackId={activeTrackId}
          selectedIds={selectedIds}
          onSelectTrack={(track) => {
            setActiveTrackId(track.id);
            setMobileInspector(true);
            setMobileSidebar(false);
          }}
          onToggleTrack={toggleTrack}
          onToggleAll={toggleAll}
          resultSelectionActive={resultSelectionActive || (pageTotal > 0 && selectedIds.size === pageTotal)}
          hasMore={hasMore}
          loadingMore={loadingMore}
          onEndReached={loadMore}
          restoreStateFrom={restoreStateFrom}
          initialScrollTop={initialScrollTop}
          onViewportState={(state) => { viewportStateRef.current = state; }}
        />

		<div className="workspace-foot">
		  <span>已加载 {visibleTracks.length} / 共 {pageTotal} 首</span>
		  <span><i className={cn('status-dot', library.watchState === 'healthy' ? 'healthy' : 'warning')} /> {library.watchState === 'polling' ? '目录轮询' : library.watchState === 'degraded' ? '监听降级' : '实时监听'}</span>
          <span>{apiReadMode === 'real' ? 'Go API · 安全写入' : 'Mock API · rev 0.1'}</span>
        </div>
      </section>

	      <TrackInspector
	        track={activeTrack}
	        saving={saving}
		indexing={Boolean(activeTrack && !activeTrackIndexed)}
		mobileOpen={mobileInspector}
		onCloseMobile={() => setMobileInspector(false)}
		onSearch={openCandidateSearch}
		onSave={async (patch, options) => { await saveTrack(patch, options, undefined, restoreDraft ? {restoreRevisionId: restoreDraft.revisionId} : undefined); }}
		onArtworkChange={changeArtwork}
		onRescan={refreshActiveTrack}
		playerTrackId={playerTrackId}
		playerPlaying={playerPlaying}
		onPlayTrack={onPlayTrack}
		onTogglePlayer={onTogglePlayer}
		onNotice={onNotice}
        showGeneratedCovers={showGeneratedCovers}
        restoreDraft={restoreDraft}
        onDiscardRestoreDraft={onDiscardRestoreDraft}
      />

      {selectedIds.size > 0 && (
	  <div className="selection-bar">
          <div className="selection-count">
            <strong>{selectedIds.size}</strong>
            <span>首已选择</span>
          </div>
          <span className="selection-divider" />
		  <button disabled={hasUnindexedSelection} title={hasUnindexedSelection ? '索引完成后可批量编辑' : undefined} onClick={() => setBatchEditOpen(true)}><Tags size={16} /> 批量编辑</button>
		  <button className="selection-organize" disabled={hasUnindexedSelection || organizableTracks.length === 0} title={hasUnindexedSelection ? '索引完成后可整理文件' : organizableTracks.length === 0 ? '没有可写曲目' : undefined} onClick={() => setOrganizeOpen(true)}><FolderTree size={16} /> 整理文件</button>
		  <button className="is-accent" disabled={hasUnindexedSelection} title={hasUnindexedSelection ? '索引完成后可抓取元数据' : undefined} onClick={() => void openReviewSelection()}>
            <Sparkles size={16} /> 抓取元数据
          </button>
          <button className="selection-clear" title="清除选择" onClick={() => { setSelectedIds(new Set()); setSelectedDetails(new Map()); setResultSelectionActive(false); }}><X size={18} /></button>
        </div>
      )}

      <CandidateDrawer
        open={candidateOpen}
        track={activeTrack}
        candidates={candidates}
        loading={candidateLoading}
        focus={candidateFocus}
		showGeneratedCovers={showGeneratedCovers}
		onSearchQuery={(query) => openCandidateSearch(candidateFocus, query)}
		onClose={() => setCandidateOpen(false)}
		onApply={(patch, candidate, options) => applyCandidate(patch, candidate, options)}
      />

      <BatchEditPanel
        open={batchEditOpen}
        tracks={selectedTracks}
        saving={saving}
        onClose={() => setBatchEditOpen(false)}
        onApply={applyBatchEdit}
      />

      <OrganizePanel
        open={organizeOpen}
        tracks={organizableTracks}
        saving={saving}
        onClose={() => setOrganizeOpen(false)}
        onApply={applyOrganize}
      />

      <TagSnapshotPanel
        open={snapshotOpen}
        tracks={snapshotTracks}
        saving={saving}
        onClose={() => setSnapshotOpen(false)}
        onApply={applySnapshot}
      />
    </div>
  );
}
