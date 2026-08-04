import { useCallback, useEffect, useRef, useState } from 'react';
import { ChevronDown, ChevronRight, Cloud, Home } from 'lucide-react';
import { api } from '../api';
import { EntryIcon } from './KindIcon';
import { dropReject, readPaths, useDragPayload } from '../lib/dnd';
import { cn } from '../lib/utils';

interface Props {
    currentPath: string;
    /** 变化时刷新已加载的节点(新建/删除/移动文件夹后) */
    refreshKey: number;
    icons: Record<string, string>;
    onNavigate: (path: string) => void;
    /** 把右侧拖过来的文件放进某个目录 */
    onDropMove?: (paths: string[], dest: string) => void;
    className?: string;
}

/** 悬停多久自动展开折叠的节点——短了容易误展开,长了拖着手酸 */
const SPRING_MS = 600;

/** 目录树:根目录在最顶,懒加载展开,和右侧文件列表联动,可接收拖拽 */
export default function FileTree({
    currentPath,
    refreshKey,
    icons,
    onNavigate,
    onDropMove,
    className,
}: Props) {
    // path -> 子文件夹名列表(undefined = 未加载)
    const [children, setChildren] = useState<Record<string, string[]>>({});
    const [expanded, setExpanded] = useState<Set<string>>(new Set(['']));
    /** 鼠标正悬在哪个节点上(拖拽中) */
    const [dropPath, setDropPath] = useState<string | null>(null);
    /** 刚接住一批文件的节点,闪一下给个「收到了」的回应 */
    const [flash, setFlash] = useState<string | null>(null);

    const dragging = useDragPayload();
    const scroller = useRef<HTMLDivElement>(null);
    const springTimer = useRef(0);
    const springFor = useRef<string | null>(null);

    const load = useCallback((p: string) => {
        api.listFiles(p)
            .then((r) =>
                setChildren((prev) => ({
                    ...prev,
                    [p]: r.entries.filter((e) => e.dir).map((e) => e.name),
                })),
            )
            .catch(() => undefined);
    }, []);

    // 刷新:重载根 + 所有已展开的节点
    useEffect(() => {
        load('');
        expanded.forEach((p) => p !== '' && load(p));
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [refreshKey]);

    // 当前路径的祖先自动展开
    useEffect(() => {
        if (currentPath === '') return;
        const parts = currentPath.split('/');
        setExpanded((prev) => {
            const next = new Set(prev);
            let acc = '';
            next.add('');
            for (const seg of parts) {
                acc = acc === '' ? seg : `${acc}/${seg}`;
                next.add(acc);
            }
            return next;
        });
        let acc = '';
        for (const seg of ['', ...parts]) {
            acc = acc === '' ? seg : `${acc}/${seg}`;
            if (children[acc] === undefined) load(acc);
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [currentPath]);

    const toggle = (p: string) => {
        setExpanded((prev) => {
            const next = new Set(prev);
            if (next.has(p)) {
                next.delete(p);
            } else {
                next.add(p);
                if (children[p] === undefined) load(p);
            }
            return next;
        });
    };

    // ---- 拖拽 ----

    const cancelSpring = () => {
        window.clearTimeout(springTimer.current);
        springFor.current = null;
    };

    /** 拖着东西在折叠的文件夹上悬停一会儿,自动展开——一路拖到深层不用先手动展开 */
    const spring = (p: string) => {
        if (springFor.current === p) return;
        cancelSpring();
        springFor.current = p;
        if (expanded.has(p)) return;
        springTimer.current = window.setTimeout(() => {
            setExpanded((prev) => {
                if (prev.has(p)) return prev;
                const next = new Set(prev);
                next.add(p);
                return next;
            });
            if (children[p] === undefined) load(p);
        }, SPRING_MS);
    };

    // 一次拖拽结束(放下、按 Esc、拖出窗口)就把所有临时状态收干净
    useEffect(() => {
        if (dragging) return;
        setDropPath(null);
        cancelSpring();
    }, [dragging]);

    // 贴着树的上下边缘时持续滚动:目标在视野外也能拖到。
    // dragover 只在鼠标移动时触发,所以滚动交给 rAF,手停住也照滚。
    const scrollSpeed = useRef(0);
    useEffect(() => {
        if (!dragging) {
            scrollSpeed.current = 0;
            return;
        }
        let raf = 0;
        const tick = () => {
            const el = scroller.current;
            if (el && scrollSpeed.current !== 0) el.scrollTop += scrollSpeed.current;
            raf = requestAnimationFrame(tick);
        };
        raf = requestAnimationFrame(tick);
        return () => {
            cancelAnimationFrame(raf);
            scrollSpeed.current = 0;
        };
    }, [dragging]);

    const onPanelDragOver = (e: React.DragEvent) => {
        if (!dragging) return;
        const el = scroller.current;
        if (!el) return;
        const r = el.getBoundingClientRect();
        const edge = 44;
        const y = e.clientY;
        if (y < r.top + edge) scrollSpeed.current = -Math.ceil((r.top + edge - y) / 5);
        else if (y > r.bottom - edge) scrollSpeed.current = Math.ceil((y - (r.bottom - edge)) / 5);
        else scrollSpeed.current = 0;
    };

    const onNodeDrop = (e: React.DragEvent, p: string) => {
        e.preventDefault();
        e.stopPropagation();
        cancelSpring();
        setDropPath(null);
        const paths = readPaths(e.dataTransfer);
        if (paths.length === 0 || !onDropMove) return;
        setFlash(p);
        window.setTimeout(() => setFlash((cur) => (cur === p ? null : cur)), 700);
        onDropMove(paths, p);
    };

    const renderNode = (p: string, name: string, depth: number) => {
        const isExpanded = expanded.has(p);
        const kids = children[p];
        const hasKids = kids === undefined || kids.length > 0;
        const active = currentPath === p;
        const reject = dragging ? dropReject(p, dragging) : null;
        const droppable = dragging !== null && reject === null;
        const isTarget = droppable && dropPath === p;
        return (
            <div key={p || '__root'}>
                <div
                    className={cn(
                        'flex items-center gap-1 rounded-lg pr-2 text-sm cursor-pointer select-none transition-colors',
                        active ? 'bg-leaf text-white font-bold' : 'hover:bg-paper-2',
                        // 拖拽时把放不进去的目录压暗,能放的一眼看得出
                        dragging !== null && !droppable && 'opacity-35',
                        isTarget &&
                            'bg-leaf-soft text-ink outline-2 outline-dashed outline-leaf -outline-offset-2 font-bold',
                        flash === p && 'bg-leaf-soft',
                    )}
                    style={{ paddingLeft: depth * 14 + 4 }}
                    title={dragging && reject ? reject : undefined}
                    onDragOver={(e) => {
                        if (!dragging) return;
                        if (!droppable) {
                            e.dataTransfer.dropEffect = 'none';
                            return;
                        }
                        // 不 preventDefault 就不允许放置,鼠标会显示禁止圈
                        e.preventDefault();
                        e.stopPropagation();
                        e.dataTransfer.dropEffect = 'move';
                        if (dropPath !== p) setDropPath(p);
                        spring(p);
                    }}
                    onDragLeave={() => {
                        if (dropPath === p) setDropPath(null);
                        if (springFor.current === p) cancelSpring();
                    }}
                    onDrop={(e) => droppable && onNodeDrop(e, p)}
                >
                    <button
                        className={cn(
                            'size-5 flex items-center justify-center shrink-0 cursor-pointer',
                            active && !isTarget ? 'text-white/80' : 'text-ink-soft',
                            !hasKids && 'invisible',
                        )}
                        aria-label={isExpanded ? '收起' : '展开'}
                        onClick={(e) => {
                            e.stopPropagation();
                            toggle(p);
                        }}
                    >
                        {isExpanded ? (
                            <ChevronDown className="size-3.5" />
                        ) : (
                            <ChevronRight className="size-3.5" />
                        )}
                    </button>
                    <button
                        className="flex items-center gap-1.5 flex-1 min-w-0 py-1.5 text-left cursor-pointer"
                        onClick={() => onNavigate(p)}
                        title={name}
                    >
                        {p === '' ? (
                            <Home
                                className={cn(
                                    'size-4 shrink-0',
                                    active && !isTarget ? 'text-white' : 'text-leaf-dark',
                                )}
                            />
                        ) : name.startsWith('@') ? (
                            <Cloud
                                className={cn(
                                    'size-4 shrink-0',
                                    active && !isTarget
                                        ? 'text-white'
                                        : 'text-sky-600 dark:text-sky-400',
                                )}
                            />
                        ) : (
                            <EntryIcon
                                kind="folder"
                                custom={icons[p]}
                                className={cn(
                                    'size-4',
                                    active && !isTarget && 'text-white fill-white/20',
                                )}
                            />
                        )}
                        <span className="truncate">{name}</span>
                    </button>
                </div>
                {isExpanded &&
                    (kids ?? []).map((child) =>
                        renderNode(p === '' ? child : `${p}/${child}`, child, depth + 1),
                    )}
            </div>
        );
    };

    return (
        <div
            ref={scroller}
            onDragOver={onPanelDragOver}
            className={cn(
                'bg-paper border border-line/70 rounded-[var(--radius-card)] shadow-[var(--shadow-card)] p-2 overflow-auto transition-shadow',
                // 拖起来的时候整块板子亮一下:提醒左边这棵树也能接
                dragging !== null && 'ring-2 ring-leaf/50 border-leaf/50',
                className,
            )}
        >
            {renderNode('', '根目录', 0)}
        </div>
    );
}
