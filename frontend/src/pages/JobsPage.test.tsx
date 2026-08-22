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
});
