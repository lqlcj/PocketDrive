import { useCallback, useEffect, useState } from 'react';
import { toast } from 'sonner';
import { api } from '../api';
import type { TrashItem } from '../api';
import { Card } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Dialog, DialogContent, DialogFooter } from '../components/ui/dialog';
import { formatBytes, formatTime } from '../util';

export default function Trash() {
    const [items, setItems] = useState<TrashItem[]>([]);
    const [loading, setLoading] = useState(true);
    const [emptyOpen, setEmptyOpen] = useState(false);
    const [permTarget, setPermTarget] = useState<TrashItem | null>(null);

    const load = useCallback(() => {
        api.trash()
            .then((r) => setItems(r.items))
            .catch(() => undefined)
            .finally(() => setLoading(false));
    }, []);

    useEffect(load, [load]);

    const run = async (fn: () => Promise<unknown>, msg?: string) => {
        try {
            await fn();
            if (msg) toast.success(msg);
            load();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '操作失败');
        }
    };

    const daysLeft = (deletedAt: string) => {
        const passed = (Date.now() - new Date(deletedAt).getTime()) / 86400000;
        return Math.max(0, Math.ceil(30 - passed));
    };

    return (
        <div>
            <div className="flex items-center gap-3 mb-4">
                <h2 className="text-xl font-extrabold">🗑️ 垃圾桶</h2>
                <span className="text-sm text-ink-soft">30 天后自动清理</span>
                {items.length > 0 && (
                    <Button
                        variant="danger"
                        size="sm"
                        className="ml-auto"
                        onClick={() => setEmptyOpen(true)}
                    >
                        清空垃圾桶
                    </Button>
                )}
            </div>

            {loading ? (
                <Card className="text-center text-ink-soft py-10 text-sm">加载中…</Card>
            ) : items.length === 0 ? (
                <Card className="text-center text-ink-soft py-10 text-sm">
                    垃圾桶干干净净 ✨
                </Card>
            ) : (
                <Card className="p-0 overflow-hidden">
                    {items.map((it) => (
                        <div
                            key={it.id}
                            className="flex items-center gap-3 px-4 py-2.5 border-b border-dashed border-line last:border-b-0 flex-wrap"
                        >
                            <span className="text-lg">{it.dir ? '📁' : '📄'}</span>
                            <span className="flex-1 min-w-0">
                                <span className="block font-bold text-sm truncate">
                                    {it.name}
                                </span>
                                <span className="block text-xs text-ink-soft truncate">
                                    原位置:{it.origPath} · {formatTime(it.deletedAt)} 删除 ·{' '}
                                    {daysLeft(it.deletedAt)} 天后清理
                                </span>
                            </span>
                            {!it.dir && (
                                <span className="text-xs text-ink-soft shrink-0">
                                    {formatBytes(it.size)}
                                </span>
                            )}
                            <div className="flex gap-1 shrink-0">
                                <Button
                                    size="sm"
                                    onClick={() =>
                                        run(() => api.restoreTrash(it.id), '已还原到原位置')
                                    }
                                >
                                    还原
                                </Button>
                                <Button
                                    variant="ghost-danger"
                                    size="sm"
                                    onClick={() => setPermTarget(it)}
                                >
                                    永久删除
                                </Button>
                            </div>
                        </div>
                    ))}
                </Card>
            )}

            <Dialog open={permTarget !== null} onOpenChange={(o) => !o && setPermTarget(null)}>
                <DialogContent title="永久删除">
                    <p className="text-sm">
                        永久删除「{permTarget?.name}」?这一步无法恢复。
                    </p>
                    <DialogFooter
                        okText="永久删除"
                        okDanger
                        onOk={() =>
                            permTarget &&
                            run(() => api.deleteTrash(permTarget.id)).then(() =>
                                setPermTarget(null),
                            )
                        }
                    />
                </DialogContent>
            </Dialog>

            <Dialog open={emptyOpen} onOpenChange={(o) => !o && setEmptyOpen(false)}>
                <DialogContent title="清空垃圾桶">
                    <p className="text-sm">
                        清空垃圾桶会永久删除全部 {items.length} 项内容,无法恢复。确定吗?
                    </p>
                    <DialogFooter
                        okText="全部永久删除"
                        okDanger
                        onOk={() =>
                            run(() => api.emptyTrash(), '垃圾桶已清空').then(() =>
                                setEmptyOpen(false),
                            )
                        }
                    />
                </DialogContent>
            </Dialog>
        </div>
    );
}
