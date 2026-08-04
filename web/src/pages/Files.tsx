import { Fragment, useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import {
    ChevronDown,
    Cloud,
    Download,
    HardDrive,
    Home,
    LayoutGrid,
    List,
    MoreHorizontal,
    RefreshCw,
    Upload,
} from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { ArchiveTask, FileEntry, Share, StoragePolicy } from '../api';
import { extractable, fileKind, formatBytes, formatTime, shareLink } from '../util';
import Preview from '../components/Preview';
import FolderPicker from '../components/FolderPicker';
import FileTree from '../components/FileTree';
import KindIcon, { EntryIcon } from '../components/KindIcon';
import { Card } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Input, NativeSelect } from '../components/ui/input';
import { Dialog, DialogContent, DialogFooter } from '../components/ui/dialog';
import {
    ContextMenu,
    ContextMenuContent,
    ContextMenuItem,
    ContextMenuSeparator,
    ContextMenuTrigger,
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from '../components/ui/dropdown-menu';
import { Progress } from '../components/ui/progress';
import { useUploads } from '../upload/store';
import { cn } from '../lib/utils';

type ViewMode = 'list' | 'grid';

/** 行操作项:同时喂给「⋯」下拉菜单和右键菜单 */
type RowAction = {
    label: string;
    run: () => void;
    danger?: boolean;
    sepBefore?: boolean;
};

const DND_TYPE = 'application/x-pd-path';

// 每页条数按视图分开:列表一行一个,50 行一屏滚两下就到底;
// 缩略图是 2-4 列的网格,24 个正好铺满 6 行且不留半行空白
const PAGE_LIST = 50;
const PAGE_GRID = 24;

const FOLDER_EMOJIS = [
    '📁', '🎵', '🎬', '🖼️', '📔', '💼', '🎮', '📚',
    '🍱', '🧸', '🌟', '💾', '🔧', '🏠', '❤️', '🎁',
    '✈️', '📷', '🎨', '🏫', '👶', '🐾', '🔒', '🗃️',
];

function Breadcrumb({ path }: { path: string }) {
    const parts = path === '' ? [] : path.split('/');
    let acc = '';
    // 第一段是 @挂载名时,整条路径都属于那个外部存储——用不同颜色标出来,
    // 免得在两个存储之间来回切时看错地方
    const inMount = parts.length > 0 && parts[0].startsWith('@');
    return (
        <div
            className={cn(
                'text-sm break-all mb-3 rounded-lg px-2 py-1 -mx-2 flex items-center gap-1 flex-wrap',
                inMount
                    ? 'bg-sky-500/10 text-sky-800 dark:text-sky-200'
                    : 'text-ink-soft',
            )}
        >
            {inMount ? (
                <Cloud className="size-3.5 text-sky-600 dark:text-sky-400 shrink-0" />
            ) : (
                <HardDrive className="size-3.5 text-ink-soft shrink-0" />
            )}
            <Link
                className={cn(
                    'font-bold inline-flex items-center gap-1 align-middle',
                    inMount ? 'text-sky-700 dark:text-sky-300' : 'text-leaf-dark',
                )}
                to="/files"
            >
                <Home className="size-3.5" /> 根目录
            </Link>
            {parts.map((p, i) => {
                acc += (i === 0 ? '' : '/') + p;
                return (
                    <span key={acc}>
                        {' / '}
                        <Link
                            className={cn(
                                'font-bold',
                                inMount ? 'text-sky-700 dark:text-sky-300' : 'text-leaf-dark',
                            )}
                            to={`/files/${acc}`}
                        >
                            {p}
                        </Link>
                    </span>
                );
            })}
            {inMount && (
                <span className="text-xs opacity-80 ml-1">· 外部存储</span>
            )}
        </div>
    );
}

function Thumb({
    path,
    name,
    dir,
    dirIcon,
}: {
    path: string;
    name: string;
    dir: boolean;
    dirIcon?: string;
}) {
    const kind = fileKind(name, dir);
    const [failed, setFailed] = useState(false);
    // 外部存储不生成缩略图,直接出图标
    const inMount = path.startsWith('@');
    if (!dir && !inMount && (kind === 'image' || kind === 'video') && !failed) {
        return (
            <img
                className="w-full h-28 object-cover bg-paper-2"
                src={api.thumbUrl(path)}
                alt={name}
                loading="lazy"
                onError={() => setFailed(true)}
            />
        );
    }
    if (dir && name.startsWith('@')) {
        return (
            <div className="w-full h-28 flex items-center justify-center bg-paper-2">
                <Cloud className="size-10 text-sky-600 dark:text-sky-400" />
            </div>
        );
    }
    return (
        <div className="w-full h-28 flex items-center justify-center bg-paper-2">
            <EntryIcon
                kind={kind}
                custom={dir ? dirIcon : undefined}
                className="size-10"
                emojiClassName="text-4xl"
            />
        </div>
    );
}

export default function Files() {
    const params = useParams();
    const path = params['*'] ?? '';
    const navigate = useNavigate();
    const { add: addUploads, completedTick } = useUploads();

    // 当前所在存储:''=本机,'@R2'=挂载
    const mountName = path.startsWith('@') ? path.split('/')[0]! : '';
    const inMount = mountName !== '';

    const [entries, setEntries] = useState<FileEntry[]>([]);
    const [page, setPage] = useState(1);
    const [loading, setLoading] = useState(true);

    const fileInput = useRef<HTMLInputElement>(null);
    const dirInput = useRef<HTMLInputElement>(null);
    const [view, setView] = useState<ViewMode>(
        () => (localStorage.getItem('pd_view') as ViewMode) || 'list',
    );
    const [treeVersion, setTreeVersion] = useState(0);
    const [icons, setIcons] = useState<Record<string, string>>({});
    const [policies, setPolicies] = useState<StoragePolicy[]>([]);

    const [mkdirOpen, setMkdirOpen] = useState(false);
    const [mkdirName, setMkdirName] = useState('');
    const [noteOpen, setNoteOpen] = useState(false);
    const [noteName, setNoteName] = useState('');
    const [renameTarget, setRenameTarget] = useState<FileEntry | null>(null);
    const [renameName, setRenameName] = useState('');
    const [moveTarget, setMoveTarget] = useState<FileEntry | null>(null);
    const [deleteTarget, setDeleteTarget] = useState<FileEntry | null>(null);
    const [iconTarget, setIconTarget] = useState<FileEntry | null>(null);
    const [previewIdx, setPreviewIdx] = useState<number | null>(null);

    const [shareTarget, setShareTarget] = useState<FileEntry | null>(null);
    const [sharePass, setSharePass] = useState('');
    const [shareType, setShareType] = useState('page');
    const [shareExpire, setShareExpire] = useState('0');
    const [shareResult, setShareResult] = useState<Share | null>(null);

    const [dragOver, setDragOver] = useState(false);
    const [dropTarget, setDropTarget] = useState<string | null>(null);

    // 压缩/解压:异步任务,起完轮询进度
    const [compressTarget, setCompressTarget] = useState<FileEntry | null>(null);
    const [compressName, setCompressName] = useState('');
    const [compressFormat, setCompressFormat] = useState('zip');
    const [extractTarget, setExtractTarget] = useState<FileEntry | null>(null);
    const [archiveTask, setArchiveTask] = useState<ArchiveTask | null>(null);

    const loadIcons = useCallback(() => {
        api.icons()
            .then((r) => setIcons(r.icons))
            .catch(() => undefined);
    }, []);

    const load = useCallback(() => {
        setLoading(true);
        api.listFiles(path)
            .then((r) => setEntries(r.entries))
            .catch((e) => {
                toast.error(e instanceof Error ? e.message : '加载失败');
                setEntries([]);
            })
            .finally(() => setLoading(false));
        setTreeVersion((v) => v + 1);
    }, [path]);

    useEffect(load, [load]);
    useEffect(loadIcons, [loadIcons]);
    useEffect(() => {
        api.storages()
            .then((r) => setPolicies(r.policies))
            .catch(() => undefined);
    }, []);

    // 有文件传完就刷新列表(上传面板在别的页面也可能传完)
    useEffect(() => {
        if (completedTick > 0) load();
    }, [completedTick, load]);

    // 换目录、换视图都回到第一页,否则会停在一个空页上
    useEffect(() => setPage(1), [path, view]);

    const join = (name: string) => (path === '' ? name : `${path}/${name}`);

    const switchView = (v: ViewMode) => {
        setView(v);
        localStorage.setItem('pd_view', v);
    };

    const pageSize = view === 'list' ? PAGE_LIST : PAGE_GRID;
    const pageCount = Math.max(1, Math.ceil(entries.length / pageSize));
    const curPage = Math.min(page, pageCount);
    const pageStart = (curPage - 1) * pageSize;
    const shown = entries.slice(pageStart, pageStart + pageSize);


    // 压缩/解压是后台任务:起完轮询到结束,完成后刷新列表
    const pollArchive = useCallback(
        (id: number) => {
            const timer = setInterval(async () => {
                try {
                    const r = await api.archiveTasks();
                    const t = r.tasks.find((x) => x.id === id);
                    if (!t) return;
                    setArchiveTask(t);
                    if (t.status === 'running') return;
                    clearInterval(timer);
                    if (t.status === 'done') {
                        toast.success(t.kind === 'compress' ? '压缩完成' : '解压完成');
                        load();
                    } else {
                        toast.error(t.errorMsg || '任务失败');
                    }
                    setTimeout(() => setArchiveTask(null), 2500);
                } catch {
                    clearInterval(timer);
                    setArchiveTask(null);
                }
            }, 1000);
        },
        [load],
    );

    const doCompress = async () => {
        if (!compressTarget) return;
        const name = compressName.trim();
        if (!name) {
            toast.warning('请填写压缩包名称');
            return;
        }
        try {
            const r = await api.compress([join(compressTarget.name)], join(name), compressFormat);
            setCompressTarget(null);
            setArchiveTask(r.task);
            pollArchive(r.task.id);
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '压缩失败');
        }
    };

    const doExtract = async () => {
        if (!extractTarget) return;
        try {
            const r = await api.extract(join(extractTarget.name), path);
            setExtractTarget(null);
            setArchiveTask(r.task);
            pollArchive(r.task.id);
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '解压失败');
        }
    };

    // 上传交给右下角的上传面板:排队、进度、暂停/续传都在那边
    const onUpload = (fileList: FileList | null) => {
        if (!fileList || fileList.length === 0) return;
        addUploads(Array.from(fileList), path);
        if (fileInput.current) fileInput.current.value = '';
        if (dirInput.current) dirInput.current.value = '';
    };

    const run = async (fn: () => Promise<unknown>, close?: () => void) => {
        try {
            await fn();
            close?.();
            load();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '操作失败');
        }
    };

    // 文件夹改名/移动时迁移自定义图标
    const migrateIcon = async (oldPath: string, newPath: string) => {
        const icon = icons[oldPath];
        if (icon) {
            await api.setIcon(newPath, icon).catch(() => undefined);
            await api.setIcon(oldPath, '').catch(() => undefined);
            loadIcons();
        }
    };

    const doMoveTo = (src: string, destDir: string) =>
        run(async () => {
            await api.move(src, destDir);
            const base = src.includes('/') ? src.slice(src.lastIndexOf('/') + 1) : src;
            await migrateIcon(src, destDir === '' ? base : `${destDir}/${base}`);
            toast.success('已移动');
        });

    const doShare = async () => {
        if (!shareTarget) return;
        try {
            const r = await api.createShare(
                join(shareTarget.name),
                sharePass,
                shareType,
                parseInt(shareExpire, 10),
            );
            setShareResult(r.share);
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '创建分享失败');
        }
    };

    const copyText = async (text: string) => {
        try {
            await navigator.clipboard.writeText(text);
            toast.success('已复制到剪贴板');
        } catch {
            toast.warning('复制失败,请手动复制');
        }
    };

    const openEntry = (e: FileEntry, idx: number) => {
        if (e.dir) navigate(`/files/${join(e.name)}`);
        else setPreviewIdx(idx);
    };

    const closeShare = () => {
        setShareTarget(null);
        setSharePass('');
        setShareType('page');
        setShareExpire('0');
        setShareResult(null);
    };

    const newNote = () => {
        const name = noteName.trim();
        if (!name) return;
        const full = join(name.endsWith('.md') ? name : `${name}.md`);
        run(
            async () => {
                await api.writeFile(full, `# ${name.replace(/\.md$/, '')}\n\n`);
                navigate(`/note/${full}`);
            },
            () => setNoteOpen(false),
        );
    };

    // ---- 行内拖拽移动 ----
    const onRowDragStart = (e: React.DragEvent, entry: FileEntry) => {
        e.dataTransfer.setData(DND_TYPE, join(entry.name));
        e.dataTransfer.effectAllowed = 'move';
    };
    const onFolderDrop = (e: React.DragEvent, folder: FileEntry) => {
        e.preventDefault();
        e.stopPropagation();
        setDropTarget(null);
        setDragOver(false);
        const src = e.dataTransfer.getData(DND_TYPE);
        if (!src) return;
        const dest = join(folder.name);
        if (src === dest) return;
        // 跨存储拖拽:后端不支持,前端直接拦下给出提示
        const srcMount = src.startsWith('@') ? src.split('/')[0] : '';
        const destMount = dest.startsWith('@') ? dest.split('/')[0] : '';
        if (srcMount !== destMount) {
            toast.warning('暂不支持跨存储移动,请下载后再上传到目标存储');
            return;
        }
        doMoveTo(src, dest);
    };

    const entryIcon = (e: FileEntry) => {
        if (e.dir && e.name.startsWith('@')) {
            return <Cloud className="size-[18px] text-sky-600 dark:text-sky-400 shrink-0" />;
        }
        return (
            <EntryIcon
                kind={fileKind(e.name, e.dir)}
                custom={e.dir ? icons[join(e.name)] : undefined}
                className="size-[18px]"
                emojiClassName="text-lg"
            />
        );
    };

    // 根目录里的 @挂载点是策略的化身:只能打开,管理去「存储策略」页
    const isMountRoot = (e: FileEntry) => path === '' && e.dir && e.name.startsWith('@');

    // 一行只把「下载」留在外面,其余收进菜单——同一份定义既供「⋯」
    // 下拉使用,也供行上的右键菜单使用
    const rowActions = (e: FileEntry): RowAction[] => {
        if (isMountRoot(e)) {
            return [{ label: '管理存储策略', run: () => navigate('/storage') }];
        }
        const list: RowAction[] = [];
        if (e.dir) {
            list.push({ label: '设置图标', run: () => setIconTarget(e) });
        } else {
            list.push({ label: '分享', run: () => setShareTarget(e) });
        }
        list.push({
            label: '重命名',
            run: () => {
                setRenameTarget(e);
                setRenameName(e.name);
            },
        });
        list.push({ label: '移动到…', run: () => setMoveTarget(e) });
        list.push({
            label: '压缩为…',
            run: () => {
                setCompressTarget(e);
                setCompressName(`${e.name}.zip`);
                setCompressFormat('zip');
            },
            sepBefore: true,
        });
        if (!e.dir && extractable(e.name)) {
            list.push({ label: '解压到此处', run: () => setExtractTarget(e) });
        }
        list.push({
            label: '删除',
            run: () => setDeleteTarget(e),
            danger: true,
            sepBefore: true,
        });
        return list;
    };

    const actions = (e: FileEntry) => (
        <span className="flex gap-0.5 shrink-0 items-center">
            {!e.dir && !isMountRoot(e) && (
                <a
                    href={api.downloadUrl(join(e.name), true)}
                    download={e.name}
                    title="下载"
                >
                    <Button variant="ghost" size="icon-sm" aria-label="下载">
                        <Download className="size-3.5" />
                    </Button>
                </a>
            )}
            <DropdownMenu>
                <DropdownMenuTrigger asChild>
                    <Button variant="ghost" size="icon-sm" aria-label="更多操作">
                        <MoreHorizontal className="size-4" />
                    </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent>
                    {rowActions(e).map((a) => (
                        <Fragment key={a.label}>
                            {a.sepBefore && <DropdownMenuSeparator />}
                            <DropdownMenuItem danger={a.danger} onSelect={a.run}>
                                {a.label}
                            </DropdownMenuItem>
                        </Fragment>
                    ))}
                </DropdownMenuContent>
            </DropdownMenu>
        </span>
    );

    return (
        <div
            onDragOver={(e) => {
                e.preventDefault();
                if (e.dataTransfer.types.includes('Files')) setDragOver(true);
            }}
            onDragLeave={() => setDragOver(false)}
            onDrop={(e) => {
                e.preventDefault();
                setDragOver(false);
                if (e.dataTransfer.types.includes('Files')) onUpload(e.dataTransfer.files);
            }}
        >
            <div className="flex items-center gap-3 flex-wrap mb-3">
                <h2 className="text-xl font-extrabold">我的文件</h2>
                {/* 存储策略切换:选择后浏览/上传都发生在对应存储 */}
                <div className="relative inline-flex items-center">
                    {inMount ? (
                        <Cloud className="absolute left-3 size-3.5 text-sky-600 dark:text-sky-400 pointer-events-none" />
                    ) : (
                        <HardDrive className="absolute left-3 size-3.5 text-ink-soft pointer-events-none" />
                    )}
                    <NativeSelect
                        className="h-8 text-xs pl-8 pr-2"
                        value={mountName}
                        onChange={(e) => {
                            const v = e.target.value;
                            if (v === '__manage__') navigate('/storage');
                            else navigate(v === '' ? '/files' : `/files/${v}`);
                        }}
                    >
                        <option value="">本机存储</option>
                        {policies.map((p) => (
                            <option key={p.id} value={`@${p.name}`}>
                                @{p.name}
                            </option>
                        ))}
                        <option value="__manage__">＋ 管理存储策略…</option>
                    </NativeSelect>
                </div>
                <div className="ml-auto flex gap-1.5 items-center">
                    <input
                        ref={fileInput}
                        type="file"
                        multiple
                        hidden
                        onChange={(e) => onUpload(e.target.files)}
                    />
                    {/* webkitdirectory:选整个文件夹,层级由 webkitRelativePath 保留 */}
                    <input
                        ref={dirInput}
                        type="file"
                        multiple
                        hidden
                        // @ts-expect-error 非标准属性,各浏览器都支持
                        webkitdirectory=""
                        directory=""
                        onChange={(e) => onUpload(e.target.files)}
                    />
                    <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                            <Button variant="primary" size="sm">
                                <Upload className="size-3.5" />
                                上传
                                <ChevronDown className="size-3.5 opacity-70" />
                            </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent>
                            <DropdownMenuItem onSelect={() => fileInput.current?.click()}>
                                上传文件
                            </DropdownMenuItem>
                            <DropdownMenuItem onSelect={() => dirInput.current?.click()}>
                                上传文件夹
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem onSelect={() => setMkdirOpen(true)}>
                                新建文件夹
                            </DropdownMenuItem>
                            <DropdownMenuItem onSelect={() => setNoteOpen(true)}>
                                新建笔记
                            </DropdownMenuItem>
                        </DropdownMenuContent>
                    </DropdownMenu>
                    <Button
                        size="icon-sm"
                        aria-label={view === 'list' ? '切换到缩略图' : '切换到列表'}
                        title={view === 'list' ? '切换到缩略图' : '切换到列表'}
                        onClick={() => switchView(view === 'list' ? 'grid' : 'list')}
                    >
                        {view === 'list' ? (
                            <LayoutGrid className="size-3.5" />
                        ) : (
                            <List className="size-3.5" />
                        )}
                    </Button>
                    <Button size="icon-sm" aria-label="刷新" title="刷新" onClick={load}>
                        <RefreshCw className="size-3.5" />
                    </Button>
                </div>
            </div>

            <Breadcrumb path={path} />

            {archiveTask && (
                <Card className="mb-3 py-3">
                    <div className="flex items-center gap-2 text-sm font-bold mb-1.5">
                        <span>
                            {archiveTask.kind === 'compress' ? '压缩中' : '解压中'}:
                            {archiveTask.kind === 'compress'
                                ? archiveTask.dest
                                : archiveTask.src}
                        </span>
                        {archiveTask.status === 'error' && (
                            <span className="text-danger font-normal text-xs">
                                {archiveTask.errorMsg}
                            </span>
                        )}
                    </div>
                    <Progress
                        percent={
                            archiveTask.status === 'done'
                                ? 100
                                : archiveTask.total > 0
                                  ? (archiveTask.done / archiveTask.total) * 100
                                  : 0
                        }
                    />
                </Card>
            )}

            <div className="flex gap-4 items-start">
                {/* 目录树 */}
                <FileTree
                    className="hidden lg:block w-52 shrink-0 max-h-[72vh] sticky top-20"
                    currentPath={path}
                    refreshKey={treeVersion}
                    icons={icons}
                    onNavigate={(p) => navigate(`/files/${p}`)}
                />

                <div className="flex-1 min-w-0">
                    <Card
                        className={cn(
                            'p-0 overflow-hidden',
                            dragOver && 'border-leaf border-dashed',
                        )}
                    >
                        {loading ? (
                            <div className="text-center text-ink-soft py-10 text-sm">
                                加载中…
                            </div>
                        ) : entries.length === 0 ? (
                            <div className="text-center text-ink-soft py-10 text-sm">
                                空文件夹 · 拖拽文件到此处上传
                            </div>
                        ) : view === 'list' ? (
                            <div>
                                {shown.map((e, i) => {
                                    const idx = pageStart + i;
                                    return (
                                    <ContextMenu key={e.name}>
                                        <ContextMenuTrigger asChild>
                                            <div
                                                draggable
                                                onDragStart={(ev) => onRowDragStart(ev, e)}
                                                onDragOver={(ev) => {
                                                    if (
                                                        e.dir &&
                                                        ev.dataTransfer.types.includes(
                                                            DND_TYPE,
                                                        )
                                                    ) {
                                                        ev.preventDefault();
                                                        ev.stopPropagation();
                                                        setDropTarget(e.name);
                                                    }
                                                }}
                                                onDragLeave={() =>
                                                    dropTarget === e.name &&
                                                    setDropTarget(null)
                                                }
                                                onDrop={(ev) => e.dir && onFolderDrop(ev, e)}
                                                className={cn(
                                                    'flex items-center gap-2.5 px-3 py-1.5 border-b border-line/50 last:border-b-0 hover:bg-paper-2/60 flex-wrap',
                                                    dropTarget === e.name &&
                                                        'bg-leaf-soft outline-2 outline-dashed outline-leaf -outline-offset-2',
                                                )}
                                            >
                                                <button
                                                    className="flex items-center gap-2 flex-1 min-w-0 basis-full sm:basis-auto font-bold text-left cursor-pointer truncate"
                                                    onClick={() => openEntry(e, idx)}
                                                    title={
                                                        e.dir
                                                            ? `${e.name}(可把文件拖进来)`
                                                            : e.name
                                                    }
                                                >
                                                    {entryIcon(e)}
                                                    <span className="truncate">{e.name}</span>
                                                </button>
                                                <span className="text-xs text-ink-soft w-20 text-right hidden sm:block shrink-0">
                                                    {e.dir ? '-' : formatBytes(e.size)}
                                                </span>
                                                <span className="text-xs text-ink-soft w-28 text-right hidden xl:block shrink-0">
                                                    {formatTime(e.mtime)}
                                                </span>
                                                {actions(e)}
                                            </div>
                                        </ContextMenuTrigger>
                                        <ContextMenuContent>
                                            {rowActions(e).map((a) => (
                                                <Fragment key={a.label}>
                                                    {a.sepBefore && <ContextMenuSeparator />}
                                                    <ContextMenuItem
                                                        danger={a.danger}
                                                        onSelect={a.run}
                                                    >
                                                        {a.label}
                                                    </ContextMenuItem>
                                                </Fragment>
                                            ))}
                                        </ContextMenuContent>
                                    </ContextMenu>
                                    );
                                })}
                            </div>
                        ) : (
                            <div className="grid grid-cols-2 sm:grid-cols-3 xl:grid-cols-4 gap-2.5 p-3">
                                {shown.map((e, i) => {
                                    const idx = pageStart + i;
                                    return (
                                    <ContextMenu key={e.name}>
                                        <ContextMenuTrigger asChild>
                                            <div
                                                draggable
                                                onDragStart={(ev) => onRowDragStart(ev, e)}
                                                onDragOver={(ev) => {
                                                    if (
                                                        e.dir &&
                                                        ev.dataTransfer.types.includes(
                                                            DND_TYPE,
                                                        )
                                                    ) {
                                                        ev.preventDefault();
                                                        ev.stopPropagation();
                                                        setDropTarget(e.name);
                                                    }
                                                }}
                                                onDragLeave={() =>
                                                    dropTarget === e.name &&
                                                    setDropTarget(null)
                                                }
                                                onDrop={(ev) => e.dir && onFolderDrop(ev, e)}
                                                className={cn(
                                                    'rounded-lg border border-line/70 overflow-hidden bg-paper flex flex-col',
                                                    dropTarget === e.name &&
                                                        'border-leaf bg-leaf-soft',
                                                )}
                                            >
                                                <button
                                                    className="cursor-pointer text-left"
                                                    onClick={() => openEntry(e, idx)}
                                                    title={e.name}
                                                >
                                                    <Thumb
                                                        path={join(e.name)}
                                                        name={e.name}
                                                        dir={e.dir}
                                                        dirIcon={icons[join(e.name)]}
                                                    />
                                                    <div className="px-2.5 pt-1.5 font-bold text-xs truncate">
                                                        {e.name}
                                                    </div>
                                                    <div className="px-2.5 pb-1 text-[11px] text-ink-soft">
                                                        {e.dir ? '文件夹' : formatBytes(e.size)}
                                                    </div>
                                                </button>
                                                <div className="px-1 pb-1.5 flex justify-center">
                                                    {actions(e)}
                                                </div>
                                            </div>
                                        </ContextMenuTrigger>
                                        <ContextMenuContent>
                                            {rowActions(e).map((a) => (
                                                <Fragment key={a.label}>
                                                    {a.sepBefore && <ContextMenuSeparator />}
                                                    <ContextMenuItem
                                                        danger={a.danger}
                                                        onSelect={a.run}
                                                    >
                                                        {a.label}
                                                    </ContextMenuItem>
                                                </Fragment>
                                            ))}
                                        </ContextMenuContent>
                                    </ContextMenu>
                                    );
                                })}
                            </div>
                        )}
                    </Card>

                    {/* 翻页:只在真的装不下一页时出现 */}
                    {entries.length > pageSize && (
                        <div className="flex items-center gap-2 mt-2.5 flex-wrap text-sm">
                            <span className="text-ink-soft text-xs">
                                第 {pageStart + 1}–{Math.min(pageStart + pageSize, entries.length)}{' '}
                                项,共 {entries.length} 项
                            </span>
                            <div className="ml-auto flex items-center gap-1.5">
                                <Button
                                    size="sm"
                                    disabled={curPage <= 1}
                                    onClick={() => setPage(curPage - 1)}
                                >
                                    上一页
                                </Button>
                                <span className="text-xs text-ink-soft tabular-nums px-1">
                                    {curPage} / {pageCount}
                                </span>
                                <Button
                                    size="sm"
                                    disabled={curPage >= pageCount}
                                    onClick={() => setPage(curPage + 1)}
                                >
                                    下一页
                                </Button>
                            </div>
                        </div>
                    )}
                    <p className="text-xs text-ink-soft mt-2">
                        拖拽文件行到文件夹(或左侧目录树同名文件夹)可移动;拖拽本地文件到列表上传;超过
                        64MB 自动分片上传
                    </p>
                </div>
            </div>

            {/* 新建文件夹 */}
            <Dialog open={mkdirOpen} onOpenChange={(o) => !o && setMkdirOpen(false)}>
                <DialogContent title="新建文件夹">
                    <Input
                        placeholder="文件夹名称"
                        value={mkdirName}
                        onChange={(e) => setMkdirName(e.target.value)}
                    />
                    <DialogFooter
                        onOk={() =>
                            mkdirName.trim() &&
                            run(
                                () => api.mkdir(join(mkdirName.trim())),
                                () => {
                                    setMkdirOpen(false);
                                    setMkdirName('');
                                },
                            )
                        }
                    />
                </DialogContent>
            </Dialog>

            {/* 新建笔记 */}
            <Dialog open={noteOpen} onOpenChange={(o) => !o && setNoteOpen(false)}>
                <DialogContent title="新建笔记">
                    <Input
                        placeholder="笔记名称(自动加 .md)"
                        value={noteName}
                        onChange={(e) => setNoteName(e.target.value)}
                        onKeyDown={(e) => e.key === 'Enter' && newNote()}
                    />
                    <DialogFooter onOk={newNote} okText="创建并编辑" />
                </DialogContent>
            </Dialog>

            {/* 重命名 */}
            <Dialog
                open={renameTarget !== null}
                onOpenChange={(o) => !o && setRenameTarget(null)}
            >
                <DialogContent title={`重命名「${renameTarget?.name ?? ''}」`}>
                    <Input
                        value={renameName}
                        onChange={(e) => setRenameName(e.target.value)}
                    />
                    <DialogFooter
                        onOk={() =>
                            renameTarget &&
                            run(
                                async () => {
                                    await api.rename(
                                        join(renameTarget.name),
                                        renameName.trim(),
                                    );
                                    if (renameTarget.dir) {
                                        await migrateIcon(
                                            join(renameTarget.name),
                                            join(renameName.trim()),
                                        );
                                    }
                                },
                                () => setRenameTarget(null),
                            )
                        }
                    />
                </DialogContent>
            </Dialog>

            {/* 移动:目录选择器(挂载内移动锁定在同一存储;本机隐藏挂载点) */}
            <FolderPicker
                open={moveTarget !== null}
                title={`移动「${moveTarget?.name ?? ''}」到…`}
                rootPath={mountName}
                hideMounts={!inMount}
                onClose={() => setMoveTarget(null)}
                onSelect={(dir) => moveTarget && doMoveTo(join(moveTarget.name), dir)}
            />

            {/* 文件夹图标 */}
            <Dialog open={iconTarget !== null} onOpenChange={(o) => !o && setIconTarget(null)}>
                <DialogContent title={`「${iconTarget?.name ?? ''}」的图标`}>
                    <div className="grid grid-cols-8 gap-1.5">
                        {FOLDER_EMOJIS.map((em) => (
                            <button
                                key={em}
                                className={cn(
                                    'text-xl rounded-xl py-1.5 cursor-pointer border transition-colors flex items-center justify-center',
                                    iconTarget &&
                                        (icons[join(iconTarget.name)] ?? '📁') === em
                                        ? 'border-leaf bg-leaf-soft'
                                        : 'border-transparent bg-paper-2 hover:border-line',
                                )}
                                title={em === '📁' ? '默认图标' : undefined}
                                onClick={() =>
                                    iconTarget &&
                                    run(
                                        async () => {
                                            await api.setIcon(
                                                join(iconTarget.name),
                                                em === '📁' ? '' : em,
                                            );
                                            loadIcons();
                                        },
                                        () => setIconTarget(null),
                                    )
                                }
                            >
                                {em === '📁' ? (
                                    <KindIcon kind="folder" className="size-6" />
                                ) : (
                                    em
                                )}
                            </button>
                        ))}
                    </div>
                    <p className="text-xs text-ink-soft mt-2">
                        第一个为默认图标
                    </p>
                </DialogContent>
            </Dialog>

            {/* 压缩 */}
            <Dialog
                open={compressTarget !== null}
                onOpenChange={(o) => !o && setCompressTarget(null)}
            >
                <DialogContent title={`压缩「${compressTarget?.name ?? ''}」`}>
                    <div className="flex flex-col gap-3">
                        <div>
                            <label className="block text-xs font-bold text-ink-soft mb-1">
                                格式
                            </label>
                            <NativeSelect
                                value={compressFormat}
                                onChange={(e) => {
                                    const f = e.target.value;
                                    setCompressFormat(f);
                                    // 扩展名跟着格式走,省得自己改
                                    const base = (compressTarget?.name ?? '').replace(
                                        /\.(zip|tar\.gz)$/i,
                                        '',
                                    );
                                    setCompressName(`${base}.${f}`);
                                }}
                            >
                                <option value="zip">zip(通用,Windows 双击可开)</option>
                                <option value="tar.gz">tar.gz(Linux 常用,压缩率更高)</option>
                            </NativeSelect>
                        </div>
                        <div>
                            <label className="block text-xs font-bold text-ink-soft mb-1">
                                压缩包名称
                            </label>
                            <Input
                                value={compressName}
                                onChange={(e) => setCompressName(e.target.value)}
                            />
                        </div>
                        <p className="text-xs text-ink-soft">
                            压缩包会放在当前目录。大文件夹需要一些时间,可以关掉这个窗口,
                            进度条在页面上方
                        </p>
                    </div>
                    <DialogFooter okText="开始压缩" onOk={doCompress} />
                </DialogContent>
            </Dialog>

            {/* 解压 */}
            <Dialog
                open={extractTarget !== null}
                onOpenChange={(o) => !o && setExtractTarget(null)}
            >
                <DialogContent title={`解压「${extractTarget?.name ?? ''}」`}>
                    <p className="text-sm">
                        解压到当前目录
                        {path === '' ? '(根目录)' : `「${path}」`}
                        。同名文件会被覆盖。
                    </p>
                    <DialogFooter okText="开始解压" onOk={doExtract} />
                </DialogContent>
            </Dialog>

            {/* 删除(本机进回收站;外部存储直接永久删除) */}
            <Dialog
                open={deleteTarget !== null}
                onOpenChange={(o) => !o && setDeleteTarget(null)}
            >
                <DialogContent title={inMount ? '永久删除' : '放进垃圾桶'}>
                    <p className="text-sm">
                        {inMount ? (
                            <>
                                外部存储不经回收站,「{deleteTarget?.name}」将
                                <b>直接从存储桶永久删除</b>,无法找回。确定吗?
                            </>
                        ) : (
                            <>
                                把「{deleteTarget?.name}」放进垃圾桶?30
                                天内可以在垃圾桶里找回, 30 天后自动清理。
                            </>
                        )}
                    </p>
                    <DialogFooter
                        okText={inMount ? '永久删除' : '放进垃圾桶'}
                        okDanger
                        onOk={() =>
                            deleteTarget &&
                            run(
                                () => api.remove([join(deleteTarget.name)]),
                                () => setDeleteTarget(null),
                            )
                        }
                    />
                </DialogContent>
            </Dialog>

            {/* 分享 */}
            <Dialog open={shareTarget !== null} onOpenChange={(o) => !o && closeShare()}>
                <DialogContent title={`分享「${shareTarget?.name ?? ''}」`}>
                    {shareResult ? (
                        <div>
                            <p className="text-sm mb-2">
                                {shareResult.type === 'direct'
                                    ? '直链已生成,带真实文件名后缀,可直接粘贴到播放器/下载工具'
                                    : '分享链接已生成'}
                            </p>
                            <code className="block bg-paper-2 rounded-xl px-3 py-2 text-xs break-all">
                                {shareLink(shareResult)}
                            </code>
                            {sharePass && shareResult.type === 'page' && (
                                <p className="text-sm mt-2">
                                    提取密码:
                                    <code className="bg-paper-2 rounded px-2">{sharePass}</code>
                                </p>
                            )}
                            <div className="flex gap-2 mt-4">
                                <Button
                                    variant="primary"
                                    onClick={() =>
                                        copyText(
                                            shareLink(shareResult) +
                                                (sharePass && shareResult.type === 'page'
                                                    ? ` 密码: ${sharePass}`
                                                    : ''),
                                        )
                                    }
                                >
                                    复制链接
                                </Button>
                                <Button onClick={closeShare}>完成</Button>
                            </div>
                        </div>
                    ) : (
                        <div className="flex flex-col gap-3">
                            <NativeSelect
                                value={shareType}
                                onChange={(e) => setShareType(e.target.value)}
                            >
                                <option value="page">分享页(可设密码,给人看)</option>
                                <option value="direct">直链(给播放器/下载工具用)</option>
                            </NativeSelect>
                            {shareType === 'page' && (
                                <Input
                                    placeholder="提取密码(留空则无需密码)"
                                    value={sharePass}
                                    onChange={(e) => setSharePass(e.target.value)}
                                />
                            )}
                            <NativeSelect
                                value={shareExpire}
                                onChange={(e) => setShareExpire(e.target.value)}
                            >
                                <option value="0">永久有效</option>
                                <option value="24">1 天</option>
                                <option value="168">7 天</option>
                                <option value="720">30 天</option>
                            </NativeSelect>
                            <p className="text-xs text-ink-soft">
                                生成后可在「分享管理」统一查看和删除
                            </p>
                            <DialogFooter onOk={doShare} okText="生成链接" />
                        </div>
                    )}
                </DialogContent>
            </Dialog>

            {previewIdx !== null && entries[previewIdx] && (
                <Preview
                    entries={entries}
                    index={previewIdx}
                    dirPath={path}
                    onNavigate={setPreviewIdx}
                    onClose={() => setPreviewIdx(null)}
                />
            )}
        </div>
    );
}
