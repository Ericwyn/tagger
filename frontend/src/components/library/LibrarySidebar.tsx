import {
  AlertTriangle,
  ChevronDown,
  CircleDashed,
  Disc3,
  FileWarning,
  Folder,
  FolderOpen,
  ImageOff,
  Music2,
  RefreshCw,
  ShieldCheck,
  X,
} from 'lucide-react';
import {cn} from '@/lib/utils';
import type {LibrarySummary, TrackHealth} from '@/types';

export type SidebarFilter = 'all' | TrackHealth;

interface LibrarySidebarProps {
  library: LibrarySummary;
  activeFolder: string | null;
  activeFilter: SidebarFilter;
  counts: Record<SidebarFilter, number>;
  mobileOpen: boolean;
  onCloseMobile: () => void;
  onSelectFolder: (id: string | null) => void;
  onSelectFilter: (filter: SidebarFilter) => void;
}

const smartFilters: Array<{id: SidebarFilter; label: string; icon: typeof Music2}> = [
  {id: 'all', label: '全部音乐', icon: Music2},
  {id: 'missing-artwork', label: '缺少封面', icon: ImageOff},
  {id: 'missing-lyrics', label: '缺少歌词', icon: CircleDashed},
  {id: 'needs-review', label: '需要确认', icon: AlertTriangle},
  {id: 'parse-error', label: '解析失败', icon: FileWarning},
  {id: 'complete', label: '资料完整', icon: ShieldCheck},
];

export function LibrarySidebar({
  library,
  activeFolder,
  activeFilter,
  counts,
  mobileOpen,
  onCloseMobile,
  onSelectFolder,
  onSelectFilter,
}: LibrarySidebarProps) {
  return (
    <aside className={cn('library-sidebar', mobileOpen && 'is-mobile-open')}>
      <div className="sidebar-mobile-head">
        <span>浏览曲库</span>
        <button title="关闭目录" onClick={onCloseMobile}><X size={18} /></button>
      </div>

      <section className="library-card">
        <div className="eyebrow">ACTIVE LIBRARY</div>
        <button className="library-switcher">
          <span className="library-icon"><Disc3 size={20} /></span>
          <span>
            <strong>{library.name}</strong>
            <small>{library.trackCount} 首 · {library.folderCount} 个目录</small>
          </span>
          <ChevronDown size={16} />
        </button>
        <div className="scan-line">
          <span><i /> 已同步 · {library.lastScanLabel}</span>
          <button title="重新扫描曲库"><RefreshCw size={14} /></button>
        </div>
      </section>

      <section className="sidebar-section">
        <div className="section-label">目录</div>
        <button
          className={cn('tree-row tree-root', activeFolder === null && activeFilter === 'all' && 'is-active')}
          onClick={() => {
            onSelectFolder(null);
            onSelectFilter('all');
          }}
        >
          <FolderOpen size={16} />
          <span>TestMusic</span>
          <em>{library.trackCount}</em>
        </button>
        <div className="tree-children">
          {library.folders.map((folder) => (
            <button
              key={folder.id}
              className={cn('tree-row', activeFolder === folder.id && 'is-active')}
              onClick={() => onSelectFolder(folder.id)}
            >
              <Folder size={15} />
              <span>{folder.name}</span>
              <em>{folder.count}</em>
            </button>
          ))}
        </div>
      </section>

      <section className="sidebar-section smart-section">
        <div className="section-label">智能筛选</div>
        {smartFilters.map(({id, label, icon: Icon}) => (
          <button
            key={id}
            className={cn('smart-row', activeFilter === id && activeFolder === null && 'is-active')}
            onClick={() => {
              onSelectFolder(null);
              onSelectFilter(id);
            }}
          >
            <Icon size={15} />
            <span>{label}</span>
            <em>{counts[id] ?? 0}</em>
          </button>
        ))}
      </section>

      <section className="storage-note">
        <div>
          <span>已索引容量</span>
          <strong>612.4 MB</strong>
        </div>
        <div className="storage-track"><span style={{width: '34%'}} /></div>
        <p>音乐目录可写 · Mock 模式</p>
      </section>
    </aside>
  );
}
