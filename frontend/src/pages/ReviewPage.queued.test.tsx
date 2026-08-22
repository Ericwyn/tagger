import {render, waitFor} from '@testing-library/react';
import {beforeEach, describe, expect, it, vi} from 'vitest';
import {seedTracks} from '@/mock/data';

const api = vi.hoisted(() => ({
  listTracks: vi.fn(),
  createMatchJob: vi.fn(),
  waitForJob: vi.fn(),
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
    api.listTracks.mockResolvedValue([seedTracks[0]]);
    api.createMatchJob.mockResolvedValue({
      id: 'job-queued', kind: 'match', state: 'running', title: '批量抓取元数据', detail: '开始查询',
      processed: 0, total: 1, succeeded: 0, failed: 0, startedAt: '刚刚',
    });
    api.waitForJob.mockImplementation(() => new Promise(() => undefined));
  });

  it('notifies the app immediately and does not wait on the review screen', async () => {
    const onJobQueued = vi.fn();
    render(<ReviewPage trackIds={[seedTracks[0].id]} onBack={vi.fn()} onComplete={vi.fn()} onJobQueued={onJobQueued} />);

    await waitFor(() => expect(onJobQueued).toHaveBeenCalledWith('job-queued'));
    expect(api.waitForJob).not.toHaveBeenCalled();
  });
});
