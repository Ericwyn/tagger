import {render, screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type {ComponentProps} from 'react';
import {describe, expect, it, vi} from 'vitest';
import {LibrarySidebar} from '@/components/library/LibrarySidebar';
import type {LibrarySummary} from '@/types';

const library: LibrarySummary = {
  id: 'lib-one', name: 'TestMusic', rootLabel: 'TestMusic', rootPath: '/music/one', active: true,
  trackCount: 12, folderCount: 2, writable: true, lastScanLabel: '刚刚', folders: [],
};

const otherLibrary: LibrarySummary = {
  ...library, id: 'lib-two', name: 'Archive', rootLabel: 'Archive', rootPath: '/music/two', active: false,
  trackCount: 4,
};

type SidebarTestOverrides = Pick<ComponentProps<typeof LibrarySidebar>, 'onSwitchLibrary' | 'onRescan' | 'onOpenSettings'>;

function renderSidebar(overrides: Partial<SidebarTestOverrides> = {}) {
  const props: ComponentProps<typeof LibrarySidebar> = {
    library,
    libraries: [library, otherLibrary],
    activeFolder: null,
    activeFilter: 'all',
    counts: {all: 12, complete: 12, 'missing-artwork': 0, 'missing-lyrics': 0, 'needs-review': 0, 'parse-error': 0},
    sourceLabel: 'Mock',
    indexedSizeBytes: 1024,
    mobileOpen: false,
    onCloseMobile: vi.fn(),
    onSelectFolder: vi.fn(),
    onSelectFilter: vi.fn(),
    onSwitchLibrary: overrides.onSwitchLibrary ?? vi.fn(),
    onRescan: overrides.onRescan ?? vi.fn(),
    onOpenSettings: overrides.onOpenSettings ?? vi.fn(),
  };
  return render(<LibrarySidebar {...props} />);
}

describe('LibrarySidebar library controls', () => {
  it('switches between registered libraries without leaving the page', async () => {
    const user = userEvent.setup();
    const onSwitchLibrary = vi.fn();
    renderSidebar({onSwitchLibrary});

    await user.click(screen.getByRole('button', {name: /12 首/}));
    expect(screen.getByRole('listbox', {name: '切换音乐库'})).toBeInTheDocument();
    await user.click(screen.getByRole('option', {name: /Archive/}));
    expect(onSwitchLibrary).toHaveBeenCalledWith(otherLibrary);
  });

  it('connects the compact rescan action and settings shortcut', async () => {
    const user = userEvent.setup();
    const onRescan = vi.fn();
    const onOpenSettings = vi.fn();
    renderSidebar({onRescan, onOpenSettings});

    await user.click(screen.getByTitle('重新扫描曲库'));
    expect(onRescan).toHaveBeenCalledOnce();
    await user.click(screen.getByRole('button', {name: /12 首/}));
    await user.click(screen.getByRole('button', {name: /管理曲库/}));
    expect(onOpenSettings).toHaveBeenCalledOnce();
  });
});
