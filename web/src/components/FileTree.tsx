import { useCallback, useEffect, useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { api } from '../api';
import { cn } from '../lib/utils';

interface Props {
    currentPath: string;
    /** 变化时刷新已加载的节点(新建/删除/移动文件夹后) */
    refreshKey: number;
    icons: Record<string, string>;
    onNavigate: (path: string) => void;
    className?: string;
}

/** 目录树:根目录在最顶,懒加载展开,和右侧文件列表联动 */
export default function FileTree({
    currentPath,
    refreshKey,
    icons,
    onNavigate,
    className,
}: Props) {
    // path -> 子文件夹名列表(undefined = 未加载)
    const [children, setChildren] = useState<Record<string, string[]>>({});
    const [expanded, setExpanded] = useState<Set<string>>(new Set(['']));

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

    const renderNode = (p: string, name: string, depth: number) => {
        const isExpanded = expanded.has(p);
        const kids = children[p];
        const hasKids = kids === undefined || kids.length > 0;
        const active = currentPath === p;
        return (
            <div key={p || '__root'}>
                <div
                    className={cn(
                        'flex items-center gap-1 rounded-lg pr-2 text-sm cursor-pointer select-none',
                        active ? 'bg-leaf text-white font-bold' : 'hover:bg-paper-2',
                    )}
                    style={{ paddingLeft: depth * 14 + 4 }}
                >
                    <button
                        className={cn(
                            'size-5 flex items-center justify-center shrink-0 cursor-pointer',
                            active ? 'text-white/80' : 'text-ink-soft',
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
                        <span className="shrink-0">
                            {p === '' ? '🏝️' : (icons[p] ?? '📁')}
                        </span>
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
            className={cn(
                'bg-paper border-2 border-line rounded-[var(--radius-card)] p-2 overflow-auto',
                className,
            )}
        >
            {renderNode('', '根目录', 0)}
        </div>
    );
}
