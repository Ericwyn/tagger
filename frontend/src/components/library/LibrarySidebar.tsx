import {useState} from 'react';
import {
  AlertTriangle,
  ChevronDown,
  ChevronRight,
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
import {cn, formatBytes} from '@/lib/utils';
import type {LibrarySummary, TrackHealth} from '@/types';
import type {FolderNode} from '@/types';

export type SidebarFilter = 'all' | TrackHealth;

interface LibrarySidebarProps {
  library: LibrarySummary;
  activeFolder: string | null;
  activeFilter: SidebarFilter;
  counts: Record<SidebarFilter, number>;
  sourceLabel: string;
  indexedSizeBytes: number;
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

interface FolderBranch {
  key: string;
  name: string;
  count: number;
  folderId?: string;
  children: FolderBranch[];
}

function folderTree(folders: FolderNode[]): FolderBranch[] {
  const roots: FolderBranch[] = [];
  for (const folder of folders) {
    if (folder.id === 'folder-root') continue;
    const parts = folder.name.split(' · ').map((part) => part.trim()).filter(Boolean);
    if (parts.length === 0) continue;
    let siblings = roots;
    let key = '';
    parts.forEach((part, index) => {
      key = key ? `${key}/${part}` : part;
      let branch = siblings.find((item) => item.key === key);
      if (!branch) {
        branch = {key, name: part, count: 0, children: []};
        siblings.push(branch);
      }
      branch.count += folder.count;
      if (index === parts.length - 1) branch.folderId = folder.id;
      siblings = branch.children;
    });
  }
  const sort = (items: FolderBranch[]) => {
    items.sort((left, right) => left.name.localeCompare(right.name, 'zh-CN'));
    items.forEach((item) => sort(item.children));
  };
  sort(roots);
  return roots;
}

function FolderBranchRow({branch, activeFolder, onSelectFolder}: {branch: FolderBranch; activeFolder: string | null; onSelectFolder: (id: string | null) => void}) {
  const [expanded, setExpanded] = useState(false);
  const hasChildren = branch.children.length > 0;
  const active = branch.folderId === activeFolder;
  return (
    <div className="tree-branch">
      <button className={cn('tree-row', active && 'is-active')} aria-expanded={hasChildren ? expanded : undefined} onClick={() => branch.folderId ? onSelectFolder(branch.folderId) : setExpanded((value) => !value)}>
        <span
          className={cn('tree-disclosure', !hasChildren && 'is-empty')}
          onClick={(event) => { if (hasChildren) { event.stopPropagation(); setExpanded((value) => !value); } }}
        >{hasChildren ? (expanded ? <ChevronDown size={13} /> : <ChevronRight size={13} />) : null}</span>
        {expanded ? <FolderOpen size={15} /> : <Folder size={15} />}
        <span>{branch.name}</span>
        <em>{branch.count}</em>
      </button>
      {expanded && hasChildren && <div className="tree-nested-children">{branch.children.map((child) => <FolderBranchRow key={child.key} branch={child} activeFolder={activeFolder} onSelectFolder={onSelectFolder} />)}</div>}
    </div>
  );
}

export function LibrarySidebar({
  library,
  activeFolder,
  activeFilter,
  counts,
  sourceLabel,
  indexedSizeBytes,
  mobileOpen,
  onCloseMobile,
  onSelectFolder,
  onSelectFilter,
}: LibrarySidebarProps) {
  const folders = folderTree(library.folders);
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
          <span className="tree-disclosure is-empty" aria-hidden="true" />
          <FolderOpen size={16} />
          <span>{library.rootLabel}</span>
          <em>{library.trackCount}</em>
        </button>
        <div className="tree-children">
          {folders.map((folder) => <FolderBranchRow key={folder.key} branch={folder} activeFolder={activeFolder} onSelectFolder={onSelectFolder} />)}
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
          <strong>{formatBytes(indexedSizeBytes)}</strong>
        </div>
        <div className="storage-track"><span style={{width: '34%'}} /></div>
        <p>{library.writable ? '音乐目录可写' : '音乐目录只读'} · {sourceLabel}</p>
      </section>
    </aside>
  );
}
