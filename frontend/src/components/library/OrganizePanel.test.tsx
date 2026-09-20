import {render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {describe, expect, it, vi} from 'vitest';
import {OrganizePanel} from '@/components/library/OrganizePanel';
import {seedTracks} from '@/mock/data';

describe('OrganizePanel', () => {
  it('previews selectable destinations and confirms a safe move', async () => {
    const user = userEvent.setup();
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(<OrganizePanel open tracks={[seedTracks[0]]} basePath="M" saving={false} onClose={vi.fn()} onApply={onApply} />);

    expect(await screen.findByText(/正在计算安全路径|再回首-姜育恒\.flac/)).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole('button', {name: /确认整理/})).not.toBeDisabled());
    expect(screen.getByText(/可整理/)).toBeInTheDocument();
    await user.selectOptions(screen.getByRole('combobox', {name: '文件整理模式'}), 'artist');
    await waitFor(() => expect(screen.getByText('M/姜育恒/再回首-姜育恒.flac')).toBeInTheDocument());
    expect(screen.getByText('M')).toBeInTheDocument();
    expect(screen.queryByText('M/姜育恒/多年以后·再回首/再回首-姜育恒.flac')).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', {name: /确认整理/}));
    expect(onApply).toHaveBeenCalledOnce();
    expect(onApply).toHaveBeenCalledWith('artist', 'M');
  });
});
