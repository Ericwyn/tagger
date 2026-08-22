import {useEffect, useState} from 'react';
import {Disc3} from 'lucide-react';
import {cn, initials} from '@/lib/utils';
import type {CoverTone} from '@/types';

interface CoverArtProps {
  title: string;
  artist?: string;
  tone: CoverTone;
  size?: 'xs' | 'sm' | 'md' | 'lg' | 'hero';
  missing?: boolean;
  imageUrl?: string;
  blankOnImageError?: boolean;
  className?: string;
  onImageInfo?: (info: {width: number; height: number}) => void;
}

export function CoverArt({title, artist, tone, size = 'md', missing, imageUrl, blankOnImageError = false, className, onImageInfo}: CoverArtProps) {
	const [imageFailed, setImageFailed] = useState(false);
	const [imageLoaded, setImageLoaded] = useState(false);

	useEffect(() => {
	  setImageFailed(false);
	  setImageLoaded(false);
	}, [imageUrl]);

  const imagePending = Boolean(imageUrl) && !imageFailed && !imageLoaded;
  const shouldBlank = Boolean(missing) || (blankOnImageError && (!imageUrl || imageFailed || imagePending));

  return (
    <div className={cn('cover-art', !shouldBlank && `cover-${tone}`, `cover-${size}`, shouldBlank && 'cover-missing', className)} aria-label={shouldBlank ? '没有封面' : `${title} 封面`}>
      {imageUrl && !imageFailed && <img className="cover-image" src={imageUrl} alt="" onError={() => setImageFailed(true)} onLoad={(event) => { setImageLoaded(true); onImageInfo?.({width: event.currentTarget.naturalWidth, height: event.currentTarget.naturalHeight}); }} />}
      {!shouldBlank && <>
        <span className="cover-index">{initials(artist || title)}</span>
        <span className="cover-title">{title}</span>
        <span className="cover-rule" />
        <span className="cover-artist">{artist}</span>
      </>}
      {shouldBlank && !imagePending && <Disc3 aria-hidden="true" />}
    </div>
  );
}
