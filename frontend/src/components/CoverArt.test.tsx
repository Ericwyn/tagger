import {render, screen} from '@testing-library/react';
import {describe, expect, it} from 'vitest';
import {CoverArt} from '@/components/CoverArt';

describe('CoverArt', () => {
  it('keeps an empty cover blank when generated placeholders are disabled', () => {
    render(<CoverArt title="歌曲" artist="歌手" tone="vermilion" blankOnImageError />);

    expect(screen.getByLabelText('没有封面')).toBeInTheDocument();
    expect(screen.queryByText('歌曲')).not.toBeInTheDocument();
    expect(screen.queryByText('歌手')).not.toBeInTheDocument();
  });
});
