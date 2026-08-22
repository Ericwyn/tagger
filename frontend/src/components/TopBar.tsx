import {
  History,
  LibraryBig,
  ListTodo,
  MoonStar,
  Settings2,
  SunMedium,
} from 'lucide-react';
import {cn} from '@/lib/utils';
import type {PageID} from '@/types';

interface TopBarProps {
  page: PageID;
  onNavigate: (page: PageID) => void;
  dark: boolean;
  onToggleTheme: () => void;
}

const navItems: Array<{id: PageID; label: string; icon: typeof LibraryBig}> = [
  {id: 'library', label: '曲库', icon: LibraryBig},
  {id: 'jobs', label: '任务', icon: ListTodo},
  {id: 'history', label: '历史', icon: History},
  {id: 'settings', label: '设置', icon: Settings2},
];

export function TopBar({page, onNavigate, dark, onToggleTheme}: TopBarProps) {
  return (
    <header className="top-bar">
      <button className="brand-block" onClick={() => onNavigate('library')} aria-label="返回曲库">
        <span className="brand-mark">
          <span />
          <span />
          <span />
        </span>
        <span>
          <strong>TAGGER</strong>
          <small>MUSIC ARCHIVE / 01</small>
        </span>
      </button>

      <nav className="primary-nav" aria-label="主导航">
        {navItems.map(({id, label, icon: Icon}) => (
          <button
            key={id}
            className={cn('nav-item', page === id && 'is-active')}
            onClick={() => onNavigate(id)}
          >
            <Icon size={16} aria-hidden="true" />
            <span>{label}</span>
          </button>
        ))}
      </nav>

      <div className="top-actions">
        <button className="icon-button" title={dark ? '切换浅色主题' : '切换深色主题'} onClick={onToggleTheme}>
          {dark ? <SunMedium size={18} /> : <MoonStar size={18} />}
        </button>
      </div>
    </header>
  );
}
