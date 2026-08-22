import {memo} from 'react';
import {Virtuoso} from 'react-virtuoso';
import {AlertCircle, Check, ChevronDown, ListFilter, MoreHorizontal} from 'lucide-react';
import {CoverArt} from '@/components/CoverArt';
import {artworkURL} from '@/api';
import {cn, formatDuration} from '@/lib/utils';
import type {Track} from '@/types';

interface TrackListProps {
  tracks: Track[];
  showGeneratedCovers?: boolean;
  activeTrackId?: string;
  selectedIds: Set<string>;
  onSelectTrack: (track: Track) => void;
  onToggleTrack: (trackId: string) => void;
  onToggleAll: () => void;
}

const healthLabel: Record<Track['health'], string> = {
  complete: '完整',
  'missing-artwork': '无封面',
  'missing-lyrics': '无歌词',
  'needs-review': '需确认',
  'parse-error': '解析失败',
};

const TrackRow = memo(function TrackRow({
  track,
  showGeneratedCovers = false,
  active,
  selected,
  onSelect,
  onToggle,
}: {
  track: Track;
  showGeneratedCovers?: boolean;
  active: boolean;
  selected: boolean;
  onSelect: () => void;
  onToggle: () => void;
}) {
  return (
    <div
      className={cn('track-row', active && 'is-active', selected && 'is-selected')}
      onClick={onSelect}
      role="row"
      tabIndex={0}
      onKeyDown={(event) => {
        if (event.key === 'Enter') onSelect();
        if (event.key === ' ') {
          event.preventDefault();
          onToggle();
        }
      }}
    >
      <div className="track-check" onClick={(event) => event.stopPropagation()}>
        <button
          className={cn('square-check', selected && 'is-checked')}
          aria-label={selected ? `取消选择 ${track.title}` : `选择 ${track.title}`}
          onClick={onToggle}
        >
          {selected && <Check size={12} strokeWidth={3} />}
        </button>
      </div>
      <CoverArt
        title={track.title || track.fileName}
        artist={track.artists[0]}
        tone={track.coverTone}
        missing={!showGeneratedCovers && track.artworkCount === 0}
		imageUrl={artworkURL(track)}
        size="xs"
      />
      <div className="track-primary">
        <strong>{track.title || '未命名曲目'}</strong>
        <span>{track.fileName}</span>
      </div>
      <div className="track-cell track-artist">{track.artists.join(' / ') || '—'}</div>
      <div className="track-cell track-album">{track.album || '—'}</div>
      <div className="track-cell track-year">{track.year || '—'}</div>
      <div className="track-cell track-format"><span>{track.format.toUpperCase()}</span></div>
      <div className="track-cell track-time">{formatDuration(track.durationSeconds)}</div>
      <div className="track-status">
        {track.health !== 'complete' && (
          <span title={healthLabel[track.health]}><AlertCircle size={14} /></span>
        )}
      </div>
      <button className="row-more" title="更多曲目操作" onClick={(event) => event.stopPropagation()}>
        <MoreHorizontal size={17} />
      </button>
    </div>
  );
});

export function TrackList({
  tracks,
  showGeneratedCovers = false,
  activeTrackId,
  selectedIds,
  onSelectTrack,
  onToggleTrack,
  onToggleAll,
}: TrackListProps) {
  const allSelected = tracks.length > 0 && tracks.every((track) => selectedIds.has(track.id));

  return (
    <div className="track-list" role="table" aria-label="音乐文件列表">
      <div className="track-head" role="row">
        <button
          className={cn('square-check', allSelected && 'is-checked')}
          aria-label={allSelected ? '取消全选' : '全选当前曲目'}
          onClick={onToggleAll}
        >
          {allSelected && <Check size={12} strokeWidth={3} />}
        </button>
        <span className="head-cover">封面</span>
        <button>标题 / 文件名 <ChevronDown size={12} /></button>
        <span>艺术家</span>
        <span>专辑</span>
        <span>年份</span>
        <span>格式</span>
        <span>时长</span>
        <span />
        <button className="column-settings" title="设置显示列"><ListFilter size={15} /></button>
      </div>

      {tracks.length > 0 ? (
        <Virtuoso
          className="track-virtuoso"
          data={tracks}
          itemContent={(_, track) => (
            <TrackRow
              track={track}
              showGeneratedCovers={showGeneratedCovers}
              active={track.id === activeTrackId}
              selected={selectedIds.has(track.id)}
              onSelect={() => onSelectTrack(track)}
              onToggle={() => onToggleTrack(track.id)}
            />
          )}
        />
      ) : (
        <div className="empty-state">
          <span>0</span>
          <strong>没有符合当前条件的曲目</strong>
          <p>调整筛选或搜索关键词后再试。</p>
        </div>
      )}
    </div>
  );
}
