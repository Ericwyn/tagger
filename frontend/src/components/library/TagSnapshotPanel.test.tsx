import {render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {describe, expect, it, vi} from 'vitest';
import {TagSnapshotPanel, buildSnapshotUpdates, parseSnapshot, serializeSnapshotCSV, serializeSnapshotJSON, trackToSnapshot} from '@/components/library/TagSnapshotPanel';
import {seedTracks} from '@/mock/data';

describe('tag snapshot', () => {
  it('round-trips JSON and keeps embedded metadata arrays', () => {
    const source = seedTracks.slice(0, 1);
    const parsed = parseSnapshot(serializeSnapshotJSON(source), 'tagger-snapshot.json');

    expect(parsed).toHaveLength(1);
    expect(parsed[0]).toEqual(expect.objectContaining({id: source[0].id, title: source[0].title, composers: source[0].composers}));
  });

  it('round-trips CSV list fields and builds only changed patches', () => {
    const source = seedTracks.slice(0, 1);
    const csv = serializeSnapshotCSV(source);
    const parsed = parseSnapshot(csv, 'tagger-snapshot.csv');
    expect(parsed[0].lyrics).toBe(source[0].lyrics);
    parsed[0].title = '导入后的标题';
    parsed[0].genres = '爵士|摇滚';
    const updates = buildSnapshotUpdates(parsed, source);

    expect(updates).toHaveLength(1);
    expect(updates[0].patch.title).toBe('导入后的标题');
    expect(updates[0].patch.genres).toEqual(['爵士', '摇滚']);
    expect(updates[0].changedFields).toEqual(expect.arrayContaining(['title', 'genres']));
  });

  it('matches snapshots by relative path and reports revision drift', () => {
    const source = seedTracks.slice(0, 1);
    const snapshot = trackToSnapshot(source[0]);
    const updates = buildSnapshotUpdates([{...snapshot, id: '', revision: 'stale', comment: 'imported'}], source);

    expect(updates).toHaveLength(1);
    expect(updates[0].track.id).toBe(source[0].id);
    expect(updates[0].revisionConflict).toBe(true);
    expect(updates[0].patch.comment).toBe('imported');
  });

  it('previews an imported file before applying its safe patch', async () => {
    const user = userEvent.setup();
    const onApply = vi.fn().mockResolvedValue(undefined);
    const source = seedTracks.slice(0, 1);
    const payload = JSON.stringify({tracks: [{id: source[0].id, title: '预览标题', revision: source[0].revision}]});
    render(<TagSnapshotPanel open tracks={source} saving={false} onClose={vi.fn()} onApply={onApply} />);

    await user.upload(screen.getByLabelText(/选择要导入/), new File([payload], 'import.json', {type: 'application/json'}));
    expect(await screen.findByText(/预览标题/)).toBeInTheDocument();
    await user.click(screen.getByRole('button', {name: /确认导入 1 首/}));
    await waitFor(() => expect(onApply).toHaveBeenCalledOnce());
    expect(onApply.mock.calls[0][0][0].patch.title).toBe('预览标题');
  });
});
