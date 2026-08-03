import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api';
import type { DiskInfo, DownloadTask, RecentFile, YtdlpTask } from '../api';
import { Card, CardTitle } from '../components/ui/card';
import { Progress } from '../components/ui/progress';
import { fileKind, formatBytes, formatTime, KIND_ICON } from '../util';

export default function Home() {
    const [disk, setDisk] = useState<DiskInfo | null>(null);
    const [recent, setRecent] = useState<RecentFile[]>([]);
    const [dl, setDl] = useState<DownloadTask[]>([]);
    const [yt, setYt] = useState<YtdlpTask[]>([]);

    useEffect(() => {
        const load = () => {
            api.storage()
                .then((r) => {
                    setDisk(r.disk);
                    setRecent(r.recent ?? []);
                })
                .catch(() => undefined);
            api.downloads().then((r) => setDl(r.tasks)).catch(() => undefined);
            api.ytdlp().then((r) => setYt(r.tasks)).catch(() => undefined);
        };
        load();
        const t = setInterval(load, 8000);
        return () => clearInterval(t);
    }, []);

    const active = [
        ...dl
            .filter((t) => ['active', 'waiting', 'paused'].includes(t.status))
            .map((t) => ({
                key: 'a' + t.gid,
                icon: '⬇️',
                name: t.name || t.url,
                pct: t.totalLength > 0 ? (t.completedLength / t.totalLength) * 100 : 0,
            })),
        ...yt
            .filter((t) => ['queued', 'running'].includes(t.status))
            .map((t) => ({
                key: 'y' + t.id,
                icon: '🎬',
                name: t.title || t.url,
                pct: t.progress,
            })),
    ];

    return (
        <div>
            <h2 className="text-xl font-extrabold mb-4">🏝️ 主页</h2>
            <div className="grid md:grid-cols-2 gap-4 items-start">
                <Card>
                    <CardTitle>💾 仓库容量</CardTitle>
                    {disk ? (
                        <>
                            <Progress percent={disk.usedPercent} />
                            <p className="text-sm text-ink-soft mt-2">
                                已用 {formatBytes(disk.used)} / 共 {formatBytes(disk.total)}
                                ,剩余 {formatBytes(disk.free)}
                            </p>
                        </>
                    ) : (
                        <p className="text-sm text-ink-soft">读取中…</p>
                    )}
                    {active.length > 0 && (
                        <div className="mt-3 pt-3 border-t-2 border-dashed border-line flex flex-col gap-1.5">
                            <div className="text-xs font-bold text-ink-soft">进行中的任务</div>
                            {active.slice(0, 5).map((t) => (
                                <div key={t.key} className="flex items-center gap-2 text-sm">
                                    <span>{t.icon}</span>
                                    <span className="flex-1 min-w-0 truncate font-bold">
                                        {t.name}
                                    </span>
                                    <span className="text-leaf-dark font-bold text-xs">
                                        {Math.round(t.pct)}%
                                    </span>
                                </div>
                            ))}
                        </div>
                    )}
                </Card>

                <Card>
                    <CardTitle>🕑 新鲜事</CardTitle>
                    {recent.length === 0 ? (
                        <p className="text-sm text-ink-soft">
                            网盘还空空的,去「我的文件」传点东西吧!
                        </p>
                    ) : (
                        <div className="flex flex-col gap-1.5">
                            {recent.map((f) => (
                                <Link
                                    key={f.path}
                                    to={`/files/${
                                        f.path.includes('/')
                                            ? f.path.slice(0, f.path.lastIndexOf('/'))
                                            : ''
                                    }`}
                                    className="flex items-center gap-2 text-sm hover:bg-paper-2 rounded-xl px-2 py-1 -mx-2"
                                >
                                    <span>{KIND_ICON[fileKind(f.name)]}</span>
                                    <span className="flex-1 min-w-0 truncate font-bold">
                                        {f.name}
                                    </span>
                                    <span className="text-xs text-ink-soft shrink-0">
                                        {formatTime(f.mtime).slice(5)}
                                    </span>
                                </Link>
                            ))}
                        </div>
                    )}
                </Card>
            </div>
        </div>
    );
}
