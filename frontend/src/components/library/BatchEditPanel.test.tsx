import {render, screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, describe, expect, it, vi} from 'vitest';
import {BatchEditPanel, buildBatchPatch} from '@/components/library/BatchEditPanel';
import {seedTracks} from '@/mock/data';

const track = seedTracks[0];

describe('BatchEditPanel', () => {
  beforeEach(() => localStorage.clear());

  it('builds explicit common-field and sequence patches', () => {
    const patch = buildBatchPatch(track, [
      {field: 'album', mode: 'set', value: '精选集'},
      {field: 'genres', mode: 'append', value: '民谣, 现场'},
      {field: 'albumArtists', mode: 'delete', value: ''},
    ], {index: 1, total: 3});

    expect(patch.album).toBe('精选集');
    expect(patch.genres).toEqual([...track.genres, '民谣', '现场']);
    expect(patch.albumArtists).toEqual([]);
    expect(patch.trackNumber).toBe(2);
    expect(patch.trackTotal).toBe(3);

    const extended = buildBatchPatch(track, [
      {field: 'comment', mode: 'set', value: 'liner note'},
      {field: 'composers', mode: 'append', value: 'Composer'},
      {field: 'bpm', mode: 'set', value: '128'},
    ]);
    expect(extended.comment).toBe('liner note');
    expect(extended.composers).toEqual([...track.composers, 'Composer']);
    expect(extended.bpm).toBe(128);
  });

  it('applies text and list find-replace operations without introducing duplicates', () => {
    const replaceTitle = buildBatchPatch({...track, title: '[Live] 再回首'}, [
      {field: 'title', mode: 'replace', find: '[Live] ', value: ''},
    ]);
    expect(replaceTitle.title).toBe('再回首');

    const replaceArtists = buildBatchPatch({...track, artists: ['歌手 (原唱)', '歌手 (原唱)']}, [
      {field: 'artists', mode: 'replace', find: ' (原唱)', value: ''},
    ]);
    expect(replaceArtists.artists).toEqual(['歌手']);
  });

  it('shows a preview and submits selected operations', async () => {
    const user = userEvent.setup();
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(<BatchEditPanel open tracks={[track]} saving={false} onClose={vi.fn()} onApply={onApply} />);

    await user.selectOptions(screen.getByLabelText('专辑操作'), 'set');
    await user.type(screen.getByLabelText('专辑值'), '现场精选');
    expect(screen.getByText('现场精选')).toBeInTheDocument();

    await user.click(screen.getByRole('button', {name: /应用到 1 首/}));
    expect(onApply).toHaveBeenCalledWith([{field: 'album', mode: 'set', value: '现场精选'}], false);
  });

  it('shows separate find and replacement inputs and submits the operation', async () => {
    const user = userEvent.setup();
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(<BatchEditPanel open tracks={[track]} saving={false} onClose={vi.fn()} onApply={onApply} />);

    await user.selectOptions(screen.getByLabelText('标题操作'), 'replace');
    await user.type(screen.getByLabelText('标题查找'), '再');
    await user.type(screen.getByLabelText('标题值'), '再（修复）');
    expect(screen.getByText('再（修复）回首')).toBeInTheDocument();
    await user.click(screen.getByRole('button', {name: /应用到 1 首/}));
    expect(onApply).toHaveBeenCalledWith([{field: 'title', mode: 'replace', value: '再（修复）', find: '再'}], false);
  });

  it('requires find text before enabling a replacement batch write', async () => {
    const user = userEvent.setup();
    render(<BatchEditPanel open tracks={[track]} saving={false} onClose={vi.fn()} onApply={vi.fn().mockResolvedValue(undefined)} />);
    await user.selectOptions(screen.getByLabelText('标题操作'), 'replace');
    expect(screen.getByRole('button', {name: /应用到 1 首/})).toBeDisabled();
    expect(screen.getByText(/查找替换必须填写查找内容/)).toBeInTheDocument();
  });

  it('exposes extended fields as safe batch operations', async () => {
    const user = userEvent.setup();
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(<BatchEditPanel open tracks={[track]} saving={false} onClose={vi.fn()} onApply={onApply} />);

    await user.selectOptions(screen.getByLabelText('注释操作'), 'set');
    await user.type(screen.getByLabelText('注释值'), 'liner note');
    await user.click(screen.getByRole('button', {name: /应用到 1 首/}));
    expect(onApply).toHaveBeenCalledWith([{field: 'comment', mode: 'set', value: 'liner note'}], false);
  });

  it('saves and reloads a reusable batch rule template', async () => {
    const user = userEvent.setup();
    const onApply = vi.fn().mockResolvedValue(undefined);
    const view = render(<BatchEditPanel open tracks={[track]} saving={false} onClose={vi.fn()} onApply={onApply} />);

    await user.selectOptions(screen.getByLabelText('专辑操作'), 'set');
    await user.type(screen.getByLabelText('专辑值'), '现场精选');
    await user.type(screen.getByLabelText('模板名称'), '现场专辑');
    await user.click(screen.getByRole('button', {name: '保存当前规则'}));
    expect(screen.getByText('模板「现场专辑」已保存')).toBeInTheDocument();

    view.rerender(<BatchEditPanel open={false} tracks={[track]} saving={false} onClose={vi.fn()} onApply={onApply} />);
    view.rerender(<BatchEditPanel open tracks={[track]} saving={false} onClose={vi.fn()} onApply={onApply} />);
    await user.selectOptions(screen.getByLabelText('批量编辑模板'), '现场专辑');
    expect(screen.getByLabelText('专辑操作')).toHaveValue('set');
    expect(screen.getByLabelText('专辑值')).toHaveValue('现场精选');
  });

  it('supports replacing the same embedded cover across the selected tracks', async () => {
    const user = userEvent.setup();
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(<BatchEditPanel open tracks={[track]} saving={false} onClose={vi.fn()} onApply={onApply} />);

    await user.selectOptions(screen.getByLabelText('批量封面操作'), 'replace');
    const file = new File([new Uint8Array([137, 80, 78, 71])], 'batch-cover.png', {type: 'image/png'});
    await user.upload(screen.getByLabelText('选择封面图片'), file);
    await user.selectOptions(screen.getByLabelText('批量封面写入尺寸'), '500');
    await user.click(screen.getByRole('button', {name: /应用到 1 首/}));

    expect(onApply).toHaveBeenCalledWith([], false, {action: 'replace', file, maxSize: 500});
  });

  it('can reuse an embedded cover from one selected song', async () => {
    const user = userEvent.setup();
    const onApply = vi.fn().mockResolvedValue(undefined);
    const source = {...track, id: 'trk-source', title: '封面来源曲目', artworkCount: 1, artworkWidth: 1200, artworkHeight: 1200};
    const target = {...track, id: 'trk-target', title: '被覆盖曲目', artworkCount: 0};
    render(<BatchEditPanel open tracks={[source, target]} saving={false} onClose={vi.fn()} onApply={onApply} />);

    await user.selectOptions(screen.getByLabelText('批量封面操作'), 'replace');
    await user.selectOptions(screen.getByLabelText('封面来源'), 'track');
    await user.selectOptions(screen.getByLabelText('封面来源曲目'), 'trk-source');
    await user.click(screen.getByRole('button', {name: /应用到 2 首/}));

    expect(onApply).toHaveBeenCalledWith([], false, {action: 'replace', sourceTrack: source, maxSize: 0});
  });
});
