import {render, screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {describe, expect, it, vi} from 'vitest';
import {ReviewPage} from '@/pages/ReviewPage';

describe('ReviewPage field selection', () => {
  it('keeps diff checkbox state at the review item level', async () => {
    const user = userEvent.setup();
    render(<ReviewPage trackIds={['trk-001']} onBack={vi.fn()} onComplete={vi.fn()} />);

    expect(await screen.findByRole('heading', {name: '审核抓取结果'})).toBeInTheDocument();
    const titleToggle = screen.getByRole('button', {name: '取消采用标题'});
    expect(titleToggle).toHaveClass('is-checked');

    await user.click(titleToggle);

    expect(screen.getByRole('button', {name: '采用标题'})).not.toHaveClass('is-checked');
	const artworkToggle = screen.getByRole('button', {name: '采用封面'});
	await user.click(artworkToggle);
	expect(screen.getByRole('button', {name: '取消采用封面'})).toHaveClass('is-checked');
  });
});
