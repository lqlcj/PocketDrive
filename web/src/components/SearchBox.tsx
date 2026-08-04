import { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Search } from 'lucide-react';
import { api } from '../api';
import type { IndexItem } from '../api';
import KindIcon from './KindIcon';
import { fileKind, formatBytes } from '../util';

/** 顶栏全局搜索:输入防抖搜文件名,点击结果跳到所在目录 */
export default function SearchBox() {
    const navigate = useNavigate();
    const [q, setQ] = useState('');
    const [results, setResults] = useState<IndexItem[]>([]);
    const [open, setOpen] = useState(false);
    const boxRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        if (!q.trim()) {
            setResults([]);
            setOpen(false);
            return;
        }
        const t = setTimeout(() => {
            api.search(q)
                .then((r) => {
                    setResults(r.results);
                    setOpen(true);
                })
                .catch(() => undefined);
        }, 250);
        return () => clearTimeout(t);
    }, [q]);

    useEffect(() => {
        const onDown = (e: MouseEvent) => {
            if (boxRef.current && !boxRef.current.contains(e.target as Node)) {
                setOpen(false);
            }
        };
        document.addEventListener('mousedown', onDown);
        return () => document.removeEventListener('mousedown', onDown);
    }, []);

    const go = (it: IndexItem) => {
        setOpen(false);
        setQ('');
        if (it.dir) {
            navigate(`/files/${it.path}`);
        } else {
            const parent = it.path.includes('/')
                ? it.path.slice(0, it.path.lastIndexOf('/'))
                : '';
            navigate(`/files/${parent}`);
        }
    };

    return (
        <div ref={boxRef} className="relative w-full max-w-xs">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-ink-soft pointer-events-none" />
            <input
                value={q}
                onChange={(e) => setQ(e.target.value)}
                onFocus={() => results.length > 0 && setOpen(true)}
                onKeyDown={(e) => e.key === 'Escape' && setOpen(false)}
                placeholder="搜索全盘文件…"
                className="h-9 w-full rounded-full border border-line bg-paper pl-9 pr-3 text-sm outline-none transition-[border-color,box-shadow] focus:border-leaf focus:ring-2 focus:ring-leaf/25 placeholder:text-ink-soft/70"
            />
            {open && (
                <div className="absolute top-11 left-0 right-0 z-40 bg-paper border border-line/70 rounded-2xl shadow-xl overflow-hidden max-h-80 overflow-y-auto">
                    {results.length === 0 ? (
                        <div className="p-4 text-center text-sm text-ink-soft">
                            没有找到「{q}」
                        </div>
                    ) : (
                        results.map((it) => (
                            <button
                                key={it.path}
                                onClick={() => go(it)}
                                className="flex items-center gap-2 w-full px-3 py-2 text-left hover:bg-paper-2 cursor-pointer"
                            >
                                <KindIcon kind={fileKind(it.name, it.dir)} />
                                <span className="flex-1 min-w-0">
                                    <span className="block text-sm font-bold truncate">
                                        {it.name}
                                    </span>
                                    <span className="block text-xs text-ink-soft truncate">
                                        {it.path}
                                    </span>
                                </span>
                                {!it.dir && (
                                    <span className="text-xs text-ink-soft">
                                        {formatBytes(it.size)}
                                    </span>
                                )}
                            </button>
                        ))
                    )}
                </div>
            )}
        </div>
    );
}
