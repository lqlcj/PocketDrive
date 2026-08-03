import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api';
import type { DiskInfo, DownloadTask, RecentFile, YtdlpTask } from '../api';
import { Card } from '../components/ui/card';
import { Progress } from '../components/ui/progress';
import { fileKind, formatBytes, formatTime, KIND_ICON } from '../util';

interface Building {
    to: string;
    emoji: string;
    name: string;
    desc: (stats: Record<string, number>) => string;
}

const BUILDINGS: Building[] = [
    {
        to: '/files',
        emoji: '🏠',
        name: '小屋',
        desc: (s) => `我的文件 · ${s['file'] ?? 0} 个`,
    },
    {
        to: '/downloads',
        emoji: '📦',
        name: '储藏室',
        desc: () => '离线下载 · yt下载',
    },
    {
        to: '/gallery',
        emoji: '🖼️',
        name: '展览馆',
        desc: (s) => `照片 · ${s['image'] ?? 0} 张`,
    },
    {
        to: '/music',
        emoji: '🎵',
        name: '留声机',
        desc: (s) => `音乐 · ${s['audio'] ?? 0} 首`,
    },
    {
        to: '/cinema',
        emoji: '📺',
        name: '影院',
        desc: (s) => `视频 · ${s['video'] ?? 0} 部`,
    },
    {
        to: '/notes',
        emoji: '📔',
        name: '笔记本',
        desc: (s) => `笔记 · ${s['markdown'] ?? 0} 篇`,
    },
];

export default function Home() {
    const [disk, setDisk] = useState<DiskInfo | null>(null);
    const [recent, setRecent] = useState<RecentFile[]>([]);
    const [stats, setStats] = useState<Record<string, number>>({});
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
            api.stats()
                .then((r) => setStats(r.stats))
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
            <div className="text-center mb-8 mt-2">
                <h1 className="text-3xl font-extrabold">🌴 PocketDrive 小岛</h1>
                <p className="text-ink-soft mt-1 text-sm">欢迎回来,今天想去哪儿逛逛?</p>
            </div>

            {/* 岛上建筑 */}
            <div className="grid grid-cols-2 sm:grid-cols-3 gap-4">
                {BUILDINGS.map((b) => (
                    <Link key={b.to} to={b.to}>
                        <Card className="text-center py-6 hover:border-leaf hover:-translate-y-0.5 transition-all cursor-pointer">
                            <div className="text-4xl">{b.emoji}</div>
                            <div className="font-extrabold mt-2">{b.name}</div>
                            <div className="text-xs text-ink-soft mt-0.5">{b.desc(stats)}</div>
                        </Card>
                    </Link>
                ))}
            </div>

            <div className="grid md:grid-cols-2 gap-4 mt-4">
                {/* 存储 */}
                <Card>
                    <h3 className="font-extrabold mb-3">💾 岛屿仓库</h3>
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
                            {active.slice(0, 4).map((t) => (
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

                {/* 最近 */}
                <Card>
                    <h3 className="font-extrabold mb-3">🕑 新鲜事</h3>
                    {recent.length === 0 ? (
                        <p className="text-sm text-ink-soft">
                            岛上还空空的,去小屋传点东西吧!
                        </p>
                    ) : (
                        <div className="flex flex-col gap-1.5">
                            {recent.slice(0, 6).map((f) => (
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
