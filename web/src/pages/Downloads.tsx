import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
    FileUp,
    Folder,
    FolderOpen,
    Rocket,
    Settings2,
} from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { DownloadTask } from '../api';
import { Card } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Input, Checkbox } from '../components/ui/input';
import { Dialog, DialogContent, DialogFooter } from '../components/ui/dialog';
import { Progress, Badge } from '../components/ui/progress';
import FolderPicker from '../components/FolderPicker';
import DownloadFileSelect, {
    type PendingSelect,
} from '../components/DownloadFileSelect';
import { formatBytes, formatSpeed, formatTime } from '../util';

const STATUS: Record<string, { text: string; tone: 'green' | 'blue' | 'orange' | 'red' | 'default' }> = {
    active: { text: '下载中', tone: 'blue' },
    waiting: { text: '排队中', tone: 'default' },
    paused: { text: '已暂停', tone: 'orange' },
    complete: { text: '已完成', tone: 'green' },
    error: { text: '出错', tone: 'red' },
    removed: { text: '已移除', tone: 'default' },
};

/** File → base64(不含 dataURL 前缀) */
function fileToBase64(f: File): Promise<string> {
    return new Promise((resolve, reject) => {
        const r = new FileReader();
        r.onload = () => resolve((r.result as string).split(',')[1] ?? '');
        r.onerror = () => reject(new Error('读取种子文件失败'));
        r.readAsDataURL(f);
    });
}

export default function Downloads() {
    const navigate = useNavigate();
    const [url, setUrl] = useState('');
    const [dir, setDir] = useState('');
    const [pickerOpen, setPickerOpen] = useState(false);
    const [tasks, setTasks] = useState<DownloadTask[]>([]);
    const [degraded, setDegraded] = useState(false);
    const [adding, setAdding] = useState(false);
    const torrentInput = useRef<HTMLInputElement>(null);
    // 删除确认:BT 下完往往是一整个文件夹,删记录还是连文件一起删得问清楚
    const [removeTarget, setRemoveTarget] = useState<DownloadTask | null>(null);
    const [removeFiles, setRemoveFiles] = useState(false);
    // 「选完再下」:上传的种子 / 磁力先挂起,弹框勾选文件后确认才开始
    const [selectQueue, setSelectQueue] = useState<PendingSelect[]>([]);
    const [selectBusy, setSelectBusy] = useState(false);

    const queueSelection = useCallback((item: PendingSelect) => {
        setSelectQueue((q) =>
            q.some((pending) => pending.gid === item.gid) ? q : [...q, item],
        );
    }, []);

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
        // 默认保存目录来自「下载设置」
        api.downloadSettings()
            .then((r) => setDir((d) => (d === '' ? r.settings.defaultDir : d)))
            .catch(() => undefined);
        const t = setInterval(load, 2000);
        return () => clearInterval(t);
    }, [load]);

    const add = async () => {
        const u = url.trim();
        if (!u) return;
        setAdding(true);
        try {
            const r = await api.addDownload(u, dir);
            setUrl('');
            if (u.startsWith('magnet:')) {
                // 磁力:任务已进 aria2 拉元数据,弹框等清单出来再让用户勾选
                queueSelection({ gid: r.task.gid, magnet: true });
            } else {
                toast.success('任务已添加');
            }
            load();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '添加失败');
        } finally {
            setAdding(false);
        }
    };

    const addTorrents = async (files: FileList | null) => {
        if (!files || files.length === 0) return;
        setAdding(true);
        const queue: PendingSelect[] = [];
        try {
            for (const f of Array.from(files)) {
                if (f.size > 12 * 1024 * 1024) {
                    toast.error(`「${f.name}」超过 12MB,不像是种子文件`);
                    continue;
                }
                const b64 = await fileToBase64(f);
                // 先挂起,弹框里勾选完再真正开始下载
                const r = await api.addTorrent(b64, f.name, dir, true);
                queue.push({ gid: r.task.gid, magnet: false });
            }
            if (queue.length > 0) {
                setSelectQueue((q) => [
                    ...q,
                    ...queue.filter((item) => !q.some((pending) => pending.gid === item.gid)),
                ]);
            }
            load();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '种子任务添加失败');
        } finally {
            setAdding(false);
            if (torrentInput.current) torrentInput.current.value = '';
        }
    };

    const confirmSelect = async (indexes: number[]) => {
        const cur = selectQueue[0];
        if (!cur || selectBusy) return;
        setSelectBusy(true);
        try {
            await api.selectDownloadFiles(cur.gid, indexes);
            toast.success(
                indexes.length === 0 ? '任务已开始下载' : `已按所选 ${indexes.length} 个文件开始下载`,
            );
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '操作失败');
        } finally {
            setSelectBusy(false);
            setSelectQueue((q) => q.slice(1));
            load();
        }
    };

    const cancelSelect = async () => {
        const cur = selectQueue[0];
        if (!cur || selectBusy) return;
        setSelectBusy(true);
        try {
            await api.removeDownload(cur.gid, false);
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '移除失败');
        } finally {
            setSelectBusy(false);
            setSelectQueue((q) => q.slice(1));
            load();
        }
    };

    const doRemove = async () => {
        if (!removeTarget) return;
        const withFiles = removeFiles;
        try {
            const r = await api.removeDownload(removeTarget.gid, withFiles);
            toast.success(
                withFiles
                    ? r.deletedFiles > 0
                        ? `任务和 ${r.deletedFiles} 个文件已删除`
                        : '任务已删除(没有找到已下载的文件)'
                    : '任务记录已删除',
            );
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '删除失败');
        } finally {
            setRemoveTarget(null);
            setRemoveFiles(false);
            load();
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
                <h2 className="text-xl font-extrabold">离线下载</h2>
                {degraded && <Badge tone="red">aria2 不可达,任务暂无法下发</Badge>}
                <Link to="/downloads/settings" className="ml-auto">
                    <Button size="sm">
                        <Settings2 className="size-3.5" /> 下载设置
                    </Button>
                </Link>
            </div>

            <Card className="mb-4 flex flex-col gap-2.5">
                <Input
                    placeholder="粘贴 http(s)/ftp 直链或磁力链接 magnet:?xt=…"
                    value={url}
                    onChange={(e) => setUrl(e.target.value)}
                />
                <div className="flex items-center gap-2 flex-wrap">
                    <div className="flex items-center gap-2 bg-paper-2 rounded-full px-3.5 py-1.5 min-w-48 flex-1">
                        <Folder className="size-4 text-ink-soft shrink-0" />
                        <span className="text-sm truncate flex-1">
                            {dir === '' ? '根目录' : dir}
                        </span>
                        <Button size="sm" onClick={() => setPickerOpen(true)}>
                            选择目录
                        </Button>
                    </div>
                    <input
                        ref={torrentInput}
                        type="file"
                        accept=".torrent,application/x-bittorrent"
                        multiple
                        hidden
                        onChange={(e) => addTorrents(e.target.files)}
                    />
                    <Button disabled={adding} onClick={() => torrentInput.current?.click()}>
                        <FileUp className="size-4" /> 上传种子
                    </Button>
                    <Button variant="primary" disabled={adding} onClick={add}>
                        添加任务
                    </Button>
                </div>
                <p className="text-xs text-ink-soft">
                    支持直链 / 磁力,也可以直接上传 .torrent 种子文件(可多选)。
                    种子和磁力会先弹框勾选要下载的文件,确认后才开始。
                </p>
            </Card>

            <FolderPicker
                open={pickerOpen}
                initial={dir}
                hideMounts
                allowCreate
                onClose={() => setPickerOpen(false)}
                onSelect={setDir}
            />

            {tasks.length === 0 ? (
                <Card className="text-center text-ink-soft py-10 text-sm">
                    暂无下载任务
                </Card>
            ) : (
                <div className="flex flex-col gap-3">
                    {tasks.map((t) => {
                        const s = STATUS[t.status] ?? { text: t.status, tone: 'default' as const };
                        const isMagnet = t.url.startsWith('magnet:');
                        // 元数据阶段还没有种子名；真实任务由 pause-metadata
                        // 保持 paused。刷新页面后可从这里重新打开文件选择器。
                        const magnetNeedsSelection =
                            isMagnet &&
                            (t.status === 'paused' || (t.status === 'active' && !t.name));
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
                                <div className="flex gap-3.5 flex-wrap text-xs text-ink-soft mt-2 items-center">
                                    <span>
                                        {formatBytes(t.completedLength)} /{' '}
                                        {t.totalLength > 0
                                            ? formatBytes(t.totalLength)
                                            : '未知'}
                                    </span>
                                    {t.speed > 0 && (
                                        <span className="inline-flex items-center gap-1">
                                            <Rocket className="size-3" />
                                            {formatSpeed(t.speed)}
                                        </span>
                                    )}
                                    <span className="inline-flex items-center gap-1">
                                        <Folder className="size-3" />
                                        {t.dir || '根目录'}
                                    </span>
                                    <span>{formatTime(t.createdAt)}</span>
                                </div>
                                {t.errorMsg && (
                                    <div className="text-danger text-xs mt-1.5 break-all">
                                        {t.errorMsg}
                                    </div>
                                )}
                                <div className="flex gap-1.5 mt-2.5">
                                    {t.status === 'active' && !magnetNeedsSelection && (
                                        <Button
                                            size="sm"
                                            onClick={() => op(() => api.pauseDownload(t.gid))}
                                        >
                                            暂停
                                        </Button>
                                    )}
                                    {t.status === 'paused' && !isMagnet && (
                                        <Button
                                            size="sm"
                                            onClick={() =>
                                                op(() => api.unpauseDownload(t.gid))
                                            }
                                        >
                                            继续
                                        </Button>
                                    )}
                                    {magnetNeedsSelection && (
                                        <Button
                                            size="sm"
                                            onClick={() =>
                                                queueSelection({ gid: t.gid, magnet: true })
                                            }
                                        >
                                            {t.status === 'paused' ? '选择文件并继续' : '获取种子信息'}
                                        </Button>
                                    )}
                                    <Button
                                        size="sm"
                                        onClick={() => navigate(`/files/${t.dir}`)}
                                    >
                                        <FolderOpen className="size-3.5" /> 打开目录
                                    </Button>
                                    <Button
                                        variant="ghost-danger"
                                        size="sm"
                                        onClick={() => {
                                            setRemoveTarget(t);
                                            setRemoveFiles(false);
                                        }}
                                    >
                                        删除任务
                                    </Button>
                                </div>
                            </Card>
                        );
                    })}
                </div>
            )}

            {/* 选完再下:种子/磁力的文件勾选弹框 */}
            {selectQueue.length > 0 && (
                <Dialog
                    open
                    onOpenChange={(o) => {
                        if (!o && !selectBusy) void cancelSelect();
                    }}
                >
                    <DialogContent title="选择要下载的文件" wide>
                        <DownloadFileSelect
                            item={selectQueue[0]!}
                            busy={selectBusy}
                            onConfirm={(indexes) => void confirmSelect(indexes)}
                            onCancel={() => void cancelSelect()}
                        />
                    </DialogContent>
                </Dialog>
            )}

            {/* 删除任务:默认只删记录,勾上才动网盘里的文件 */}
            <Dialog
                open={removeTarget !== null}
                onOpenChange={(o) => {
                    if (!o) {
                        setRemoveTarget(null);
                        setRemoveFiles(false);
                    }
                }}
            >
                <DialogContent title="删除任务">
                    <div className="flex flex-col gap-3">
                        <p className="text-sm">
                            删除「{removeTarget?.name || removeTarget?.url}」这条任务记录。
                        </p>
                        <Checkbox
                            label="同时删除已下载的文件"
                            checked={removeFiles}
                            onChange={(e) => setRemoveFiles(e.target.checked)}
                        />
                        <p className="text-xs text-ink-soft leading-relaxed">
                            {removeFiles ? (
                                <>
                                    保存在「{removeTarget?.dir || '根目录'}
                                    」里的文件会<b>直接删除,不进回收站</b>。
                                    BT 任务连同种子自己建的那层文件夹一起清掉,
                                    没下完的分片文件也会删。
                                </>
                            ) : (
                                <>只删这条记录,已经下好的文件留在网盘里。</>
                            )}
                        </p>
                    </div>
                    <DialogFooter
                        okText={removeFiles ? '删除任务和文件' : '删除记录'}
                        okDanger
                        onOk={doRemove}
                    />
                </DialogContent>
            </Dialog>
        </div>
    );
}
