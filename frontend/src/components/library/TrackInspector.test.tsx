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
});
