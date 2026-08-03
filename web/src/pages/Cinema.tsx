import { useEffect, useState } from 'react';
import { api } from '../api';
import type { IndexItem } from '../api';
import { Card } from '../components/ui/card';
import { Dialog, DialogContent } from '../components/ui/dialog';
import { browserPlayable, formatBytes, formatTime } from '../util';

/** 影院:全岛视频墙 */
export default function Cinema() {
    const [items, setItems] = useState<IndexItem[]>([]);
    const [loading, setLoading] = useState(true);
    const [cur, setCur] = useState<IndexItem | null>(null);

    useEffect(() => {
        api.category('video')
            .then((r) => setItems(r.items))
            .catch(() => undefined)
            .finally(() => setLoading(false));
    }, []);

    return (
        <div>
            <div className="flex items-center gap-3 mb-4">
                <h2 className="text-xl font-extrabold">📺 影院</h2>
                <span className="text-sm text-ink-soft">{items.length} 部影片</span>
            </div>
            {loading ? (
                <Card className="text-center text-ink-soft py-10 text-sm">加载中…</Card>
            ) : items.length === 0 ? (
                <Card className="text-center text-ink-soft py-10 text-sm">
                    影院还没排片,去 yt下载 或上传几部视频吧 🎬
                </Card>
            ) : (
                <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3">
                    {items.map((it) => (
                        <button
                            key={it.path}
                            className="rounded-2xl overflow-hidden border-2 border-line hover:border-leaf cursor-pointer bg-paper text-left"
                            onClick={() => setCur(it)}
                            title={it.path}
                        >
                            <div className="aspect-video bg-paper-2 relative">
                                <img
                                    src={api.thumbUrl(it.path)}
                                    alt=""
                                    loading="lazy"
                                    className="w-full h-full object-cover"
                                    onError={(e) => {
                                        (e.target as HTMLImageElement).style.display = 'none';
                                    }}
                                />
                                <span className="absolute inset-0 flex items-center justify-center text-3xl pointer-events-none drop-shadow">
                                    ▶️
                                </span>
                            </div>
                            <div className="px-2.5 py-1.5">
                                <div className="font-bold text-xs truncate">{it.name}</div>
                                <div className="text-[11px] text-ink-soft">
                                    {formatBytes(it.size)} · {formatTime(it.mtime).slice(0, 10)}
                                </div>
                            </div>
                        </button>
                    ))}
                </div>
            )}

            {cur && (
                <Dialog open onOpenChange={(o) => !o && setCur(null)}>
                    <DialogContent title={cur.name} wide>
                        {browserPlayable(cur.name) ? (
                            // eslint-disable-next-line jsx-a11y/media-has-caption
                            <video
                                src={api.downloadUrl(cur.path)}
                                controls
                                autoPlay
                                className="w-full max-h-[70vh] rounded-xl bg-black"
                            />
                        ) : (
                            <p className="text-sm py-4">
                                此格式浏览器无法直接播放,请{' '}
                                <a
                                    className="text-leaf-dark underline"
                                    href={api.downloadUrl(cur.path, true)}
                                    download
                                >
                                    下载
                                </a>{' '}
                                后本地观看。
                            </p>
                        )}
                    </DialogContent>
                </Dialog>
            )}
        </div>
    );
}
