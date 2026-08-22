import {useEffect, useMemo, useState} from 'react';
import {
  ArrowDownUp,
  Archive,
  FolderTree,
  LoaderCircle,
  Menu,
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
import {LibrarySidebar, type SidebarFilter} from '@/components/library/LibrarySidebar';
import {TrackInspector} from '@/components/library/TrackInspector';
import {TrackList} from '@/components/library/TrackList';
import {TagSnapshotPanel, trackToPatch, type SnapshotUpdate} from '@/components/library/TagSnapshotPanel';
import {
  apiReadMode,
  applyCandidateArtwork,
  createBatchEditJob,
  listLibraries,
  listTracks,
  rescanLibrary,
  rescanTrack,
  searchCandidates,
  switchLibrary,
  updateArtwork,
	updateTrack,
	waitForJob,
} from '@/api';
import type {BatchArtworkInput, CandidateSearchQuery, LibrarySummary, MatchCandidate, Track, TrackFormat, TrackPatch, UpdateProvenance} from '@/types';

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
}

const filterLabels: Record<SidebarFilter, string> = {
  all: '全部音乐',
  complete: '资料完整',
  'missing-artwork': '缺少封面',
  'missing-lyrics': '缺少歌词',
  'needs-review': '需要确认',
  'parse-error': '解析失败',
};

type FormatFilter = 'all' | TrackFormat;
type SortMode = 'album' | 'title' | 'modified' | 'format';

const formatLabels: Record<FormatFilter, string> = {
  all: '全部格式',
  flac: 'FLAC 无损',
  mp3: 'MP3',
  wav: 'WAV',
};

const sortLabels: Record<SortMode, string> = {
  album: '专辑顺序',
  title: '标题顺序',
  modified: '最近修改',
  format: '格式顺序',
};

const trackCollator = new Intl.Collator('zh-Hans-CN', {numeric: true, sensitivity: 'base'});

export function LibraryPage({onOpenReview, onOpenSettings, onNotice, playerTrackId, playerPlaying, onPlayTrack, onTogglePlayer, showGeneratedCovers = false}: LibraryPageProps) {
  const [library, setLibrary] = useState<LibrarySummary | null>(null);
  const [libraries, setLibraries] = useState<LibrarySummary[]>([]);
  const [tracks, setTracks] = useState<Track[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [scanning, setScanning] = useState(false);
  const [switchingLibraryId, setSwitchingLibraryId] = useState<string>();
  const [activeTrackId, setActiveTrackId] = useState<string>();
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [activeFolder, setActiveFolder] = useState<string | null>(null);
  const [activeFilter, setActiveFilter] = useState<SidebarFilter>('all');
  const [search, setSearch] = useState('');
  const [formatFilter, setFormatFilter] = useState<FormatFilter>('all');
  const [sortMode, setSortMode] = useState<SortMode>('album');
  const [saving, setSaving] = useState(false);
  const [candidateOpen, setCandidateOpen] = useState(false);
  const [candidateLoading, setCandidateLoading] = useState(false);
  const [candidates, setCandidates] = useState<MatchCandidate[]>([]);
  const [candidateFocus, setCandidateFocus] = useState<'metadata' | 'lyrics'>('metadata');
  const [batchEditOpen, setBatchEditOpen] = useState(false);
  const [mobileSidebar, setMobileSidebar] = useState(false);
  const [mobileInspector, setMobileInspector] = useState(false);
  const [snapshotOpen, setSnapshotOpen] = useState(false);
  const [snapshotUndo, setSnapshotUndo] = useState<{before: Track[]; afterRevisions: Map<string, string>}>();

  const loadData = async (preserveSelection = false) => {
    setLoading(true);
    setLoadError('');
    try {
      const [nextLibraries, nextTracks] = await Promise.all([listLibraries(), listTracks()]);
      // An explicit false/absent active flag means the process is waiting for
      // the user to choose a root. Do not silently display an unrelated
      // previously indexed library as the active empty page.
      const nextLibrary = nextLibraries.find((item) => item.active);
      if (!nextLibrary) throw new Error('尚未配置音乐曲库');
      setLibraries(nextLibraries.map((item) => ({...item, active: item.id === nextLibrary.id})));
      setLibrary({...nextLibrary, active: true});
      setTracks(nextTracks);
      setActiveFolder((current) => preserveSelection && current && nextLibrary.folders.some((folder) => folder.id === current) ? current : null);
      if (!preserveSelection) {
        setActiveFilter('all');
        setSelectedIds(new Set());
      }
      setActiveTrackId((current) => preserveSelection && nextTracks.some((track) => track.id === current)
        ? current
        : nextTracks[0]?.id);
    } catch (error) {
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

  const counts = useMemo(() => {
    const next: Record<SidebarFilter, number> = {
      all: tracks.length,
      complete: 0,
      'missing-artwork': 0,
      'missing-lyrics': 0,
      'needs-review': 0,
      'parse-error': 0,
    };
    tracks.forEach((track) => {
      next[track.health] += 1;
    });
    return next;
  }, [tracks]);

  const visibleTracks = useMemo(() => {
    const query = search.trim().toLocaleLowerCase();
    const filtered = tracks.filter((track) => {
      if (activeFolder && track.folderId !== activeFolder) return false;
      if (!activeFolder && activeFilter !== 'all' && track.health !== activeFilter) return false;
      if (formatFilter !== 'all' && track.format !== formatFilter) return false;
      if (!query) return true;
      return [
        track.title,
        track.fileName,
        track.album,
        track.artists.join(' '),
        track.genres.join(' '),
      ].some((value) => value.toLocaleLowerCase().includes(query));
    });
    return [...filtered].sort((left, right) => compareTracks(left, right, sortMode));
  }, [activeFilter, activeFolder, formatFilter, search, sortMode, tracks]);

  const snapshotTracks = selectedIds.size > 0
    ? tracks.filter((track) => selectedIds.has(track.id))
    : visibleTracks;

  const saveTrack = async (
    patch: TrackPatch,
    options: LyricsSaveOptions = {},
    notice = apiReadMode === 'real' ? '标签已通过安全写入流程保存到音乐文件' : '标签草稿已写入 Mock 数据层',
	provenance?: UpdateProvenance,
  ): Promise<Track | undefined> => {
	if (!activeTrack) return undefined;
    const writeTag = options.writeTag ?? true;
    if (!writeTag) {
      onNotice('未选择“写入音频标签”，文件未修改');
      return activeTrack;
    }
    setSaving(true);
    let updated = activeTrack;
    try {
	  updated = await updateTrack(activeTrack.id, patch, provenance);
	  setTracks((current) => current.map((item) => item.id === updated.id ? updated : item));
	  onNotice(notice);
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
	  setSaving(true);
	  let tagsApplied = false;
	  try {
		let updated = await updateTrack(activeTrack.id, patch, {providerId: candidate.providerId});
		tagsApplied = true;
		setTracks((current) => current.map((item) => item.id === updated.id ? updated : item));
		if (options.artwork) {
		  updated = await applyCandidateArtwork(updated.id, candidate.id, options.artworkMaxSize ?? 0);
		  setTracks((current) => current.map((item) => item.id === updated.id ? updated : item));
		}
		onNotice(`已采用 ${candidate.providerName} 候选并安全写入${options.artwork ? '标签与封面' : '音乐标签'}`);
	  } catch (error) {
		const message = error instanceof Error ? error.message : '候选资料应用失败';
		onNotice(tagsApplied && options.artwork ? `标签已写入，但候选封面应用失败：${message}` : message);
	  } finally {
		setSaving(false);
	  }
	};

  const openCandidateSearch = async (focus: 'metadata' | 'lyrics' = 'metadata', query?: CandidateSearchQuery) => {
    if (!activeTrack) return;
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
	  setSaving(true);
	  try {
		const updated = await updateArtwork(activeTrack.id, file, maxSize);
		setTracks((current) => current.map((item) => item.id === updated.id ? updated : item));
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
			setTracks((current) => current.map((item) => item.id === updated.id ? updated : item));
			onNotice('已重新读取当前音乐文件的标签、封面和技术信息');
		} catch (error) {
			onNotice(error instanceof Error ? error.message : '单曲扫描失败');
		} finally {
			setSaving(false);
		}
	};

  const toggleTrack = (id: string) => {
    setSelectedIds((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const toggleAll = () => {
    setSelectedIds((current) => {
      const allSelected = visibleTracks.length > 0 && visibleTracks.every((track) => current.has(track.id));
      if (allSelected) {
        const next = new Set(current);
        visibleTracks.forEach((track) => next.delete(track.id));
        return next;
      }
      return new Set([...current, ...visibleTracks.map((track) => track.id)]);
    });
  };

	const applyBatchEdit = async (operations: BatchOperation[], sequenceTracks: boolean, artwork?: BatchArtworkInput) => {
    const selectedTracks = tracks.filter((track) => selectedIds.has(track.id));
    const failed = new Set<string>();
    let succeeded = 0;
    setSaving(true);
    if (apiReadMode === 'real') {
      try {
		const job = await createBatchEditJob(selectedTracks.map((track) => ({trackId: track.id, baseRevision: track.revision})), operations, sequenceTracks, artwork);
        setBatchEditOpen(false);
        setSelectedIds(new Set());
        onNotice(job ? `批量编辑任务已创建：${job.id}` : '批量编辑任务已创建');
        if (job) {
          void waitForJob(job.id).then(async (completed) => {
            const refreshed = await listTracks();
            setTracks(refreshed);
            setActiveTrackId((current) => current && refreshed.some((track) => track.id === current) ? current : refreshed[0]?.id);
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
      for (const [index, track] of selectedTracks.entries()) {
        if (!track.writable) {
          failed.add(track.id);
          continue;
        }
		try {
		  let updated = track;
		  if (operations.length > 0 || sequenceTracks) {
			updated = await updateTrack(track.id, buildBatchPatch(track, operations, sequenceTracks ? {index, total: selectedTracks.length} : undefined));
		  }
		  if (artwork) {
			updated = await updateArtwork(track.id, artwork.action === 'delete' ? null : artwork.file ?? null, artwork.maxSize ?? 0);
		  }
		  setTracks((current) => current.map((item) => item.id === updated.id ? updated : item));
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

  const applySnapshot = async (updates: SnapshotUpdate[]) => {
    if (saving || updates.length === 0) return;
    setSaving(true);
    let nextTracks = tracks;
    const afterRevisions = new Map<string, string>();
    let applied = 0;
    try {
      for (const update of updates) {
        const current = nextTracks.find((track) => track.id === update.track.id);
        if (!current) continue;
        const updated = await updateTrack(current.id, update.patch);
        nextTracks = nextTracks.map((track) => track.id === updated.id ? updated : track);
        afterRevisions.set(updated.id, updated.revision);
        applied += 1;
      }
      setTracks(nextTracks);
      setSnapshotUndo({before: updates.map((update) => update.track), afterRevisions});
      setSnapshotOpen(false);
      onNotice(`已导入 ${applied} 首曲目的内嵌标签；可在曲库中撤销本次导入`);
    } catch (error) {
      setTracks(nextTracks);
      onNotice(applied > 0
        ? `已写入 ${applied} 首，后续导入中断：${error instanceof Error ? error.message : '未知错误'}`
        : error instanceof Error ? error.message : '标签快照导入失败');
    } finally {
      setSaving(false);
    }
  };

  const undoSnapshot = async () => {
    if (!snapshotUndo || saving) return;
    const conflicted = [...snapshotUndo.afterRevisions.entries()].some(([id, revision]) => tracks.find((track) => track.id === id)?.revision !== revision);
    if (conflicted) {
      onNotice('撤销已停止：部分曲目在导入后又发生了其他修改，请通过修改历史逐曲恢复');
      return;
    }
    setSaving(true);
    let nextTracks = tracks;
    let restored = 0;
    try {
      for (const before of snapshotUndo.before) {
        const current = nextTracks.find((track) => track.id === before.id);
        if (!current) continue;
        const updated = await updateTrack(current.id, trackToPatch(before));
        nextTracks = nextTracks.map((track) => track.id === updated.id ? updated : track);
        restored += 1;
      }
      setTracks(nextTracks);
      setSnapshotUndo(undefined);
      onNotice(`已撤销 ${restored} 首曲目的快照导入`);
    } catch (error) {
      setTracks(nextTracks);
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

  const currentLabel = activeFolder
    ? library.folders.find((folder) => folder.id === activeFolder)?.name
    : filterLabels[activeFilter];

  return (
    <div className="library-page">
      <LibrarySidebar
        library={library}
        libraries={libraries}
        switchingLibraryId={switchingLibraryId}
        activeFolder={activeFolder}
        activeFilter={activeFilter}
        counts={counts}
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
          setActiveFilter('all');
          setActiveTrackId((id ? tracks.find((track) => track.folderId === id) : tracks[0])?.id);
          setMobileSidebar(false);
        }}
        onSelectFilter={(filter) => {
          setActiveFilter(filter);
          setActiveTrackId((filter === 'all' ? tracks[0] : tracks.find((track) => track.health === filter))?.id);
          setMobileSidebar(false);
        }}
      />

      <section className="library-workspace">
        <div className="workspace-titlebar">
          <button className="mobile-panel-button" title="打开目录" onClick={() => setMobileSidebar(true)}>
            <FolderTree size={18} />
          </button>
          <div>
            <div className="breadcrumb">
              <span>{library.name.toLocaleUpperCase()}</span>
              <i>/</i>
              <strong>{currentLabel}</strong>
            </div>
            <h1>{currentLabel}</h1>
            <p>{visibleTracks.length} 首曲目 · {visibleTracks.filter((track) => track.format === 'flac').length} 首无损音频</p>
          </div>
          <div className="workspace-actions">
            <button className="secondary-button" disabled={scanning} onClick={() => void runRescan()}>
              <RefreshCw size={15} className={scanning ? 'spin' : undefined} /> {scanning ? '扫描中…' : '快速扫描'}
            </button>
            {snapshotUndo && <button className="secondary-button" disabled={saving} onClick={() => void undoSnapshot()}><Undo2 size={15} /> 撤销导入</button>}
            <button className="primary-button" onClick={() => onOpenReview(visibleTracks.map((track) => track.id))}>
              <Sparkles size={15} /> 批量补全
            </button>
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
            <kbd>⌘ K</kbd>
          </label>
          <div className="toolbar-spacer" />
          <button className="toolbar-button" disabled={snapshotTracks.length === 0} onClick={() => setSnapshotOpen(true)}><Archive size={15} /> 标签快照</button>
          <label className="toolbar-filter">
            <SlidersHorizontal size={15} />
            <span>筛选</span>
            <select aria-label="曲目格式筛选" value={formatFilter} onChange={(event) => setFormatFilter(event.target.value as FormatFilter)}>
              {Object.entries(formatLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </select>
          </label>
          <label className="toolbar-filter">
            <ArrowDownUp size={15} />
            <span>排序</span>
            <select aria-label="曲目排序" value={sortMode} onChange={(event) => setSortMode(event.target.value as SortMode)}>
              {Object.entries(sortLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </select>
          </label>
          <button className="mobile-panel-button" title="打开曲目详情" onClick={() => setMobileInspector(true)}>
            <Menu size={18} />
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
          }}
          onToggleTrack={toggleTrack}
          onToggleAll={toggleAll}
        />

        <div className="workspace-foot">
          <span>显示 {visibleTracks.length} / {tracks.length} 首</span>
          <span><i className="status-dot healthy" /> 索引健康</span>
          <span>{apiReadMode === 'real' ? 'Go API · 安全写入' : 'Mock API · rev 0.1'}</span>
        </div>
      </section>

      <TrackInspector
        track={activeTrack}
        saving={saving}
		mobileOpen={mobileInspector}
		onCloseMobile={() => setMobileInspector(false)}
		onSearch={openCandidateSearch}
		onSave={async (patch, options) => { await saveTrack(patch, options); }}
		onArtworkChange={changeArtwork}
		onRescan={refreshActiveTrack}
		playerTrackId={playerTrackId}
		playerPlaying={playerPlaying}
		onPlayTrack={onPlayTrack}
		onTogglePlayer={onTogglePlayer}
		onNotice={onNotice}
		showGeneratedCovers={showGeneratedCovers}
      />

      {selectedIds.size > 0 && (
        <div className="selection-bar">
          <div className="selection-count">
            <strong>{selectedIds.size}</strong>
            <span>首已选择</span>
          </div>
          <span className="selection-divider" />
          <button onClick={() => setBatchEditOpen(true)}><Tags size={16} /> 批量编辑</button>
          <button className="is-accent" onClick={() => onOpenReview(Array.from(selectedIds))}>
            <Sparkles size={16} /> 抓取元数据
          </button>
          <button className="selection-clear" title="清除选择" onClick={() => setSelectedIds(new Set())}><X size={18} /></button>
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
        tracks={tracks.filter((track) => selectedIds.has(track.id))}
        saving={saving}
        onClose={() => setBatchEditOpen(false)}
        onApply={applyBatchEdit}
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

function compareTracks(left: Track, right: Track, mode: SortMode): number {
  if (mode === 'title') {
    return trackCollator.compare(left.title || left.fileName, right.title || right.fileName);
  }
  if (mode === 'modified') {
    return right.modifiedAt.localeCompare(left.modifiedAt) || trackCollator.compare(left.relativePath, right.relativePath);
  }
  if (mode === 'format') {
    return trackCollator.compare(left.format, right.format) || trackCollator.compare(left.title, right.title);
  }
  return trackCollator.compare(left.album, right.album)
    || (left.discNumber ?? 0) - (right.discNumber ?? 0)
    || (left.trackNumber ?? 0) - (right.trackNumber ?? 0)
    || trackCollator.compare(left.title || left.fileName, right.title || right.fileName);
}
