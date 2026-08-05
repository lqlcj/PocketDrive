import { Fragment, useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import {
    Check,
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
import { extractable, fileKind, formatBytes, formatTime, shareLink, copyText } from '../util';
import { fetchList, invalidateList, peekList, prefetchList } from '../lib/listcache';
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
import {
    DND_MIME,
    baseOf,
    canDrop,
    dropReject,
    endDrag,
    parentOf,
    readPaths,
    setDragImage,
    startDrag,
    storeOf,
    useDragPayload,
} from '../lib/dnd';
import { cn } from '../lib/utils';

type ViewMode = 'list' | 'grid';

/** 行操作项:同时喂给「⋯」下拉菜单和右键菜单 */
type RowAction = {
    label: string;
    run: () => void;
    danger?: boolean;
    sepBefore?: boolean;
};

const DND_TYPE = DND_MIME;

// 每页条数按视图分开:列表一行一个,50 行一屏滚两下就到底;
// 缩略图是 2-4 列的网格,24 个正好铺满 6 行且不留半行空白
const PAGE_LIST = 50;
const PAGE_GRID = 24;

const FOLDER_EMOJIS = [
    '📁', '🎵', '🎬', '🖼️', '📔', '💼', '🎮', '📚',
    '🍱', '🧸', '🌟', '💾', '🔧', '🏠', '❤️', '🎁',
    '✈️', '📷', '🎨', '🏫', '👶', '🐾', '🔒', '🗃️',
];

/** 面包屑:每一段都能接住拖过来的文件,往上层挪不用先切目录 */
function Breadcrumb({
    path,
    onDropMove,
}: {
    path: string;
    onDropMove: (paths: string[], dest: string) => void;
}) {
    const parts = path === '' ? [] : path.split('/');
    const dragging = useDragPayload();
    const [over, setOver] = useState<string | null>(null);
    // 第一段是 @挂载名时,整条路径都属于那个外部存储——用不同颜色标出来,
    // 免得在两个存储之间来回切时看错地方
    const inMount = parts.length > 0 && parts[0].startsWith('@');

    const dropProps = (dest: string) => {
        const droppable = dragging !== null && canDrop(dest, dragging);
        return {
            active: droppable && over === dest,
            handlers: {
                onDragOver: (e: React.DragEvent) => {
                    if (!dragging) return;
                    if (!droppable) {
                        e.dataTransfer.dropEffect = 'none';
                        return;
                    }
                    e.preventDefault();
                    e.stopPropagation();
                    e.dataTransfer.dropEffect = 'move';
                    setOver(dest);
                },
                onDragLeave: () => setOver((cur) => (cur === dest ? null : cur)),
                onDrop: (e: React.DragEvent) => {
                    e.preventDefault();
                    e.stopPropagation();
                    setOver(null);
                    if (!droppable) return;
                    const paths = readPaths(e.dataTransfer);
                    if (paths.length > 0) onDropMove(paths, dest);
                },
            },
        };
    };

    const root = dropProps('');
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
                    'font-bold inline-flex items-center gap-1 align-middle rounded px-1 -mx-1',
                    inMount ? 'text-sky-700 dark:text-sky-300' : 'text-leaf-dark',
                    root.active && 'bg-leaf-soft outline-2 outline-dashed outline-leaf',
                )}
                to="/files"
                // 面包屑是落点不是拖手,别让浏览器把它当链接拖起来
                draggable={false}
                onMouseEnter={() => prefetchList('')}
                {...root.handlers}
            >
                <Home className="size-3.5" /> 根目录
            </Link>
            {parts.map((p, i) => {
                const acc = parts.slice(0, i + 1).join('/');
                const d = dropProps(acc);
                return (
                    <span key={acc}>
                        {' / '}
                        <Link
                            className={cn(
                                'font-bold rounded px-1 -mx-1',
                                inMount ? 'text-sky-700 dark:text-sky-300' : 'text-leaf-dark',
                                d.active && 'bg-leaf-soft outline-2 outline-dashed outline-leaf',
                            )}
                            to={`/files/${acc}`}
                            draggable={false}
                            onMouseEnter={() => prefetchList(acc)}
                            {...d.handlers}
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
    // 触屏没有 hover,复选框不能藏在图标底下,得单独占一格常驻显示
    const [coarse] = useState(
        () => typeof window !== 'undefined' && window.matchMedia('(hover: none)').matches,
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
    // 移动/删除/压缩都可能针对一批选中的项,统一存一组名字(空数组 = 对话框关着)
    const [moveNames, setMoveNames] = useState<string[]>([]);
    const [deleteNames, setDeleteNames] = useState<string[]>([]);
    const [iconTarget, setIconTarget] = useState<FileEntry | null>(null);
    const [previewIdx, setPreviewIdx] = useState<number | null>(null);

    // 多选:存当前目录下的名字。换目录、刷新列表都会清空
    const [selected, setSelected] = useState<Set<string>>(new Set());
    // Shift 连选的锚点(上一次单独点中的行号)
    const anchor = useRef<number | null>(null);
    // 正在移动的项:先淡出,等接口回来再整体刷新,不会突然消失一格
    const [moving, setMoving] = useState<Set<string>>(new Set());
    const dragging = useDragPayload();

    const [shareTarget, setShareTarget] = useState<FileEntry | null>(null);
    const [sharePass, setSharePass] = useState('');
    const [shareType, setShareType] = useState('page');
    const [shareExpire, setShareExpire] = useState('0');
    const [shareResult, setShareResult] = useState<Share | null>(null);

    const [dragOver, setDragOver] = useState(false);
    const [dropTarget, setDropTarget] = useState<string | null>(null);

    // 压缩/解压:异步任务,起完轮询进度
    const [compressNames, setCompressNames] = useState<string[]>([]);
    const [compressName, setCompressName] = useState('');
    const [compressFormat, setCompressFormat] = useState('zip');
    const [extractTarget, setExtractTarget] = useState<FileEntry | null>(null);
    const [archiveTask, setArchiveTask] = useState<ArchiveTask | null>(null);

    const loadIcons = useCallback(() => {
        api.icons()
            .then((r) => setIcons(r.icons))
            .catch(() => undefined);
    }, []);

    // 进目录:有缓存就先把旧列表画上去(不转圈),同时后台刷新。
    // 大部分情况下第二次进同一个目录是瞬间的。
    const load = useCallback(
        (force = false) => {
            const cached = peekList(path);
            if (cached) {
                setEntries(cached);
                setLoading(false);
            } else {
                setLoading(true);
            }
            fetchList(path, force)
                .then((entries) => {
                    setEntries(entries);
                    // 列表变了(移走、删掉、传完),把已经不在的项从选择里剔除
                    const names = new Set(entries.map((e) => e.name));
                    setSelected((prev) => {
                        const next = new Set([...prev].filter((n) => names.has(n)));
                        return next.size === prev.size ? prev : next;
                    });
                })
                .catch((e) => {
                    toast.error(e instanceof Error ? e.message : '加载失败');
                    setEntries([]);
                })
                .finally(() => {
                    setLoading(false);
                    setMoving((prev) => (prev.size === 0 ? prev : new Set()));
                });
        },
        [path],
    );

    /**
     * 写操作之后的刷新:缓存全清 + 强制重拉 + 让左边目录树也跟着更新。
     * 目录树刷新只挂在这里——以前 load() 里也带着,于是每点一次目录,
     * 树就把根和所有展开的节点重新请求一遍,进三层目录要等 5 个往返。
     */
    const refresh = useCallback(() => {
        invalidateList();
        load(true);
        setTreeVersion((v) => v + 1);
    }, [load]);

    useEffect(() => load(), [load]);
    useEffect(loadIcons, [loadIcons]);
    useEffect(() => {
        api.storages()
            .then((r) => setPolicies(r.policies))
            .catch(() => undefined);
    }, []);

    // 有文件传完就刷新列表(上传面板在别的页面也可能传完)
    useEffect(() => {
        if (completedTick > 0) refresh();
    }, [completedTick, refresh]);

    // 换目录、换视图都回到第一页,否则会停在一个空页上
    useEffect(() => setPage(1), [path, view]);

    // 等超过 180ms 才承认「在加载」——快的时候不闪字,慢的时候有反馈
    const [slow, setSlow] = useState(false);
    useEffect(() => {
        if (!loading) {
            setSlow(false);
            return;
        }
        const t = window.setTimeout(() => setSlow(true), 180);
        return () => window.clearTimeout(t);
    }, [loading]);

    // 换目录后原来的选择没有意义了
    useEffect(() => {
        setSelected(new Set());
        anchor.current = null;
    }, [path]);

    // Esc 取消选择,Ctrl/⌘+A 全选(在输入框里打字时不抢)
    useEffect(() => {
        const onKey = (ev: KeyboardEvent) => {
            const t = ev.target as HTMLElement | null;
            if (
                t &&
                (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)
            ) {
                return;
            }
            if (ev.key === 'Escape') {
                setSelected((prev) => (prev.size === 0 ? prev : new Set()));
            } else if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === 'a') {
                ev.preventDefault();
                setSelected(new Set(entries.map((e) => e.name)));
            }
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [entries]);

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
                        refresh();
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
        [refresh],
    );

    const doCompress = async () => {
        if (compressNames.length === 0) return;
        const name = compressName.trim();
        if (!name) {
            toast.warning('请填写压缩包名称');
            return;
        }
        try {
            const r = await api.compress(
                compressNames.map((n) => join(n)),
                join(name),
                compressFormat,
            );
            setCompressNames([]);
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
            refresh();
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

    // ---- 移动:拖拽和「移动到…」走的是同一条路 ----

    const destLabel = (dest: string) => (dest === '' ? '根目录' : baseOf(dest));

    /** 撤销一次移动:照原路把每一项送回去 */
    const undoMove = async (moved: string[], dest: string) => {
        let ok = 0;
        for (const src of moved) {
            const now = dest === '' ? baseOf(src) : `${dest}/${baseOf(src)}`;
            try {
                await api.move(now, parentOf(src));
                await migrateIcon(now, src);
                ok++;
            } catch {
                // 那边已经被改过名或再次移动,放不回去就算了
            }
        }
        refresh();
        if (ok > 0) toast.success(`已放回 ${ok} 项`);
        else toast.error('放不回去了,东西可能已经被改名或再次移动');
    };

    /**
     * 把一批路径移进 dest。后端一次只收一个,这里挨个发。
     * 目标里已有同名的先挑出来跳过,不然会撞上一个看不懂的 rename 报错;
     * 移完只弹一条 toast,带「撤销」——拖歪了一下就能放回去。
     */
    const moveMany = async (paths: string[], dest: string) => {
        const list = paths.filter(
            (p) => p !== '' && p !== dest && parentOf(p) !== dest && !dest.startsWith(`${p}/`),
        );
        if (list.length === 0) return;
        if (list.some((p) => storeOf(p) !== storeOf(dest))) {
            toast.warning('暂不支持跨存储移动,请下载后再上传到目标存储');
            return;
        }

        let taken = new Set<string>();
        try {
            const r = await api.listFiles(dest);
            taken = new Set(r.entries.map((e) => e.name));
        } catch {
            // 目标目录读不到就直接发请求,让后端去判
        }
        const dup = list.filter((p) => taken.has(baseOf(p)));
        const todo = list.filter((p) => !taken.has(baseOf(p)));
        if (todo.length === 0) {
            toast.warning(
                `「${destLabel(dest)}」里已经有同名的${dup.length > 1 ? `${dup.length} 项` : `「${baseOf(dup[0])}」`},没有移动`,
            );
            return;
        }

        // 先让这几行淡下去,等接口回来再整体刷新
        setMoving(new Set(todo.map(baseOf)));
        const done: string[] = [];
        let firstErr = '';
        for (const src of todo) {
            try {
                await api.move(src, dest);
                await migrateIcon(src, dest === '' ? baseOf(src) : `${dest}/${baseOf(src)}`);
                done.push(src);
            } catch (e) {
                if (!firstErr) firstErr = e instanceof Error ? e.message : '移动失败';
            }
        }
        refresh();
        setSelected(new Set());

        if (done.length > 0) {
            const what =
                done.length === 1 ? `「${baseOf(done[0])}」` : `${done.length} 项`;
            toast.success(`已把 ${what} 移到「${destLabel(dest)}」`, {
                duration: 7000,
                action: { label: '撤销', onClick: () => void undoMove(done, dest) },
            });
        }
        if (dup.length > 0) toast.warning(`${dup.length} 项因目标已有同名而跳过`);
        if (firstErr) toast.error(`${todo.length - done.length} 项没能移动:${firstErr}`);
    };

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

    // 走 util 里的 copyText:http:// 下 navigator.clipboard 是 undefined,
    // 那边有 execCommand 兜底(这里以前直接用 clipboard,所以在 VPS 上
    // 一按就是「复制失败」)
    const copy = async (text: string) => {
        if (await copyText(text)) toast.success('已复制到剪贴板');
        else toast.warning('复制失败,请手动选中链接复制');
    };

    const openEntry = (e: FileEntry, idx: number) => {
        if (e.dir) navigate(`/files/${join(e.name)}`);
        else setPreviewIdx(idx);
    };

    /**
     * 鼠标扫过文件夹就把它的内容先拉回来。从悬停到点下去通常有几百毫秒,
     * 够一个往返了——真点进去时列表往往已经在缓存里,不再转圈。
     */
    const hoverFolder = (e: FileEntry) => {
        if (e.dir) prefetchList(join(e.name));
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

    // ---- 多选 ----

    // 保持列表里的先后顺序,批量操作时提示语读起来才对得上
    const selectedNames = entries.filter((e) => selected.has(e.name)).map((e) => e.name);

    const toggleSelect = (name: string, idx: number) => {
        anchor.current = idx;
        setSelected((prev) => {
            const next = new Set(prev);
            if (next.has(name)) next.delete(name);
            else next.add(name);
            return next;
        });
    };

    /** 点名字:普通=打开,Ctrl/⌘ 点=加选一项,Shift 点=连选一段 */
    const onEntryClick = (ev: React.MouseEvent, e: FileEntry, idx: number) => {
        if (ev.shiftKey && anchor.current !== null) {
            ev.preventDefault();
            const from = Math.min(anchor.current, idx);
            const to = Math.max(anchor.current, idx);
            setSelected(new Set(entries.slice(from, to + 1).map((x) => x.name)));
            return;
        }
        if (ev.ctrlKey || ev.metaKey) {
            ev.preventDefault();
            toggleSelect(e.name, idx);
            return;
        }
        openEntry(e, idx);
    };

    // ---- 拖拽移动 ----

    /**
     * 一次拖走哪些:拖的是已选中的项就整批走,否则只走手底下这一个,
     * 并把选择切成它——松手后高亮的正好是刚拖走的东西(和资源管理器一致)。
     */
    const onRowDragStart = (ev: React.DragEvent, entry: FileEntry) => {
        const batch =
            selected.has(entry.name) && selected.size > 1
                ? selectedNames.filter((n) => !(path === '' && n.startsWith('@')))
                : [entry.name];
        if (batch.length === 0) return;
        const paths = batch.map((n) => join(n));
        ev.dataTransfer.setData(DND_TYPE, JSON.stringify(paths));
        ev.dataTransfer.effectAllowed = 'move';
        startDrag({ paths, store: mountName, label: batch[0] });
        setDragImage(
            ev.dataTransfer,
            batch[0],
            batch.length,
            batch.length > 1 ? '🗂️' : entry.dir ? '📁' : '📄',
        );
        if (!selected.has(entry.name)) setSelected(new Set([entry.name]));
    };

    const onRowDragEnd = () => {
        endDrag();
        setDropTarget(null);
    };

    const onFolderDrop = (ev: React.DragEvent, folder: FileEntry) => {
        ev.preventDefault();
        ev.stopPropagation();
        setDropTarget(null);
        setDragOver(false);
        const paths = readPaths(ev.dataTransfer);
        if (paths.length > 0) void moveMany(paths, join(folder.name));
    };

    /** 文件夹作为落点的那套事件 + 状态,列表和缩略图共用 */
    const folderDrop = (entry: FileEntry) => {
        const droppable =
            entry.dir && dragging !== null && canDrop(join(entry.name), dragging);
        const rejectWhy =
            entry.dir && dragging !== null ? dropReject(join(entry.name), dragging) : null;
        return {
            droppable,
            active: droppable && dropTarget === entry.name,
            rejectWhy,
            handlers: {
                onDragOver: (ev: React.DragEvent) => {
                    if (!dragging) return;
                    if (!droppable) {
                        // 不 preventDefault:鼠标直接变成禁止圈,不用等松手才知道
                        ev.dataTransfer.dropEffect = 'none';
                        return;
                    }
                    ev.preventDefault();
                    ev.stopPropagation();
                    ev.dataTransfer.dropEffect = 'move';
                    if (dropTarget !== entry.name) setDropTarget(entry.name);
                },
                onDragLeave: () =>
                    setDropTarget((cur) => (cur === entry.name ? null : cur)),
                onDrop: (ev: React.DragEvent) => droppable && onFolderDrop(ev, entry),
            },
        };
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

    const selectBox = (e: FileEntry, idx: number, on: boolean) => (
        <span
            role="checkbox"
            aria-checked={on}
            aria-label={`选择 ${e.name}`}
            title="选择(也可以按住 Ctrl/⌘ 点名字)"
            onClick={(ev) => {
                ev.stopPropagation();
                toggleSelect(e.name, idx);
            }}
            className={cn(
                'size-[18px] rounded-[6px] border flex items-center justify-center cursor-pointer shrink-0',
                on
                    ? 'bg-leaf border-leaf text-white'
                    : 'bg-paper/90 border-line hover:border-leaf',
            )}
        >
            {on && <Check className="size-3 stroke-[3.5]" />}
        </span>
    );

    /**
     * 列表行最左边那一格:桌面上复选框藏在图标底下,鼠标扫过或已选中才浮出来,
     * 平时一行还是干干净净的图标;触屏没有 hover,就让它单独占一格常驻。
     */
    const iconCell = (e: FileEntry, idx: number) => {
        const on = selected.has(e.name);
        if (coarse) {
            return (
                <>
                    {selectBox(e, idx, on)}
                    {entryIcon(e)}
                </>
            );
        }
        return (
            <span className="relative size-[18px] shrink-0">
                <span
                    className={cn(
                        'absolute inset-0 flex items-center justify-center transition-opacity',
                        on ? 'opacity-0' : 'group-hover/row:opacity-0',
                    )}
                >
                    {entryIcon(e)}
                </span>
                <span
                    className={cn(
                        'absolute inset-0 flex items-center justify-center transition-opacity',
                        on ? 'opacity-100' : 'opacity-0 group-hover/row:opacity-100',
                    )}
                >
                    {selectBox(e, idx, on)}
                </span>
            </span>
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
        // 右键点在选中的一批上:菜单整批生效,不用为了删 10 个点 10 次
        if (selected.size > 1 && selected.has(e.name)) {
            const n = selected.size;
            return [
                { label: `移动这 ${n} 项到…`, run: () => setMoveNames(selectedNames) },
                {
                    label: `压缩这 ${n} 项`,
                    run: () => {
                        setCompressNames(selectedNames);
                        setCompressName(`${path === '' ? '打包' : baseOf(path)}.zip`);
                        setCompressFormat('zip');
                    },
                },
                {
                    label: `删除这 ${n} 项`,
                    run: () => setDeleteNames(selectedNames),
                    danger: true,
                    sepBefore: true,
                },
            ];
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
        list.push({ label: '移动到…', run: () => setMoveNames([e.name]) });
        list.push({
            label: '压缩为…',
            run: () => {
                setCompressNames([e.name]);
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
            run: () => setDeleteNames([e.name]),
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
                    // 否则从这里起手拖的是链接本身,不是这一行文件
                    draggable={false}
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
                // 只接从桌面拖进来的文件;网盘内部的拖拽落在空白处不该显示「可以放」
                if (!e.dataTransfer.types.includes('Files')) return;
                e.preventDefault();
                setDragOver(true);
            }}
            onDragLeave={() => setDragOver(false)}
            onDrop={(e) => {
                if (!e.dataTransfer.types.includes('Files')) return;
                e.preventDefault();
                setDragOver(false);
                onUpload(e.dataTransfer.files);
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
                    <Button size="icon-sm" aria-label="刷新" title="刷新" onClick={refresh}>
                        <RefreshCw className="size-3.5" />
                    </Button>
                </div>
            </div>

            <Breadcrumb path={path} onDropMove={(paths, dest) => void moveMany(paths, dest)} />

            {/* 选中一批之后浮出来的操作条 */}
            {selected.size > 0 && (
                <Card className="mb-3 py-2 px-3 flex items-center gap-2 flex-wrap sticky top-16 z-20 border-leaf/60 shadow-md">
                    <span className="text-sm font-bold">
                        已选 {selected.size} 项
                        <span className="font-normal text-ink-soft text-xs ml-2 hidden sm:inline">
                            可整批拖到左边目录树
                        </span>
                    </span>
                    <div className="ml-auto flex gap-1.5 flex-wrap">
                        <Button size="sm" onClick={() => setMoveNames(selectedNames)}>
                            移动到…
                        </Button>
                        <Button
                            size="sm"
                            onClick={() => {
                                setCompressNames(selectedNames);
                                setCompressName(
                                    `${path === '' ? '打包' : baseOf(path)}.zip`,
                                );
                                setCompressFormat('zip');
                            }}
                        >
                            压缩
                        </Button>
                        <Button
                            size="sm"
                            variant="danger"
                            onClick={() => setDeleteNames(selectedNames)}
                        >
                            删除
                        </Button>
                        <Button size="sm" variant="ghost" onClick={() => setSelected(new Set())}>
                            取消
                        </Button>
                    </div>
                </Card>
            )}

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
                    onDropMove={(paths, dest) => void moveMany(paths, dest)}
                />

                <div className="flex-1 min-w-0">
                    <Card
                        className={cn(
                            'p-0 overflow-hidden',
                            dragOver && 'border-leaf border-dashed',
                        )}
                    >
                        {loading && entries.length === 0 ? (
                            <div className="text-center text-ink-soft py-10 text-sm">
                                {/* 没到 180ms 就先空着:够快的时候不该闪一下「加载中」*/}
                                {slow ? '加载中…' : ' '}
                            </div>
                        ) : entries.length === 0 ? (
                            <div className="text-center text-ink-soft py-10 text-sm">
                                空文件夹 · 拖拽文件到此处上传
                            </div>
                        ) : view === 'list' ? (
                            <div>
                                {shown.map((e, i) => {
                                    const idx = pageStart + i;
                                    const on = selected.has(e.name);
                                    const drop = folderDrop(e);
                                    return (
                                    <ContextMenu key={e.name}>
                                        <ContextMenuTrigger asChild>
                                            <div
                                                draggable={!isMountRoot(e)}
                                                onDragStart={(ev) => onRowDragStart(ev, e)}
                                                onDragEnd={onRowDragEnd}
                                                {...drop.handlers}
                                                title={drop.rejectWhy ?? undefined}
                                                className={cn(
                                                    'group/row flex items-center gap-2.5 px-3 py-1.5 border-b border-line/50 last:border-b-0 hover:bg-paper-2/60 flex-wrap select-none transition-opacity',
                                                    on && 'bg-leaf-soft/60',
                                                    drop.active &&
                                                        'bg-leaf-soft outline-2 outline-dashed outline-leaf -outline-offset-2',
                                                    // 拖着东西路过放不进去的文件夹时压暗一点
                                                    dragging !== null &&
                                                        e.dir &&
                                                        !drop.droppable &&
                                                        'opacity-45',
                                                    moving.has(e.name) &&
                                                        'opacity-40 pointer-events-none',
                                                )}
                                            >
                                                {iconCell(e, idx)}
                                                <button
                                                    className="flex items-center gap-2 flex-1 min-w-0 basis-full sm:basis-auto font-bold text-left cursor-pointer truncate"
                                                    onClick={(ev) => onEntryClick(ev, e, idx)}
                                                    onMouseEnter={() => hoverFolder(e)}
                                                    title={
                                                        e.dir
                                                            ? `${e.name}(可把文件拖进来)`
                                                            : e.name
                                                    }
                                                >
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
                                    const on = selected.has(e.name);
                                    const drop = folderDrop(e);
                                    return (
                                    <ContextMenu key={e.name}>
                                        <ContextMenuTrigger asChild>
                                            <div
                                                draggable={!isMountRoot(e)}
                                                onDragStart={(ev) => onRowDragStart(ev, e)}
                                                onDragEnd={onRowDragEnd}
                                                {...drop.handlers}
                                                title={drop.rejectWhy ?? undefined}
                                                className={cn(
                                                    'group/card relative rounded-lg border border-line/70 overflow-hidden bg-paper flex flex-col select-none transition-opacity',
                                                    on && 'border-leaf ring-2 ring-leaf/40',
                                                    drop.active && 'border-leaf bg-leaf-soft',
                                                    dragging !== null &&
                                                        e.dir &&
                                                        !drop.droppable &&
                                                        'opacity-45',
                                                    moving.has(e.name) &&
                                                        'opacity-40 pointer-events-none',
                                                )}
                                            >
                                                <span
                                                    className={cn(
                                                        'absolute left-1.5 top-1.5 z-10 transition-opacity',
                                                        on || coarse
                                                            ? 'opacity-100'
                                                            : 'opacity-0 group-hover/card:opacity-100',
                                                    )}
                                                >
                                                    {selectBox(e, idx, on)}
                                                </span>
                                                <button
                                                    className="cursor-pointer text-left"
                                                    onClick={(ev) => onEntryClick(ev, e, idx)}
                                                    onMouseEnter={() => hoverFolder(e)}
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
                        把文件拖到左侧目录树或列表里的文件夹上就能移动,拖着悬停一会儿会自动展开子目录;
                        Ctrl/⌘ 点或 Shift 点名字可以多选,选中后整批一起拖。
                        从桌面拖文件进来是上传,超过 64MB 自动分片
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
                open={moveNames.length > 0}
                title={
                    moveNames.length === 1
                        ? `移动「${moveNames[0]}」到…`
                        : `移动 ${moveNames.length} 项到…`
                }
                rootPath={mountName}
                hideMounts={!inMount}
                onClose={() => setMoveNames([])}
                onSelect={(dir) =>
                    void moveMany(
                        moveNames.map((n) => join(n)),
                        dir,
                    )
                }
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
                open={compressNames.length > 0}
                onOpenChange={(o) => !o && setCompressNames([])}
            >
                <DialogContent
                    title={
                        compressNames.length === 1
                            ? `压缩「${compressNames[0]}」`
                            : `压缩选中的 ${compressNames.length} 项`
                    }
                >
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
                                    const base = compressName.replace(
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
                open={deleteNames.length > 0}
                onOpenChange={(o) => !o && setDeleteNames([])}
            >
                <DialogContent title={inMount ? '永久删除' : '放进垃圾桶'}>
                    <p className="text-sm">
                        {inMount ? (
                            <>
                                外部存储不经回收站,
                                {deleteNames.length === 1
                                    ? `「${deleteNames[0]}」`
                                    : `选中的 ${deleteNames.length} 项`}
                                将<b>直接从存储桶永久删除</b>,无法找回。确定吗?
                            </>
                        ) : (
                            <>
                                把
                                {deleteNames.length === 1
                                    ? `「${deleteNames[0]}」`
                                    : `选中的 ${deleteNames.length} 项`}
                                放进垃圾桶?30 天内可以在垃圾桶里找回, 30 天后自动清理。
                            </>
                        )}
                    </p>
                    <DialogFooter
                        okText={inMount ? '永久删除' : '放进垃圾桶'}
                        okDanger
                        onOk={() =>
                            deleteNames.length > 0 &&
                            run(
                                () => api.remove(deleteNames.map((n) => join(n))),
                                () => {
                                    setDeleteNames([]);
                                    setSelected(new Set());
                                },
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
                                        copy(
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
