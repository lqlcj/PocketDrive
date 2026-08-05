import { useState } from 'react';
import type { CSSProperties } from 'react';
import {
    ChevronDown,
    ChevronUp,
    ListMusic,
    Music,
    Pause,
    Play,
    Repeat,
    Repeat1,
    SkipBack,
    SkipForward,
    Volume2,
    VolumeX,
    X,
} from 'lucide-react';
import { usePlayer } from '../player/store';
import { Button } from './ui/button';
import { cn } from '../lib/utils';

// 右下角的音乐播放器。上传面板在它下面,两个盒子共用一套外观。
//
// 这里只是遥控器,状态全在 player/store 里——面板收起、切页面都不影响
// 播放,只有右上角那个 ✕ 才真的停下来。

function clock(s: number): string {
    if (!Number.isFinite(s) || s < 0) return '0:00';
    const m = Math.floor(s / 60);
    const r = Math.floor(s % 60);
    return `${m}:${String(r).padStart(2, '0')}`;
}

const REPEAT_HINT: Record<string, string> = {
    off: '不循环(放完就停)',
    all: '列表循环',
    one: '单曲循环',
};

export default function MusicPlayer() {
    const {
        queue,
        index,
        current,
        playing,
        time,
        duration,
        repeat,
        volume,
        error,
        playList,
        toggle,
        next,
        prev,
        seek,
        setVolume,
        cycleRepeat,
        close,
    } = usePlayer();
    const [collapsed, setCollapsed] = useState(false);
    const [showQueue, setShowQueue] = useState(false);

    if (!current) return null;

    const pct = duration > 0 ? (time / duration) * 100 : 0;
    const multi = queue.length > 1;

    return (
        <div className="w-[min(92vw,22rem)] bg-paper border border-line/70 rounded-[var(--radius-card)] shadow-lg overflow-hidden">
            {/* 标题条:点歌名收起/展开 */}
            <div className="flex items-center gap-2 px-3 py-2">
                <span className="shrink-0 text-leaf-dark grid place-items-center size-4">
                    {playing ? (
                        <span className="pd-eq" aria-hidden>
                            <i />
                            <i />
                            <i />
                        </span>
                    ) : (
                        <Music className="size-4" />
                    )}
                </span>
                <button
                    className="flex-1 min-w-0 text-left cursor-pointer"
                    title={current.path}
                    onClick={() => setCollapsed((v) => !v)}
                >
                    <div className="text-sm font-bold truncate">{current.name}</div>
                    <div
                        className={cn(
                            'text-[11px] truncate',
                            error ? 'text-danger' : 'text-ink-soft',
                        )}
                    >
                        {error ??
                            (multi
                                ? `${index + 1} / ${queue.length} 首 · ${playing ? '播放中' : '已暂停'}`
                                : playing
                                  ? '播放中'
                                  : '已暂停')}
                    </div>
                </button>
                <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={collapsed ? '展开' : '收起'}
                    title={collapsed ? '展开' : '收起'}
                    onClick={() => setCollapsed((v) => !v)}
                >
                    {collapsed ? (
                        <ChevronUp className="size-4" />
                    ) : (
                        <ChevronDown className="size-4" />
                    )}
                </Button>
                <Button
                    variant="ghost-danger"
                    size="icon-sm"
                    aria-label="关闭播放器"
                    title="关闭播放器"
                    onClick={close}
                >
                    <X className="size-4" />
                </Button>
            </div>

            {/* 收起时留一条细进度,一眼知道放到哪儿了 */}
            {collapsed ? (
                <div className="h-1 bg-paper-2">
                    <div className="h-full bg-leaf" style={{ width: `${pct}%` }} />
                </div>
            ) : (
                <div className="px-3 pb-2">
                    <input
                        type="range"
                        className="pd-range w-full block"
                        style={{ '--pct': `${pct}%` } as CSSProperties}
                        min={0}
                        max={duration > 0 ? duration : 0}
                        step={0.1}
                        value={time}
                        disabled={duration <= 0}
                        aria-label="播放进度"
                        onChange={(e) => seek(Number(e.target.value))}
                    />
                    <div className="flex justify-between text-[11px] text-ink-soft tabular-nums mt-1">
                        <span>{clock(time)}</span>
                        <span>{clock(duration)}</span>
                    </div>

                    <div className="flex items-center gap-0.5 mt-1">
                        <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label="上一首"
                            title="上一首"
                            disabled={!multi && time <= 3}
                            onClick={prev}
                        >
                            <SkipBack className="size-4" />
                        </Button>
                        <Button
                            variant="primary"
                            size="icon-sm"
                            className="rounded-full"
                            aria-label={playing ? '暂停' : '播放'}
                            title={playing ? '暂停' : '播放'}
                            onClick={toggle}
                        >
                            {playing ? (
                                <Pause className="size-3.5" />
                            ) : (
                                <Play className="size-3.5" />
                            )}
                        </Button>
                        <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label="下一首"
                            title="下一首"
                            disabled={!multi}
                            onClick={next}
                        >
                            <SkipForward className="size-4" />
                        </Button>
                        <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label="循环方式"
                            title={REPEAT_HINT[repeat]}
                            className={cn(repeat !== 'off' && 'text-leaf-dark')}
                            onClick={cycleRepeat}
                        >
                            {repeat === 'one' ? (
                                <Repeat1 className="size-4" />
                            ) : (
                                <Repeat className="size-4" />
                            )}
                        </Button>
                        {multi && (
                            <Button
                                variant="ghost"
                                size="icon-sm"
                                aria-label="播放列表"
                                title="播放列表"
                                className={cn(showQueue && 'text-leaf-dark')}
                                onClick={() => setShowQueue((v) => !v)}
                            >
                                <ListMusic className="size-4" />
                            </Button>
                        )}

                        <div className="flex items-center gap-1 ml-auto">
                            <Button
                                variant="ghost"
                                size="icon-sm"
                                aria-label={volume === 0 ? '取消静音' : '静音'}
                                title={volume === 0 ? '取消静音' : '静音'}
                                onClick={() => setVolume(volume === 0 ? 1 : 0)}
                            >
                                {volume === 0 ? (
                                    <VolumeX className="size-4" />
                                ) : (
                                    <Volume2 className="size-4" />
                                )}
                            </Button>
                            <input
                                type="range"
                                className="pd-range w-14"
                                style={{ '--pct': `${volume * 100}%` } as CSSProperties}
                                min={0}
                                max={1}
                                step={0.01}
                                value={volume}
                                aria-label="音量"
                                onChange={(e) => setVolume(Number(e.target.value))}
                            />
                        </div>
                    </div>
                </div>
            )}

            {/* 播放列表:进来时那个目录里的音乐,点一下直接跳过去 */}
            {!collapsed && showQueue && multi && (
                <div className="max-h-40 overflow-y-auto border-t border-line/50">
                    {queue.map((t, i) => (
                        <button
                            key={t.path}
                            className={cn(
                                'w-full text-left px-3 py-1.5 text-xs truncate cursor-pointer hover:bg-paper-2/60 flex items-center gap-2',
                                i === index && 'text-leaf-dark font-bold',
                            )}
                            title={t.path}
                            onClick={() => playList(queue, i)}
                        >
                            <span className="text-ink-soft tabular-nums shrink-0 w-5 text-right">
                                {i + 1}
                            </span>
                            <span className="truncate">{t.name}</span>
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
}
