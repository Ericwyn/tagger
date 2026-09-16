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
    sourceLabel: 'Mock',
    indexedSizeBytes: 1024,
    mobileOpen: false,
    onCloseMobile: vi.fn(),
    onSelectFolder: vi.fn(),
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

  it('keeps the full folder name available to hover and assistive users', async () => {
    const user = userEvent.setup();
    const longName = '非常长的唱片目录名称-2026-现场录音-高解析度收藏版';
    const clientWidth = vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(80);
    const scrollWidth = vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockReturnValue(260);
    const {container} = render(<LibrarySidebar {...{
      library: {...library, folders: [{id: 'folder-long', name: longName, count: 1}]},
      libraries: [library, otherLibrary], activeFolder: null, activeFilter: 'all',
      sourceLabel: 'Mock', indexedSizeBytes: 1024, mobileOpen: false, onCloseMobile: vi.fn(),
      onSelectFolder: vi.fn(), onSwitchLibrary: vi.fn(), onRescan: vi.fn(), onOpenSettings: vi.fn(),
    }} />);
    await user.hover(screen.getByRole('button', {name: new RegExp(longName)}));
    expect([...document.querySelectorAll('.tree-label-popover strong')].some((node) => node.textContent === longName)).toBe(true);
    expect(container.querySelector(`.tree-label-clip[title="${longName}"]`)).not.toBeInTheDocument();
    clientWidth.mockRestore();
    scrollWidth.mockRestore();
  });
});
