import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';
import { api } from '../api';
import type { DownloadTask } from '../api';
import { Card } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Progress, Badge } from '../components/ui/progress';
import FolderPicker from '../components/FolderPicker';
import { formatBytes, formatSpeed, formatTime } from '../util';

const STATUS: Record<string, { text: string; tone: 'green' | 'blue' | 'orange' | 'red' | 'default' }> = {
    active: { text: '下载中', tone: 'blue' },
    waiting: { text: '排队中', tone: 'default' },
    paused: { text: '已暂停', tone: 'orange' },
    complete: { text: '已完成', tone: 'green' },
    error: { text: '出错', tone: 'red' },
    removed: { text: '已移除', tone: 'default' },
};

export default function Downloads() {
    const navigate = useNavigate();
    const [url, setUrl] = useState('');
    const [dir, setDir] = useState('');
    const [pickerOpen, setPickerOpen] = useState(false);
    const [tasks, setTasks] = useState<DownloadTask[]>([]);
    const [degraded, setDegraded] = useState(false);
    const [adding, setAdding] = useState(false);

    const load = useCallback(() => {
        api.downloads()
            .then((r) => {
                setTasks(r.tasks);
                setDegraded(r.degraded);
            })
            .catch(() => undefined);
    }, []);

    useEffect(() => {
        load();
        const t = setInterval(load, 2000);
        return () => clearInterval(t);
    }, [load]);

    const add = async () => {
        if (!url.trim()) return;
        setAdding(true);
        try {
            await api.addDownload(url.trim(), dir);
            toast.success('任务已添加');
            setUrl('');
            load();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '添加失败');
        } finally {
            setAdding(false);
        }
    };

    const op = async (fn: () => Promise<unknown>) => {
        try {
            await fn();
            load();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '操作失败');
        }
    };

    return (
        <div>
            <div className="flex items-center gap-3 mb-4 flex-wrap">
                <h2 className="text-xl font-extrabold">⬇️ 离线下载</h2>
                {degraded && <Badge tone="red">aria2 不可达,任务暂无法下发</Badge>}
            </div>

            <Card className="mb-4 flex flex-col gap-2.5">
                <Input
                    placeholder="粘贴 http(s)/ftp 直链或磁力链接 magnet:?xt=…"
                    value={url}
                    onChange={(e) => setUrl(e.target.value)}
                />
                <div className="flex items-center gap-2 flex-wrap">
                    <div className="flex items-center gap-2 bg-paper-2 rounded-full px-3.5 py-1.5 min-w-48 flex-1">
                        <span className="text-sm truncate flex-1">
                            📁 {dir === '' ? '根目录' : dir}
                        </span>
                        <Button size="sm" onClick={() => setPickerOpen(true)}>
                            选择目录
                        </Button>
                    </div>
                    <Button variant="primary" disabled={adding} onClick={add}>
                        添加任务
                    </Button>
                </div>
            </Card>

            <FolderPicker
                open={pickerOpen}
                initial={dir}
                onClose={() => setPickerOpen(false)}
                onSelect={setDir}
            />

            {tasks.length === 0 ? (
                <Card className="text-center text-ink-soft py-10 text-sm">
                    暂无下载任务 🍃
                </Card>
            ) : (
                <div className="flex flex-col gap-3">
                    {tasks.map((t) => {
                        const s = STATUS[t.status] ?? { text: t.status, tone: 'default' as const };
                        const pct =
                            t.totalLength > 0
                                ? (t.completedLength / t.totalLength) * 100
                                : 0;
                        return (
                            <Card key={t.gid}>
                                <div className="flex items-center gap-2 mb-2">
                                    <span
                                        className="flex-1 min-w-0 font-bold truncate"
                                        title={t.name || t.url}
                                    >
                                        {t.name || t.url}
                                    </span>
                                    <Badge tone={s.tone}>{s.text}</Badge>
                                </div>
                                {!['complete', 'error', 'removed'].includes(t.status) && (
                                    <Progress percent={pct} />
                                )}
                                <div className="flex gap-3.5 flex-wrap text-xs text-ink-soft mt-2">
                                    <span>
                                        {formatBytes(t.completedLength)} /{' '}
                                        {t.totalLength > 0
                                            ? formatBytes(t.totalLength)
                                            : '未知'}
                                    </span>
                                    {t.speed > 0 && <span>🚀 {formatSpeed(t.speed)}</span>}
                                    <span>📂 {t.dir || '根目录'}</span>
                                    <span>{formatTime(t.createdAt)}</span>
                                </div>
                                {t.errorMsg && (
                                    <div className="text-danger text-xs mt-1.5 break-all">
                                        {t.errorMsg}
                                    </div>
                                )}
                                <div className="flex gap-1.5 mt-2.5">
                                    {t.status === 'active' && (
                                        <Button
                                            size="sm"
                                            onClick={() => op(() => api.pauseDownload(t.gid))}
                                        >
                                            暂停
                                        </Button>
                                    )}
                                    {t.status === 'paused' && (
                                        <Button
                                            size="sm"
                                            onClick={() =>
                                                op(() => api.unpauseDownload(t.gid))
                                            }
                                        >
                                            继续
                                        </Button>
                                    )}
                                    <Button
                                        size="sm"
                                        onClick={() => navigate(`/files/${t.dir}`)}
                                    >
                                        打开目录
                                    </Button>
                                    <Button
                                        variant="ghost-danger"
                                        size="sm"
                                        onClick={() => op(() => api.removeDownload(t.gid))}
                                    >
                                        删除任务
                                    </Button>
                                </div>
                            </Card>
                        );
                    })}
                </div>
            )}
        </div>
    );
}
