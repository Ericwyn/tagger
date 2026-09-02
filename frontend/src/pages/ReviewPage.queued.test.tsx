import {render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, describe, expect, it, vi} from 'vitest';
import {seedTracks} from '@/mock/data';

const api = vi.hoisted(() => ({
  resolveTracks: vi.fn(),
  createMatchJob: vi.fn(),
  waitForJob: vi.fn(),
	listMatchItems: vi.fn(),
}));

vi.mock('@/api', async () => ({
  ...(await vi.importActual<typeof import('@/api')>('@/api')),
  apiReadMode: 'real',
  ...api,
}));

import {ReviewPage} from '@/pages/ReviewPage';

describe('ReviewPage queued match navigation', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.resolveTracks.mockResolvedValue({tracks: [seedTracks[0]], total: 1});
    api.createMatchJob.mockResolvedValue({
      id: 'job-queued', kind: 'match', state: 'running', title: '批量抓取元数据', detail: '开始查询',
      processed: 0, total: 1, succeeded: 0, failed: 0, startedAt: '刚刚',
    });
    api.waitForJob.mockImplementation(() => new Promise(() => undefined));
	api.listMatchItems.mockResolvedValue([]);
  });

  it('notifies the app immediately and does not wait on the review screen', async () => {
    const onJobQueued = vi.fn();
    render(<ReviewPage trackIds={[seedTracks[0].id]} onBack={vi.fn()} onComplete={vi.fn()} onJobQueued={onJobQueued} />);

    await waitFor(() => expect(onJobQueued).toHaveBeenCalledWith('job-queued'));
    expect(api.waitForJob).not.toHaveBeenCalled();
  });

	it('shows a recoverable error instead of an empty review page when loading fails', async () => {
	  const user = userEvent.setup();
	  api.listMatchItems.mockRejectedValue(new Error('候选数据格式不兼容'));
	  render(<ReviewPage trackIds={[]} matchJobId="job-broken" onBack={vi.fn()} onComplete={vi.fn()} />);

	  expect(await screen.findByRole('alert')).toHaveTextContent('无法装载审核结果');
	  expect(screen.getByText('候选数据格式不兼容')).toBeInTheDocument();
	  await user.click(screen.getByRole('button', {name: '重试加载'}));
	  await waitFor(() => expect(api.listMatchItems).toHaveBeenCalledTimes(2));
	});
});
