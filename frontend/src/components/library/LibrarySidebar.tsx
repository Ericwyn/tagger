import {useEffect, useRef, useState, type CSSProperties} from 'react';
import {
  ChevronDown,
  ChevronRight,
  Disc3,
  Folder,
  FolderOpen,
  RefreshCw,
  X,
} from 'lucide-react';
import {cn, formatBytes} from '@/lib/utils';
import type {LibrarySummary, TrackHealth} from '@/types';
import type {FolderNode} from '@/types';

export type SidebarFilter = 'all' | TrackHealth;

interface LibrarySidebarProps {
  library: LibrarySummary;
  libraries?: LibrarySummary[];
  switchingLibraryId?: string;
  activeFolder: string | null;
  activeFolderPath?: string | null;
  activeFilter: SidebarFilter;
  sourceLabel: string;
  indexedSizeBytes: number;
  mobileOpen: boolean;
  onCloseMobile: () => void;
  onSwitchLibrary?: (library: LibrarySummary) => void;
  onRescan?: () => void;
  scanning?: boolean;
  onOpenSettings?: () => void;
  onSelectFolder: (id: string | null) => void;
  onSelectFolderPath?: (path: string) => void;
}

interface FolderBranch {
  key: string;
  path: string;
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
        branch = {key, path: parts.slice(0, index + 1).join(' · '), name: part, count: 0, children: []};
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

function branchContainsSelection(branch: FolderBranch, folderId: string | null, folderPath: string | null): boolean {
  return branch.folderId === folderId || branch.path === folderPath || branch.children.some((child) => branchContainsSelection(child, folderId, folderPath));
}

function TreeLabel({children}: {children: string}) {
  const wrapperRef = useRef<HTMLSpanElement>(null);
  const contentRef = useRef<HTMLSpanElement>(null);
  const [overflow, setOverflow] = useState(false);
  const [shift, setShift] = useState(0);

  useEffect(() => {
    const measure = () => {
      const wrapper = wrapperRef.current;
      const content = contentRef.current;
      if (!wrapper || !content) return;
      const nextShift = Math.max(0, content.scrollWidth - wrapper.clientWidth);
      setShift(nextShift);
      setOverflow(nextShift > 1);
    };
    measure();
    if (typeof ResizeObserver === 'undefined' || !wrapperRef.current) return;
    const observer = new ResizeObserver(measure);
    observer.observe(wrapperRef.current);
    return () => observer.disconnect();
  }, [children]);

  const style = {
    '--tree-label-shift': `${shift}px`,
    '--tree-label-duration': `${Math.max(4, Math.min(12, shift / 12))}s`,
  } as CSSProperties;
  return (
    <span ref={wrapperRef} className={cn('tree-label', overflow && 'is-overflow')} title={children} style={style}>
      <span ref={contentRef}>{children}</span>
    </span>
  );
}

function FolderBranchRow({branch, activeFolder, activeFolderPath, onSelectFolder, onSelectFolderPath}: {branch: FolderBranch; activeFolder: string | null; activeFolderPath: string | null; onSelectFolder: (id: string | null) => void; onSelectFolderPath?: (path: string) => void}) {
  const hasChildren = branch.children.length > 0;
  const containsActiveFolder = branchContainsSelection(branch, activeFolder, activeFolderPath);
  const [expanded, setExpanded] = useState(containsActiveFolder);
  const active = branch.folderId === activeFolder || branch.path === activeFolderPath;

  useEffect(() => {
    if (containsActiveFolder) setExpanded(true);
  }, [containsActiveFolder]);

  return (
    <div className="tree-branch">
      <button className={cn('tree-row', active && 'is-active')} aria-expanded={hasChildren ? expanded : undefined} onClick={() => branch.folderId ? onSelectFolder(branch.folderId) : onSelectFolderPath ? onSelectFolderPath(branch.path) : setExpanded((value) => !value)}>
        <span
          className={cn('tree-disclosure', !hasChildren && 'is-empty')}
          onClick={(event) => { if (hasChildren) { event.stopPropagation(); setExpanded((value) => !value); } }}
        >{hasChildren ? (expanded ? <ChevronDown size={13} /> : <ChevronRight size={13} />) : null}</span>
        {expanded ? <FolderOpen size={15} /> : <Folder size={15} />}
        <TreeLabel>{branch.name}</TreeLabel>
        <em>{branch.count}</em>
      </button>
      {expanded && hasChildren && <div className="tree-nested-children">{branch.children.map((child) => <FolderBranchRow key={child.key} branch={child} activeFolder={activeFolder} activeFolderPath={activeFolderPath} onSelectFolder={onSelectFolder} onSelectFolderPath={onSelectFolderPath} />)}</div>}
    </div>
  );
}

export function LibrarySidebar({
  library,
  libraries = [],
  switchingLibraryId,
  activeFolder,
  activeFolderPath = null,
  activeFilter,
  sourceLabel,
  indexedSizeBytes,
  mobileOpen,
  onCloseMobile,
  onSwitchLibrary,
  onRescan,
  scanning = false,
  onOpenSettings,
  onSelectFolder,
  onSelectFolderPath,
}: LibrarySidebarProps) {
  const folders = folderTree(library.folders);
  const [libraryMenuOpen, setLibraryMenuOpen] = useState(false);
  return (
    <aside className={cn('library-sidebar', mobileOpen && 'is-mobile-open')}>
      <div className="sidebar-mobile-head">
        <span>浏览曲库</span>
        <button title="关闭目录" onClick={onCloseMobile}><X size={18} /></button>
      </div>

      <section className="library-card">
        <div className="eyebrow">ACTIVE LIBRARY</div>
        <button className="library-switcher" aria-expanded={libraryMenuOpen} onClick={() => setLibraryMenuOpen((value) => !value)}>
          <span className="library-icon"><Disc3 size={20} /></span>
          <span>
            <strong>{library.name}</strong>
            <small>{library.trackCount} 首 · {library.folderCount} 个目录</small>
          </span>
          <ChevronDown size={16} />
        </button>
        {libraryMenuOpen && (
          <div className="library-switcher-menu" role="listbox" aria-label="切换音乐库">
            {libraries.map((item) => (
              <button key={item.id} role="option" aria-selected={item.id === library.id} disabled={item.id === library.id || Boolean(switchingLibraryId)} onClick={() => { setLibraryMenuOpen(false); onSwitchLibrary?.(item); }}>
                <span><strong>{item.name}</strong><small>{item.rootPath || item.rootLabel}</small></span>
                {switchingLibraryId === item.id ? <RefreshCw size={14} className="spin" /> : item.id === library.id ? <span className="library-menu-current">当前</span> : <ChevronRight size={14} />}
              </button>
            ))}
            {libraries.length === 0 && <p>尚未注册其他曲库</p>}
            {onOpenSettings && <button className="library-menu-settings" onClick={() => { setLibraryMenuOpen(false); onOpenSettings(); }}><FolderOpen size={14} /> 管理曲库</button>}
          </div>
        )}
        <div className="scan-line">
          <span><i /> 已同步 · {library.lastScanLabel}</span>
          <button title="重新扫描曲库" disabled={scanning} onClick={onRescan}>{<RefreshCw size={14} className={scanning ? 'spin' : undefined} />}</button>
        </div>
      </section>

      <section className="sidebar-section">
        <div className="section-label">目录</div>
        <button
          className={cn('tree-row tree-root', activeFolder === null && activeFolderPath === null && activeFilter === 'all' && 'is-active')}
          onClick={() => {
            onSelectFolder(null);
          }}
        >
          <span className="tree-disclosure is-empty" aria-hidden="true" />
          <FolderOpen size={16} />
          <TreeLabel>{library.rootLabel}</TreeLabel>
          <em>{library.trackCount}</em>
        </button>
        <div className="tree-children">
          {folders.map((folder) => <FolderBranchRow key={folder.key} branch={folder} activeFolder={activeFolder} activeFolderPath={activeFolderPath} onSelectFolder={onSelectFolder} onSelectFolderPath={onSelectFolderPath} />)}
        </div>
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
