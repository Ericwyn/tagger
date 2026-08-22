import {Disc3} from 'lucide-react';
import {cn, initials} from '@/lib/utils';
import type {CoverTone} from '@/types';

interface CoverArtProps {
  title: string;
  artist?: string;
  tone: CoverTone;
  size?: 'xs' | 'sm' | 'md' | 'lg' | 'hero';
  missing?: boolean;
  className?: string;
}

export function CoverArt({title, artist, tone, size = 'md', missing, className}: CoverArtProps) {
  if (missing) {
    return (
      <div className={cn('cover-art cover-missing', `cover-${size}`, className)} aria-label="没有封面">
        <Disc3 aria-hidden="true" />
      </div>
    );
  }

  return (
    <div className={cn('cover-art', `cover-${tone}`, `cover-${size}`, className)} aria-label={`${title} 封面`}>
      <span className="cover-index">{initials(artist || title)}</span>
      <span className="cover-title">{title}</span>
      <span className="cover-rule" />
      <span className="cover-artist">{artist}</span>
    </div>
  );
}
