import {fireEvent, render, screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {describe, expect, it, vi} from 'vitest';
import {TrackInspector} from '@/components/library/TrackInspector';
import {seedTracks} from '@/mock/data';
import type {Track, TrackPatch} from '@/types';

describe('TrackInspector', () => {
  it('keeps path hints out of real fields until the user explicitly applies one', async () => {
    const user = userEvent.setup();
    const onSave = vi.fn().mockResolvedValue(undefined);
    const hintedTrack: Track = {
      ...seedTracks[0],
      title: '', artists: [], album: '', albumArtists: [],
      health: 'tag-compatibility',
      tagIssues: ['missing-embedded-title', 'missing-embedded-artist'],
      tagHints: [
        {title: '宮崎歩', artists: ['brave heart'], albumArtists: ['brave heart'], source: 'filename', pattern: 'title-artist'},
        {title: 'brave heart', artists: ['宮崎歩'], albumArtists: ['宮崎歩'], source: 'filename', pattern: 'artist-title'},
      ],
    };
    render(
      <TrackInspector
        track={hintedTrack}
        saving={false}
        mobileOpen
        onCloseMobile={() => {}}
        onSearch={() => {}}
        onSave={onSave}
        onArtworkChange={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    expect(screen.getByLabelText('标题')).toHaveValue('');
    expect(screen.getByLabelText(/^艺术家/)).toHaveValue('');
    expect(screen.getByRole('button', {name: '保存修改'})).toBeDisabled();

    await user.click(screen.getByRole('button', {name: /brave heart · 宮崎歩.*艺术家 - 标题/}));
    expect(screen.getByLabelText('标题')).toHaveValue('brave heart');
    expect(screen.getByLabelText(/^艺术家/)).toHaveValue('宮崎歩');
    expect(screen.getByLabelText('专辑艺术家')).toHaveValue('宮崎歩');

    await user.click(screen.getByRole('button', {name: '保存修改'}));
    await user.click(screen.getByRole('button', {name: '确认写入'}));
    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({
      title: 'brave heart', artists: ['宮崎歩'], albumArtists: ['宮崎歩'],
    }), {writeTag: true});
  });

  it('keeps the historical snapshot notice compact and dismissible', async () => {
    const user = userEvent.setup();
    const onDiscard = vi.fn();
    const patch = {
      title: '历史标题', artists: [], album: '', albumArtists: [], genres: [], lyrics: '', comment: '',
      composers: [], conductor: '', lyricists: [], copyright: '', isrc: '', musicbrainzTrackId: '',
      musicbrainzReleaseId: '', musicbrainzArtistIds: [], acoustidId: '', acoustidFingerprint: '',
    } as TrackPatch;
    const {container} = render(
      <TrackInspector
        track={seedTracks[0]}
        saving={false}
        mobileOpen
        onCloseMobile={() => {}}
        onSearch={() => {}}
        onSave={vi.fn().mockResolvedValue(undefined)}
        onArtworkChange={vi.fn().mockResolvedValue(undefined)}
        restoreDraft={{key: 'rev-1', revisionId: 'rev-1', trackId: seedTracks[0].id, patch, label: '历史修订 rev-1'}}
        onDiscardRestoreDraft={onDiscard}
      />,
    );

    expect(container.querySelector('.track-inspector')?.classList.contains('has-restore-draft')).toBe(true);
    expect(screen.getByText('已加载历史快照')).toBeInTheDocument();
    await user.click(screen.getByRole('button', {name: '取消加载'}));
    expect(onDiscard).toHaveBeenCalledTimes(1);
  });

  it('previews and submits field-level edits', async () => {
    const user = userEvent.setup();
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(
      <TrackInspector
        track={seedTracks[0]}
        saving={false}
        mobileOpen
        onCloseMobile={() => {}}
        onSearch={() => {}}
        onSave={onSave}
		onArtworkChange={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    const title = screen.getByLabelText('标题');
    await user.clear(title);
    await user.type(title, '再回首（修订）');
    await user.click(screen.getByRole('button', {name: '保存修改'}));

    expect(screen.getByText('确认写入 1 项修改')).toBeInTheDocument();
    expect(screen.getByText('再回首（修订）')).toBeInTheDocument();

    await user.click(screen.getByRole('button', {name: '确认写入'}));
    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({title: '再回首（修订）'}), {writeTag: true});
  });

  it('saves edited lyrics to the embedded audio tag without exposing sidecar writing', async () => {
    const user = userEvent.setup();
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(
      <TrackInspector
        track={seedTracks[0]}
        saving={false}
        mobileOpen
        onCloseMobile={() => {}}
        onSearch={() => {}}
        onSave={onSave}
        onArtworkChange={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    await user.click(screen.getByRole('tab', {name: '歌词'}));
    const lyrics = screen.getByPlaceholderText(/在这里输入歌词/);
    fireEvent.change(lyrics, {target: {value: '[00:01.00] embedded only'}});
    expect(screen.queryByRole('checkbox', {name: /同时保存/})).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', {name: '保存修改'}));
    await user.click(screen.getByRole('button', {name: '确认写入'}));

    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({lyrics: '[00:01.00] embedded only'}), {writeTag: true});
  });

  it('edits extended embedded fields without changing unknown raw tags', async () => {
    const user = userEvent.setup();
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(
      <TrackInspector
        track={seedTracks[0]}
        saving={false}
        mobileOpen
        onCloseMobile={() => {}}
        onSearch={() => {}}
        onSave={onSave}
        onArtworkChange={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    await user.click(screen.getByRole('button', {name: /扩展内嵌字段/}));
    await user.type(screen.getByLabelText('注释'), 'liner note');
    await user.type(screen.getByLabelText('BPM'), '128');
    await user.type(screen.getByLabelText('MusicBrainz Track ID'), 'track-mbid');
    await user.click(screen.getByRole('button', {name: '保存修改'}));
    await user.click(screen.getByRole('button', {name: '确认写入'}));

    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({
      comment: 'liner note', bpm: 128, musicbrainzTrackId: 'track-mbid',
    }), {writeTag: true});
  });

  it('uploads artwork and requires a second click before deletion', async () => {
	const user = userEvent.setup();
	const onArtworkChange = vi.fn().mockResolvedValue(undefined);
	const {container} = render(
	  <TrackInspector
		track={seedTracks[0]}
		saving={false}
		mobileOpen
		onCloseMobile={() => {}}
		onSearch={() => {}}
		onSave={vi.fn().mockResolvedValue(undefined)}
		onArtworkChange={onArtworkChange}
	  />,
	);
	await user.click(screen.getByRole('tab', {name: '封面'}));
	const fileInput = container.querySelector<HTMLInputElement>('input[type="file"]');
	if (!fileInput) throw new Error('missing artwork file input');
	const file = new File([new Uint8Array([137, 80, 78, 71])], 'cover.png', {type: 'image/png'});
	await user.upload(fileInput, file);
	expect(onArtworkChange).toHaveBeenCalledWith(file);

	await user.click(screen.getByRole('button', {name: '删除当前封面'}));
	expect(onArtworkChange).toHaveBeenCalledTimes(1);
    await user.click(screen.getByRole('button', {name: '再次点击确认删除'}));
    expect(onArtworkChange).toHaveBeenLastCalledWith(null);
  });

  it('keeps the mock preview player interactive', async () => {
    const user = userEvent.setup();
    render(
      <TrackInspector
        track={seedTracks[0]}
        saving={false}
        mobileOpen
        onCloseMobile={() => {}}
        onSearch={() => {}}
        onSave={vi.fn().mockResolvedValue(undefined)}
        onArtworkChange={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    await user.click(screen.getByTitle('试听'));
    expect(screen.getByTitle('暂停试听')).toBeInTheDocument();
    await user.click(screen.getByTitle('暂停试听'));
    expect(screen.getByTitle('试听')).toBeInTheDocument();
  });

  it('copies the relative path and reports the result', async () => {
    const user = userEvent.setup();
    const onNotice = vi.fn();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {configurable: true, value: {writeText}});
    render(
      <TrackInspector
        track={seedTracks[0]}
        saving={false}
        mobileOpen
        onCloseMobile={() => {}}
        onSearch={() => {}}
        onSave={vi.fn().mockResolvedValue(undefined)}
        onArtworkChange={vi.fn().mockResolvedValue(undefined)}
        onNotice={onNotice}
      />,
    );

    await user.click(screen.getByRole('tab', {name: '技术'}));
    await user.click(screen.getByRole('button', {name: '复制相对路径'}));
    expect(writeText).toHaveBeenCalledWith(seedTracks[0].relativePath);
    expect(onNotice).toHaveBeenCalledWith('相对路径已复制');
  });

  it('opens the raw tag panel in mock mode', async () => {
    const user = userEvent.setup();
    render(
      <TrackInspector
        track={seedTracks[0]}
        saving={false}
        mobileOpen
        onCloseMobile={() => {}}
        onSearch={() => {}}
        onSave={vi.fn().mockResolvedValue(undefined)}
        onArtworkChange={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    await user.click(screen.getByRole('button', {name: '查看原始标签'}));
    expect(await screen.findByText('TagLib PropertyMap')).toBeInTheDocument();
    expect(screen.getByText('TITLE')).toBeInTheDocument();
    expect(screen.getAllByText('再回首').length).toBeGreaterThanOrEqual(1);
  });

  it('opens a lyrics-focused provider search from the lyrics tab', async () => {
    const user = userEvent.setup();
    const onSearch = vi.fn();
    render(
      <TrackInspector
        track={seedTracks[0]}
        saving={false}
        mobileOpen
        onCloseMobile={() => {}}
        onSearch={onSearch}
        onSave={vi.fn().mockResolvedValue(undefined)}
        onArtworkChange={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    await user.click(screen.getByRole('tab', {name: '歌词'}));
    await user.click(screen.getByRole('button', {name: '查找歌词'}));
    expect(onSearch).toHaveBeenCalledWith('lyrics');
  });
});
