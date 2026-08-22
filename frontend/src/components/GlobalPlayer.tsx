import {useEffect, useRef, useState} from 'react';
import {Pause, Play, X} from 'lucide-react';
import {audioURL} from '@/api';
import {cn, formatDuration} from '@/lib/utils';
import type {Track} from '@/types';

interface GlobalPlayerProps {
  track: Track | null;
  playing: boolean;
  onPlayingChange: (playing: boolean) => void;
  onClose: () => void;
}

export function GlobalPlayer({track, playing, onPlayingChange, onClose}: GlobalPlayerProps) {
  const audioRef = useRef<HTMLAudioElement>(null);
  const [currentTime, setCurrentTime] = useState(0);
  const source = track ? audioURL(track) : undefined;

  useEffect(() => {
    const audio = audioRef.current;
    if (!audio) return;
    setCurrentTime(0);
    if (source) {
      audio.pause();
      audio.src = source;
      audio.load();
    } else {
      audio.removeAttribute('src');
    }
    if (playing && source) {
      void audio.play().catch(() => onPlayingChange(false));
    }
  }, [source, track?.id]);

  useEffect(() => {
    const audio = audioRef.current;
    if (!audio || !source) return;
    if (playing && audio.paused) {
      void audio.play().catch(() => onPlayingChange(false));
    } else if (!playing && !audio.paused) {
      audio.pause();
    }
  }, [playing, source, onPlayingChange]);

  if (!track) return null;

  const toggle = () => {
    const audio = audioRef.current;
    if (!source || !audio) {
      onPlayingChange(!playing);
      return;
    }
    if (audio.paused) {
      void audio.play().then(() => onPlayingChange(true)).catch(() => onPlayingChange(false));
    } else {
      audio.pause();
      onPlayingChange(false);
    }
  };

  const progress = track.durationSeconds > 0 ? Math.min(100, (currentTime / track.durationSeconds) * 100) : 0;

  return (
    <div className="global-player" role="region" aria-label="全局播放器">
      <audio
        ref={audioRef}
        preload="metadata"
        onTimeUpdate={(event) => setCurrentTime(event.currentTarget.currentTime)}
        onPlay={() => onPlayingChange(true)}
        onPause={() => onPlayingChange(false)}
        onEnded={() => onPlayingChange(false)}
        onError={() => onPlayingChange(false)}
      />
      <button className="global-player-toggle" title={playing ? '暂停播放' : '继续播放'} onClick={toggle}>
        {playing ? <Pause size={15} fill="currentColor" /> : <Play size={15} fill="currentColor" />}
      </button>
      <div className="global-player-copy">
        <strong>{track.title || track.fileName}</strong>
        <span>{track.artists.join(' / ') || '未知艺术家'} · {track.album || '未知专辑'}</span>
      </div>
      <div className="global-player-progress" aria-label="播放进度">
        <i style={{width: `${progress}%`}} />
      </div>
      <span className="global-player-time">{formatDuration(Math.round(currentTime))} / {formatDuration(track.durationSeconds)}</span>
      <button className={cn('icon-button', 'global-player-close')} title="关闭播放器" onClick={onClose}><X size={15} /></button>
    </div>
  );
}
