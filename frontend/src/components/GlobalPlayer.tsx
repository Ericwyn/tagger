import {useEffect, useRef, useState} from 'react';
import {Pause, Play, Repeat1, X} from 'lucide-react';
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
  const [singleLoop, setSingleLoop] = useState(false);
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
    if (audioRef.current) audioRef.current.loop = singleLoop;
  }, [singleLoop]);

  useEffect(() => {
    const audio = audioRef.current;
    if (!audio || !source) return;
    if (playing && audio.paused) {
      void audio.play().catch(() => onPlayingChange(false));
    } else if (!playing && !audio.paused) {
      audio.pause();
    }
  }, [playing, source, onPlayingChange]);

  const toggle = () => {
    const audio = audioRef.current;
    if (!track || !source || !audio) {
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

  const seek = (value: number) => {
    setCurrentTime(value);
    if (audioRef.current && track) audioRef.current.currentTime = value;
  };

  return (
    <div className="global-player" role="region" aria-label="全局播放器">
      <audio
        ref={audioRef}
        preload="metadata"
        onTimeUpdate={(event) => setCurrentTime(event.currentTarget.currentTime)}
        onPlay={() => onPlayingChange(true)}
        onPause={() => onPlayingChange(false)}
        onEnded={() => onPlayingChange(singleLoop ? true : false)}
        onError={() => onPlayingChange(false)}
      />
      <button className="global-player-toggle" disabled={!track} title={track ? (playing ? '暂停播放' : '继续播放') : '暂无播放歌曲'} onClick={toggle}>
        {playing ? <Pause size={15} fill="currentColor" /> : <Play size={15} fill="currentColor" />}
      </button>
      <div className="global-player-copy">
        <strong>{track ? (track.title || track.fileName) : '无播放歌曲'}</strong>
        <span>{track ? `${track.artists.join(' / ') || '未知艺术家'} · ${track.album || '未知专辑'}` : '从曲库选择一首歌曲开始播放'}</span>
      </div>
      <input
        className="global-player-progress"
        type="range"
        min={0}
        max={track?.durationSeconds || 0}
        step={0.1}
        value={track ? Math.min(currentTime, track.durationSeconds || 0) : 0}
        aria-label="播放进度"
        disabled={!track || track.durationSeconds <= 0}
        onChange={(event) => seek(Number(event.target.value))}
      />
      <span className="global-player-time">{track ? `${formatDuration(Math.round(currentTime))} / ${formatDuration(track.durationSeconds)}` : '— / —'}</span>
      <button className={cn('icon-button', 'global-player-loop', singleLoop && 'is-active')} disabled={!track} aria-pressed={singleLoop} title={singleLoop ? '关闭单曲循环' : '开启单曲循环'} onClick={() => setSingleLoop((value) => !value)}><Repeat1 size={16} /></button>
      <button className={cn('icon-button', 'global-player-close')} disabled={!track} title="关闭播放器" onClick={onClose}><X size={15} /></button>
    </div>
  );
}
