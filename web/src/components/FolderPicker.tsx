import { useCallback, useEffect, useState } from 'react';
import { Cloud, Folder, FolderPlus, Home, X } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import { Dialog, DialogContent, DialogFooter } from './ui/dialog';
import { Button } from './ui/button';
import { Input } from './ui/input';

interface Props {
    open: boolean;
    title?: string;
    /** 初始目录(相对根) */
    initial?: string;
    /**
     * 浏览根:''(默认)= 整个网盘;'@R2' = 只能在该挂载内选
     * (跨存储移动不支持,把选择范围锁在源存储里)。
     */
    rootPath?: string;
    /** 隐藏 @ 挂载点(离线下载只能落本机时用) */
    hideMounts?: boolean;
    /** 允许在当前目录创建并选中新文件夹 */
    allowCreate?: boolean;
    onClose: () => void;
    onSelect: (dir: string) => void;
}

/** 浏览网盘现有文件夹并选择一个;默认根目录 */
export default function FolderPicker({
    open,
    title = '选择文件夹',
    initial = '',
    rootPath = '',
    hideMounts = false,
    allowCreate = false,
    onClose,
    onSelect,
}: Props) {
    const [path, setPath] = useState(initial);
    const [dirs, setDirs] = useState<string[]>([]);
    const [loading, setLoading] = useState(false);
    const [creating, setCreating] = useState(false);
    const [folderName, setFolderName] = useState('');
    const [creatingBusy, setCreatingBusy] = useState(false);

    const load = useCallback(
        (p: string) => {
            setLoading(true);
            api.listFiles(p)
                .then((r) => {
                    setPath(p);
                    setDirs(
                        r.entries
                            .filter((e) => e.dir)
                            .map((e) => e.name)
                            .filter((n) => !(hideMounts && p === '' && n.startsWith('@'))),
                    );
                })
                .catch((e) => toast.error(e instanceof Error ? e.message : '加载失败'))
                .finally(() => setLoading(false));
        },
        [hideMounts],
    );

    useEffect(() => {
        if (open) {
            setCreating(false);
            setFolderName('');
            load(initial || rootPath);
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [open]);

    const createFolder = async () => {
        const name = folderName.trim();
        if (!name || creatingBusy) return;
        const nextPath = path === '' ? name : `${path}/${name}`;
        setCreatingBusy(true);
        try {
            await api.mkdir(nextPath);
            setCreating(false);
            setFolderName('');
            load(nextPath);
            toast.success(`已创建文件夹「${name}」`);
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '新建文件夹失败');
        } finally {
            setCreatingBusy(false);
        }
    };

    // 面包屑从浏览根开始(锁定在挂载内时,根就是挂载点)
    const relToRoot = rootPath === '' ? path : path.slice(rootPath.length).replace(/^\//, '');
    const parts = relToRoot === '' ? [] : relToRoot.split('/');
    const isMount = rootPath.startsWith('@');

    return (
        <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
            <DialogContent title={title}>
                <div className="flex items-start gap-2 mb-2">
                    <div className="text-sm text-ink-soft break-all flex-1 min-w-0 py-1.5">
                        <button
                            className="text-leaf-dark font-bold cursor-pointer inline-flex items-center gap-1 align-middle"
                            onClick={() => load(rootPath)}
                        >
                            {isMount ? (
                                <>
                                    <Cloud className="size-3.5" /> {rootPath}
                                </>
                            ) : (
                                <>
                                    <Home className="size-3.5" /> 根目录
                                </>
                            )}
                        </button>
                        {parts.map((seg, i) => {
                            const p =
                                (rootPath ? rootPath + '/' : '') + parts.slice(0, i + 1).join('/');
                            return (
                                <span key={p}>
                                    {' / '}
                                    <button
                                        className="text-leaf-dark font-bold cursor-pointer"
                                        onClick={() => load(p)}
                                    >
                                        {seg}
                                    </button>
                                </span>
                            );
                        })}
                    </div>
                    {allowCreate && !creating && (
                        <Button size="sm" onClick={() => setCreating(true)}>
                            <FolderPlus className="size-4" /> 新建文件夹
                        </Button>
                    )}
                </div>
                {allowCreate && creating && (
                    <div className="flex items-center gap-2 mb-2">
                        <Input
                            autoFocus
                            className="flex-1 min-w-0"
                            value={folderName}
                            placeholder="输入文件夹名称"
                            disabled={creatingBusy}
                            onChange={(e) => setFolderName(e.target.value)}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') createFolder();
                                if (e.key === 'Escape' && !creatingBusy) {
                                    setCreating(false);
                                    setFolderName('');
                                }
                            }}
                        />
                        <Button
                            size="sm"
                            variant="primary"
                            disabled={folderName.trim() === '' || creatingBusy}
                            onClick={createFolder}
                        >
                            {creatingBusy ? '创建中…' : '创建'}
                        </Button>
                        <Button
                            size="icon"
                            variant="ghost"
                            aria-label="取消新建文件夹"
                            disabled={creatingBusy}
                            onClick={() => {
                                setCreating(false);
                                setFolderName('');
                            }}
                        >
                            <X className="size-4" />
                        </Button>
                    </div>
                )}
                <div className="border border-line rounded-2xl p-1 max-h-64 overflow-auto flex flex-col">
                    {loading ? (
                        <div className="text-center text-sm text-ink-soft py-8">加载中…</div>
                    ) : dirs.length === 0 ? (
                        <div className="text-center text-sm text-ink-soft py-8">
                            没有子文件夹,点确定选当前目录
                        </div>
                    ) : (
                        dirs.map((d) => (
                            <button
                                key={d}
                                className="flex items-center gap-2 text-left px-3 py-2 rounded-xl hover:bg-paper-2 text-sm cursor-pointer"
                                onClick={() => load(path === '' ? d : `${path}/${d}`)}
                            >
                                {d.startsWith('@') && path === '' ? (
                                    <Cloud className="size-4 text-sky-600 dark:text-sky-400 shrink-0" />
                                ) : (
                                    <Folder className="size-4 text-amber-600 dark:text-amber-400 fill-amber-600/15 shrink-0" />
                                )}
                                {d}
                            </button>
                        ))
                    )}
                </div>
                <p className="text-sm text-ink-soft mt-2">
                    当前选择:
                    <code className="bg-paper-2 rounded-lg px-2 py-0.5">
                        {path === '' ? '根目录' : path}
                    </code>
                </p>
                <DialogFooter
                    onOk={() => {
                        onSelect(path);
                        onClose();
                    }}
                />
            </DialogContent>
        </Dialog>
    );
}
