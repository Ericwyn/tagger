import {render, screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {describe, expect, it, vi} from 'vitest';
import {TrackInspector} from '@/components/library/TrackInspector';
import {seedTracks} from '@/mock/data';

describe('TrackInspector', () => {
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
    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({title: '再回首（修订）'}));
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
});
