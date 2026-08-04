import { useCallback, useEffect, useState } from 'react';
import { Link2, Lock } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { Share } from '../api';
import { Card } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Badge } from '../components/ui/progress';
import { formatTime } from '../util';

export default function SharesPage() {
    const [shares, setShares] = useState<Share[]>([]);
    const [loading, setLoading] = useState(true);

    const load = useCallback(() => {
        api.shares()
            .then((r) => setShares(r.shares))
            .catch(() => undefined)
            .finally(() => setLoading(false));
    }, []);

    useEffect(load, [load]);

    const shareLink = (s: Share) =>
        `${window.location.origin}${s.type === 'direct' ? '/d/' : '/s/'}${s.token}`;

    const copyLink = async (s: Share) => {
        try {
            await navigator.clipboard.writeText(shareLink(s));
            toast.success('已复制链接');
        } catch {
            toast.warning('复制失败');
        }
    };

    const delShare = async (s: Share) => {
        try {
            await api.deleteShare(s.id);
            toast.success('已删除分享');
            load();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '删除失败');
        }
    };

    return (
        <div>
            <h2 className="text-xl font-extrabold mb-4 flex items-center gap-2">
                <Link2 className="size-5 text-leaf-dark" /> 分享管理
            </h2>
            {loading ? (
                <Card className="text-center text-ink-soft py-10 text-sm">加载中…</Card>
            ) : shares.length === 0 ? (
                <Card className="text-center text-ink-soft py-10 text-sm">
                    还没有分享。在「我的文件」里点某个文件的「分享」即可创建分享页或直链。
                </Card>
            ) : (
                <Card className="p-0 overflow-hidden">
                    {shares.map((s) => (
                        <div
                            key={s.id}
                            className="flex items-center gap-2.5 px-4 py-2.5 border-b border-line/50 last:border-b-0 flex-wrap"
                        >
                            <Badge tone={s.type === 'direct' ? 'blue' : 'green'}>
                                {s.type === 'direct' ? '直链' : '分享页'}
                            </Badge>
                            <div className="flex-1 min-w-0">
                                <div className="font-bold text-sm truncate">{s.path}</div>
                                <div className="text-xs text-ink-soft truncate">
                                    {shareLink(s)}
                                </div>
                                <div className="text-xs text-ink-soft flex items-center gap-1 flex-wrap">
                                    {s.hasPassword ? (
                                        <span className="inline-flex items-center gap-0.5">
                                            <Lock className="size-3" /> 有密码
                                        </span>
                                    ) : (
                                        '公开'
                                    )}{' '}
                                    · {s.expiresAt ? `${formatTime(s.expiresAt)} 过期` : '永久'} ·{' '}
                                    {formatTime(s.createdAt)} 创建
                                </div>
                            </div>
                            <Button size="sm" onClick={() => copyLink(s)}>
                                复制
                            </Button>
                            <Button variant="ghost-danger" size="sm" onClick={() => delShare(s)}>
                                删除
                            </Button>
                        </div>
                    ))}
                </Card>
            )}
        </div>
    );
}
