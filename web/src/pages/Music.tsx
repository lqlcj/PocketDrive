import { useEffect, useRef, useState } from 'react';
import { api } from '../api';
import type { IndexItem } from '../api';
import { Card } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { formatBytes } from '../util';
import { cn } from '../lib/utils';

/** 留声机:全岛音乐,点一首连播 */
export default function Music() {
    const [items, setItems] = useState<IndexItem[]>([]);
    const [loading, setLoading] = useState(true);
    const [current, setCurrent] = useState<number | null>(null);
    const audioRef = useRef<HTMLAudioElement>(null);

    useEffect(() => {
        api.category('audio')
            .then((r) => setItems(r.items))
            .catch(() => undefined)
            .finally(() => setLoading(false));
    }, []);

    const play = (i: number) => {
        setCurrent(i);
        // src 变更后自动播放由 audio 的 autoPlay 处理
    };

    const next = () => {
        if (current === null) return;
        if (current < items.length - 1) play(current + 1);
    };

    const cur = current !== null ? items[current] : null;

    return (
        <div className={cn(cur && 'pb-24')}>
            <div className="flex items-center gap-3 mb-4">
                <h2 className="text-xl font-extrabold">🎵 留声机</h2>
                <span className="text-sm text-ink-soft">{items.length} 首曲子</span>
            </div>
            {loading ? (
                <Card className="text-center text-ink-soft py-10 text-sm">加载中…</Card>
            ) : items.length === 0 ? (
                <Card className="text-center text-ink-soft py-10 text-sm">
                    留声机还没有唱片,传几首歌来听听 🎶
                </Card>
            ) : (
                <Card className="p-0 overflow-hidden">
                    {items.map((it, i) => (
                        <button
                            key={it.path}
                            onClick={() => play(i)}
                            className={cn(
                                'flex items-center gap-3 w-full px-4 py-2.5 text-left border-b border-dashed border-line last:border-b-0 cursor-pointer hover:bg-paper-2/60',
                                current === i && 'bg-leaf-soft',
                            )}
                        >
                            <span className="text-lg">{current === i ? '🎶' : '🎵'}</span>
                            <span className="flex-1 min-w-0">
                                <span
                                    className={cn(
                                        'block font-bold text-sm truncate',
                                        current === i && 'text-leaf-dark',
                                    )}
                                >
                                    {it.name}
                                </span>
                                <span className="block text-xs text-ink-soft truncate">
                                    {it.path}
                                </span>
                            </span>
                            <span className="text-xs text-ink-soft shrink-0">
                                {formatBytes(it.size)}
                            </span>
                        </button>
                    ))}
                </Card>
            )}

            {/* 底部播放条 */}
            {cur && (
                <div className="fixed bottom-0 left-0 right-0 z-40 bg-paper border-t-2 border-dashed border-line">
                    <div className="max-w-5xl mx-auto px-4 py-2.5 flex items-center gap-3">
                        <span className="text-xl shrink-0">🎶</span>
                        <span className="font-bold text-sm truncate max-w-[30%]">
                            {cur.name}
                        </span>
                        {/* eslint-disable-next-line jsx-a11y/media-has-caption */}
                        <audio
                            ref={audioRef}
                            src={api.downloadUrl(cur.path)}
                            controls
                            autoPlay
                            onEnded={next}
                            className="flex-1 h-9"
                        />
                        <Button size="sm" onClick={() => setCurrent(null)}>
                            收起
                        </Button>
                    </div>
                </div>
            )}
        </div>
    );
}
