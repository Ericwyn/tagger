import {render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {describe, expect, it, vi} from 'vitest';
import {OrganizePanel} from '@/components/library/OrganizePanel';
import {seedTracks} from '@/mock/data';

describe('OrganizePanel', () => {
  it('previews the artist/album destination and confirms a safe move', async () => {
    const user = userEvent.setup();
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(<OrganizePanel open tracks={[seedTracks[0]]} saving={false} onClose={vi.fn()} onApply={onApply} />);

    expect(await screen.findByText(/正在计算安全路径|再回首-姜育恒\.flac/)).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole('button', {name: /确认整理/})).not.toBeDisabled());
    expect(screen.getByText(/可整理/)).toBeInTheDocument();
    await user.click(screen.getByRole('button', {name: /确认整理/}));
    expect(onApply).toHaveBeenCalledOnce();
  });
});
