import { useEffect, useState } from 'react';
import { api } from '../api';
import type { IndexItem } from '../api';
import { Card } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Dialog, DialogContent } from '../components/ui/dialog';
import { formatBytes, formatTime } from '../util';

/** 展览馆:全岛照片墙 */
export default function Gallery() {
    const [items, setItems] = useState<IndexItem[]>([]);
    const [loading, setLoading] = useState(true);
    const [idx, setIdx] = useState<number | null>(null);

    useEffect(() => {
        api.category('image')
            .then((r) => setItems(r.items))
            .catch(() => undefined)
            .finally(() => setLoading(false));
    }, []);

    const cur = idx !== null ? items[idx] : null;

    return (
        <div>
            <div className="flex items-center gap-3 mb-4">
                <h2 className="text-xl font-extrabold">🖼️ 展览馆</h2>
                <span className="text-sm text-ink-soft">{items.length} 张照片</span>
            </div>
            {loading ? (
                <Card className="text-center text-ink-soft py-10 text-sm">加载中…</Card>
            ) : items.length === 0 ? (
                <Card className="text-center text-ink-soft py-10 text-sm">
                    展览馆还是空的,传几张照片来布展吧 🖼️
                </Card>
            ) : (
                <div className="grid grid-cols-3 sm:grid-cols-4 md:grid-cols-5 gap-2">
                    {items.map((it, i) => (
                        <button
                            key={it.path}
                            className="aspect-square rounded-xl overflow-hidden border-2 border-line hover:border-leaf cursor-pointer bg-paper-2"
                            onClick={() => setIdx(i)}
                            title={it.path}
                        >
                            <img
                                src={api.thumbUrl(it.path)}
                                alt={it.name}
                                loading="lazy"
                                className="w-full h-full object-cover"
                            />
                        </button>
                    ))}
                </div>
            )}

            {cur && idx !== null && (
                <Dialog open onOpenChange={(o) => !o && setIdx(null)}>
                    <DialogContent title={cur.name} wide>
                        <div className="text-center">
                            <img
                                src={api.downloadUrl(cur.path)}
                                alt={cur.name}
                                className="max-w-full max-h-[65vh] rounded-xl inline-block"
                            />
                            <div className="flex items-center justify-center gap-3 mt-3 text-sm text-ink-soft">
                                <Button
                                    size="sm"
                                    disabled={idx <= 0}
                                    onClick={() => setIdx(idx - 1)}
                                >
                                    ← 上一张
                                </Button>
                                <span>
                                    {idx + 1} / {items.length} · {formatBytes(cur.size)} ·{' '}
                                    {formatTime(cur.mtime)}
                                </span>
                                <Button
                                    size="sm"
                                    disabled={idx >= items.length - 1}
                                    onClick={() => setIdx(idx + 1)}
                                >
                                    下一张 →
                                </Button>
                            </div>
                        </div>
                    </DialogContent>
                </Dialog>
            )}
        </div>
    );
}
