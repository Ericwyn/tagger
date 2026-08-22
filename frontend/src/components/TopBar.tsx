import {
  History,
  LibraryBig,
  ListTodo,
  Settings2,
} from 'lucide-react';
import {cn} from '@/lib/utils';
import {GlobalPlayer} from '@/components/GlobalPlayer';
import type {PageID, Track} from '@/types';

interface TopBarProps {
  page: PageID;
  onNavigate: (page: PageID) => void;
  playerTrack: Track | null;
  playerPlaying: boolean;
  onPlayerPlayingChange: (playing: boolean) => void;
  onPlayerClose: () => void;
}

const navItems: Array<{id: PageID; label: string; icon: typeof LibraryBig}> = [
  {id: 'library', label: '曲库', icon: LibraryBig},
  {id: 'jobs', label: '任务', icon: ListTodo},
  {id: 'history', label: '历史', icon: History},
  {id: 'settings', label: '设置', icon: Settings2},
];

export function TopBar({page, onNavigate, playerTrack, playerPlaying, onPlayerPlayingChange, onPlayerClose}: TopBarProps) {
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

      <GlobalPlayer track={playerTrack} playing={playerPlaying} onPlayingChange={onPlayerPlayingChange} onClose={onPlayerClose} />
    </header>
  );
}
