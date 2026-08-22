import {render, screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {describe, expect, it, vi} from 'vitest';
import {ReviewPage} from '@/pages/ReviewPage';

describe('ReviewPage field selection', () => {
  it('keeps diff checkbox state at the review item level', async () => {
    const user = userEvent.setup();
    render(<ReviewPage trackIds={['trk-001']} onBack={vi.fn()} onComplete={vi.fn()} />);

    expect(await screen.findByRole('heading', {name: '审核抓取结果'})).toBeInTheDocument();
    const titleToggle = screen.getByRole('button', {name: '采用标题'});
    expect(titleToggle).not.toHaveClass('is-checked');

    await user.click(titleToggle);

    expect(screen.getByRole('button', {name: '取消采用标题'})).toHaveClass('is-checked');
	await user.click(screen.getByRole('button', {name: '全选字段'}));
	expect(screen.getByRole('button', {name: '取消采用标题'})).toHaveClass('is-checked');
	await user.click(screen.getByRole('button', {name: '全部取消字段'}));
	expect(screen.getByRole('button', {name: '采用标题'})).not.toHaveClass('is-checked');
	const artworkToggle = screen.getByRole('checkbox', {name: '审核是否写入候选封面'});
	await user.click(artworkToggle);
	expect(screen.getByRole('button', {name: '取消采用替换封面'})).toHaveClass('is-checked');
	const artworkSize = screen.getByRole('combobox', {name: '审核封面写入尺寸'});
	await user.selectOptions(artworkSize, '500');
	expect(artworkSize).toHaveValue('500');
	});

  it('opens every candidate and lets the reviewer choose a specific source', async () => {
    const user = userEvent.setup();
    render(<ReviewPage trackIds={['trk-001', 'trk-002']} onBack={vi.fn()} onComplete={vi.fn()} />);

    expect(await screen.findByRole('heading', {name: '审核抓取结果'})).toBeInTheDocument();
    expect(screen.getByRole('heading', {name: '再回首'})).toBeInTheDocument();

    await user.click(screen.getByRole('button', {name: /更换候选/}));
    expect(screen.getByRole('listbox', {name: '候选列表'})).toBeInTheDocument();
    await user.click(screen.getByRole('option', {name: /Apple Music.*apple-182911/}));
    expect(screen.getByText(/Apple Music \/ apple-182911/)).toBeInTheDocument();

    await user.click(screen.getByRole('button', {name: /接受候选/}));
    expect(screen.getByRole('heading', {name: '愛上一個不回家的人'})).toBeInTheDocument();
  });

  it('filters the review queue by persisted decision state and provider', async () => {
    const user = userEvent.setup();
    render(<ReviewPage trackIds={['trk-001', 'trk-002']} onBack={vi.fn()} onComplete={vi.fn()} />);

    expect(await screen.findByRole('heading', {name: '审核抓取结果'})).toBeInTheDocument();
    await user.click(screen.getByRole('button', {name: /跳过此曲/}));

    await user.selectOptions(screen.getByRole('combobox', {name: '审核状态筛选'}), 'skipped');
    expect(screen.getByRole('button', {name: /再回首/})).toBeInTheDocument();
    expect(screen.queryByRole('button', {name: /愛上一個不回家的人/})).not.toBeInTheDocument();

    await user.selectOptions(screen.getByRole('combobox', {name: '候选来源筛选'}), 'netease');
    expect(screen.getByRole('combobox', {name: '候选来源筛选'})).toHaveValue('netease');
  });
});
