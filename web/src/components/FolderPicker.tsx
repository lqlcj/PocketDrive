import { useCallback, useEffect, useState } from 'react';
import { toast } from 'sonner';
import { api } from '../api';
import { Dialog, DialogContent, DialogFooter } from './ui/dialog';

interface Props {
    open: boolean;
    title?: string;
    /** 初始目录(相对根) */
    initial?: string;
    onClose: () => void;
    onSelect: (dir: string) => void;
}

/** 浏览网盘现有文件夹并选择一个;默认根目录 */
export default function FolderPicker({
    open,
    title = '选择文件夹',
    initial = '',
    onClose,
    onSelect,
}: Props) {
    const [path, setPath] = useState(initial);
    const [dirs, setDirs] = useState<string[]>([]);
    const [loading, setLoading] = useState(false);

    const load = useCallback((p: string) => {
        setLoading(true);
        api.listFiles(p)
            .then((r) => {
                setPath(p);
                setDirs(r.entries.filter((e) => e.dir).map((e) => e.name));
            })
            .catch((e) => toast.error(e instanceof Error ? e.message : '加载失败'))
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => {
        if (open) load(initial);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [open]);

    const parts = path === '' ? [] : path.split('/');

    return (
        <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
            <DialogContent title={title}>
                <div className="text-sm text-ink-soft break-all mb-2">
                    <button
                        className="text-leaf-dark font-bold cursor-pointer"
                        onClick={() => load('')}
                    >
                        🏝️ 根目录
                    </button>
                    {parts.map((seg, i) => {
                        const p = parts.slice(0, i + 1).join('/');
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
                <div className="border-2 border-dashed border-line rounded-2xl p-1 max-h-64 overflow-auto flex flex-col">
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
                                className="text-left px-3 py-2 rounded-xl hover:bg-paper-2 text-sm cursor-pointer"
                                onClick={() => load(path === '' ? d : `${path}/${d}`)}
                            >
                                📁 {d}
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
