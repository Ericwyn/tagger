import {render, screen, waitFor} from '@testing-library/react';
import {beforeEach, describe, expect, it, vi} from 'vitest';

const api = vi.hoisted(() => ({
  listJobs: vi.fn(),
  listBatchEditItems: vi.fn(),
  listTracks: vi.fn(),
  subscribeJobEvents: vi.fn(() => () => undefined),
  cancelJob: vi.fn(),
  retryJob: vi.fn(),
}));

vi.mock('@/api', () => ({...api, apiReadMode: 'real'}));

import {JobsPage} from '@/pages/JobsPage';

describe('JobsPage', () => {
  beforeEach(() => {
    api.listJobs.mockResolvedValue([{
      id: 'job-edit', kind: 'batch_edit', state: 'partial', title: '批量编辑标签', detail: '1 首失败',
      processed: 2, total: 2, succeeded: 1, failed: 1, startedAt: '刚刚',
    }]);
    api.listTracks.mockResolvedValue([{id: 'trk-1', fileName: '忽略不计.mp3'}]);
    api.listBatchEditItems.mockResolvedValue([{
      id: 'item-1', jobId: 'job-edit', trackId: 'trk-1', state: 'failed', error: 'revision conflict',
      diff: [{field: 'genres', operation: 'set', before: ['Pop'], after: ['Live']}],
    }]);
  });

  it('shows per-file status, error and diff for batch edit jobs', async () => {
    render(<JobsPage onOpenReview={() => {}} />);

    expect(await screen.findByRole('heading', {name: '批量编辑标签'})).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText('忽略不计.mp3')).toBeInTheDocument());
    expect(screen.getAllByText('失败').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('revision conflict')).toBeInTheDocument();
    expect(screen.getByText('Pop')).toBeInTheDocument();
    expect(screen.getByText('Live')).toBeInTheDocument();
    expect(api.listBatchEditItems).toHaveBeenCalledWith('job-edit');
  });

  it('filters the task list when a status tab is selected', async () => {
    const user = (await import('@testing-library/user-event')).default.setup();
    render(<JobsPage onOpenReview={() => {}} />);
    expect(await screen.findByRole('heading', {name: '批量编辑标签'})).toBeInTheDocument();

    await user.click(screen.getByRole('button', {name: /进行中/}));
    expect(screen.queryByRole('heading', {name: '批量编辑标签'})).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', {name: /失败/}));
    expect(screen.getByRole('heading', {name: '批量编辑标签'})).toBeInTheDocument();
  });

  it('allows a review task to be discarded before switching libraries', async () => {
    const user = (await import('@testing-library/user-event')).default.setup();
    const reviewJob = {
      id: 'job-review', kind: 'match', state: 'review', title: '批量抓取元数据', detail: '等待审核',
      processed: 2, total: 2, succeeded: 2, failed: 0, startedAt: '刚刚',
    } as const;
    api.listJobs.mockResolvedValue([reviewJob]);
    api.cancelJob.mockResolvedValue({...reviewJob, state: 'cancelled', detail: '任务已取消'});
    vi.stubGlobal('confirm', vi.fn(() => true));
    render(<JobsPage onOpenReview={() => {}} />);
    expect(await screen.findByRole('heading', {name: '批量抓取元数据'})).toBeInTheDocument();
    await user.click(screen.getByRole('button', {name: '丢弃审核任务'}));
    await waitFor(() => expect(api.cancelJob).toHaveBeenCalledWith('job-review'));
    vi.unstubAllGlobals();
  });
});
