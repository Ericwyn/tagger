import {useEffect, useMemo, useState} from 'react';
import {
  ArrowDownUp,
  ChevronDown,
  FolderTree,
  LoaderCircle,
  Menu,
  RefreshCw,
  Search,
  SlidersHorizontal,
  Sparkles,
  Tags,
  X,
} from 'lucide-react';
import {CandidateDrawer} from '@/components/library/CandidateDrawer';
import {BatchEditPanel, buildBatchPatch, type BatchOperation} from '@/components/library/BatchEditPanel';
import {LibrarySidebar, type SidebarFilter} from '@/components/library/LibrarySidebar';
import {TrackInspector} from '@/components/library/TrackInspector';
import {TrackList} from '@/components/library/TrackList';
import {
  apiReadMode,
  applyCandidateArtwork,
  getLibrary,
  listTracks,
  rescanLibrary,
  searchCandidates,
  updateArtwork,
  updateTrack,
	waitForJob,
} from '@/api';
import type {LibrarySummary, MatchCandidate, Track, TrackPatch, UpdateProvenance} from '@/types';

interface LibraryPageProps {
  onOpenReview: (ids: string[]) => void;
  onNotice: (message: string) => void;
}

const filterLabels: Record<SidebarFilter, string> = {
  all: '全部音乐',
  complete: '资料完整',
  'missing-artwork': '缺少封面',
  'missing-lyrics': '缺少歌词',
  'needs-review': '需要确认',
  'parse-error': '解析失败',
};

export function LibraryPage({onOpenReview, onNotice}: LibraryPageProps) {
  const [library, setLibrary] = useState<LibrarySummary | null>(null);
  const [tracks, setTracks] = useState<Track[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [scanning, setScanning] = useState(false);
  const [activeTrackId, setActiveTrackId] = useState<string>();
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [activeFolder, setActiveFolder] = useState<string | null>(null);
  const [activeFilter, setActiveFilter] = useState<SidebarFilter>('all');
  const [search, setSearch] = useState('');
  const [saving, setSaving] = useState(false);
  const [candidateOpen, setCandidateOpen] = useState(false);
  const [candidateLoading, setCandidateLoading] = useState(false);
  const [candidates, setCandidates] = useState<MatchCandidate[]>([]);
  const [batchEditOpen, setBatchEditOpen] = useState(false);
  const [mobileSidebar, setMobileSidebar] = useState(false);
  const [mobileInspector, setMobileInspector] = useState(false);

  const loadData = async (preserveSelection = false) => {
    setLoading(true);
    setLoadError('');
    try {
      const [nextLibrary, nextTracks] = await Promise.all([getLibrary(), listTracks()]);
      setLibrary(nextLibrary);
      setTracks(nextTracks);
      setActiveTrackId((current) => preserveSelection && nextTracks.some((track) => track.id === current)
        ? current
        : nextTracks[0]?.id);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : '曲库加载失败');
    } finally {
      setLoading(false);
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
    return tracks.filter((track) => {
      if (activeFolder && track.folderId !== activeFolder) return false;
      if (!activeFolder && activeFilter !== 'all' && track.health !== activeFilter) return false;
      if (!query) return true;
      return [
        track.title,
        track.fileName,
        track.album,
        track.artists.join(' '),
        track.genres.join(' '),
      ].some((value) => value.toLocaleLowerCase().includes(query));
    });
  }, [activeFilter, activeFolder, search, tracks]);

  const saveTrack = async (
    patch: TrackPatch,
    notice = apiReadMode === 'real' ? '标签已通过安全写入流程保存到音乐文件' : '标签草稿已写入 Mock 数据层',
	provenance?: UpdateProvenance,
  ): Promise<Track | undefined> => {
	if (!activeTrack) return undefined;
    setSaving(true);
    try {
      const updated = await updateTrack(activeTrack.id, patch, provenance);
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

	const applyCandidate = async (patch: TrackPatch, candidate: MatchCandidate, includeArtwork: boolean) => {
	  if (!activeTrack) return;
	  setSaving(true);
	  let tagsApplied = false;
	  try {
		let updated = await updateTrack(activeTrack.id, patch, {providerId: candidate.providerId});
		tagsApplied = true;
		setTracks((current) => current.map((item) => item.id === updated.id ? updated : item));
		if (includeArtwork) {
		  updated = await applyCandidateArtwork(updated.id, candidate.id);
		  setTracks((current) => current.map((item) => item.id === updated.id ? updated : item));
		}
		onNotice(`已采用 ${candidate.providerName} 候选并安全写入${includeArtwork ? '标签与封面' : '音乐标签'}`);
	  } catch (error) {
		const message = error instanceof Error ? error.message : '候选资料应用失败';
		onNotice(tagsApplied && includeArtwork ? `标签已写入，但候选封面应用失败：${message}` : message);
	  } finally {
		setSaving(false);
	  }
	};

  const openCandidateSearch = async () => {
    if (!activeTrack) return;
    setCandidateOpen(true);
    setCandidateLoading(true);
    setCandidates([]);
    try {
      setCandidates(await searchCandidates(activeTrack));
    } finally {
      setCandidateLoading(false);
    }
  };

  const changeArtwork = async (file: File | null) => {
	if (!activeTrack) return;
	setSaving(true);
	try {
	  const updated = await updateArtwork(activeTrack.id, file);
	  setTracks((current) => current.map((item) => item.id === updated.id ? updated : item));
	  onNotice(file ? '封面已验证并安全写入音乐文件' : '当前封面已安全删除并记录历史');
	} catch (error) {
	  onNotice(error instanceof Error ? error.message : '封面操作失败');
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

  const applyBatchEdit = async (operations: BatchOperation[], sequenceTracks: boolean) => {
    const selectedTracks = tracks.filter((track) => selectedIds.has(track.id));
    const failed = new Set<string>();
    let succeeded = 0;
    setSaving(true);
    try {
      for (const [index, track] of selectedTracks.entries()) {
        if (!track.writable) {
          failed.add(track.id);
          continue;
        }
        try {
          const updated = await updateTrack(track.id, buildBatchPatch(track, operations, sequenceTracks ? {index, total: selectedTracks.length} : undefined));
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
      ? `已安全写入 ${succeeded} 首曲目的批量标签修改`
      : `已写入 ${succeeded} 首，${failed.size} 首失败并保留选择，请检查后重试`);
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
        activeFolder={activeFolder}
        activeFilter={activeFilter}
        counts={counts}
        sourceLabel={apiReadMode === 'real' ? '真实索引' : 'Mock 模式'}
        indexedSizeBytes={tracks.reduce((total, track) => total + track.sizeBytes, 0)}
        mobileOpen={mobileSidebar}
        onCloseMobile={() => setMobileSidebar(false)}
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
          <button className="toolbar-button"><SlidersHorizontal size={15} /> 筛选 <ChevronDown size={13} /></button>
          <button className="toolbar-button"><ArrowDownUp size={15} /> 专辑顺序 <ChevronDown size={13} /></button>
          <button className="mobile-panel-button" title="打开曲目详情" onClick={() => setMobileInspector(true)}>
            <Menu size={18} />
          </button>
        </div>

        <TrackList
          tracks={visibleTracks}
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
		onSave={async (patch) => { await saveTrack(patch); }}
		onArtworkChange={changeArtwork}
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
        onClose={() => setCandidateOpen(false)}
		onApply={(patch, candidate, options) => applyCandidate(patch, candidate, options.artwork)}
      />

      <BatchEditPanel
        open={batchEditOpen}
        tracks={tracks.filter((track) => selectedIds.has(track.id))}
        saving={saving}
        onClose={() => setBatchEditOpen(false)}
        onApply={applyBatchEdit}
      />
    </div>
  );
}
