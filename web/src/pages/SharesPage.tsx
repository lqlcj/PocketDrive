import { useCallback, useEffect, useState } from 'react';
import { FileText, Lock } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { Share } from '../api';
import { Card } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Dialog, DialogContent, DialogFooter } from '../components/ui/dialog';
import { Input, NativeSelect, Textarea } from '../components/ui/input';
import { Badge } from '../components/ui/progress';
import { copyText, formatTime, shareLink } from '../util';

export default function SharesPage() {
    const [shares, setShares] = useState<Share[]>([]);
    const [loading, setLoading] = useState(true);
    const [textOpen, setTextOpen] = useState(false);
    const [textContent, setTextContent] = useState('');
    const [textPassword, setTextPassword] = useState('');
    const [textExpire, setTextExpire] = useState('0');
    const [creatingText, setCreatingText] = useState(false);
    const [createdText, setCreatedText] = useState<Share | null>(null);

    const load = useCallback(() => {
        api.shares()
            .then((r) => setShares(r.shares))
            .catch(() => undefined)
            .finally(() => setLoading(false));
    }, []);

    useEffect(load, [load]);

    const copy = async (value: string, success = '已复制链接') => {
        // navigator.clipboard 在 http:// 下不存在,copyText 里有兜底。
        if (await copyText(value)) toast.success(success);
        else toast.warning('复制失败,请手动选中复制');
    };

    const copyLink = (share: Share) => copy(shareLink(share));

    const delShare = async (share: Share) => {
        try {
            await api.deleteShare(share.id);
            toast.success('已删除分享');
            load();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '删除失败');
        }
    };

    const closeTextDialog = () => {
        setTextOpen(false);
        setTextContent('');
        setTextPassword('');
        setTextExpire('0');
        setCreatedText(null);
        setCreatingText(false);
    };

    const createText = async () => {
        if (!textContent.trim()) {
            toast.warning('请先写入要分享的文本');
            return;
        }
        setCreatingText(true);
        try {
            const result = await api.createTextShare(
                textContent,
                textPassword,
                Number(textExpire),
            );
            setCreatedText(result.share);
            load();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '创建文本分享失败');
        } finally {
            setCreatingText(false);
        }
    };

    return (
        <div>
            <div className="flex items-center justify-between gap-3 mb-4">
                <h2 className="text-xl font-extrabold">分享管理</h2>
                <Button variant="primary" onClick={() => setTextOpen(true)}>
                    <FileText className="size-4" /> 分享文本
                </Button>
            </div>

            {loading ? (
                <Card className="text-center text-ink-soft py-10 text-sm">加载中…</Card>
            ) : shares.length === 0 ? (
                <Card className="text-center text-ink-soft py-10 text-sm">
                    还没有分享。可以在这里分享一段文本，或在「我的文件」里分享文件。
                </Card>
            ) : (
                <Card className="p-0 overflow-hidden">
                    {shares.map((share) => {
                        const isText = share.type === 'text';
                        return (
                            <div
                                key={share.id}
                                className="flex items-center gap-2.5 px-4 py-2.5 border-b border-line/50 last:border-b-0 flex-wrap"
                            >
                                <Badge
                                    tone={
                                        share.type === 'direct'
                                            ? 'blue'
                                            : isText
                                              ? 'orange'
                                              : 'green'
                                    }
                                >
                                    {share.type === 'direct'
                                        ? '直链'
                                        : isText
                                          ? '文本'
                                          : '分享页'}
                                </Badge>
                                <div className="flex-1 min-w-0">
                                    <div className="font-bold text-sm truncate">
                                        {isText ? share.summary || '文本分享' : share.path}
                                    </div>
                                    <div className="text-xs text-ink-soft truncate">
                                        {shareLink(share)}
                                    </div>
                                    <div className="text-xs text-ink-soft flex items-center gap-1 flex-wrap">
                                        {share.hasPassword ? (
                                            <span className="inline-flex items-center gap-0.5">
                                                <Lock className="size-3" /> 有密码
                                            </span>
                                        ) : (
                                            '公开'
                                        )}{' '}
                                        ·{' '}
                                        {share.expiresAt
                                            ? `${formatTime(share.expiresAt)} 过期`
                                            : '永久'}{' '}
                                        · {formatTime(share.createdAt)} 创建
                                    </div>
                                </div>
                                <Button size="sm" onClick={() => copyLink(share)}>
                                    复制
                                </Button>
                                <Button
                                    variant="ghost-danger"
                                    size="sm"
                                    onClick={() => delShare(share)}
                                >
                                    删除
                                </Button>
                            </div>
                        );
                    })}
                </Card>
            )}

            <Dialog open={textOpen} onOpenChange={(open) => !open && closeTextDialog()}>
                <DialogContent title="分享文本">
                    {createdText ? (
                        <div>
                            <p className="text-sm mb-2">
                                文本分享页已生成，打开后可直接阅读和复制。
                            </p>
                            <code className="block bg-paper-2 rounded-xl px-3 py-2 text-xs break-all">
                                {shareLink(createdText)}
                            </code>
                            {textPassword && (
                                <p className="text-sm mt-2">
                                    提取密码：
                                    <code className="bg-paper-2 rounded px-2">
                                        {textPassword}
                                    </code>
                                </p>
                            )}
                            <div className="flex gap-2 mt-4">
                                <Button
                                    variant="primary"
                                    onClick={() =>
                                        copy(
                                            shareLink(createdText) +
                                                (textPassword
                                                    ? ` 密码: ${textPassword}`
                                                    : ''),
                                            '分享信息已复制',
                                        )
                                    }
                                >
                                    复制分享信息
                                </Button>
                                <Button onClick={closeTextDialog}>完成</Button>
                            </div>
                        </div>
                    ) : (
                        <div className="flex flex-col gap-3">
                            <div>
                                <Textarea
                                    autoFocus
                                    className="h-44"
                                    maxLength={20000}
                                    placeholder="粘贴或写入要分享的文字、地址、说明等…"
                                    value={textContent}
                                    onChange={(event) => setTextContent(event.target.value)}
                                />
                                <div className="mt-1 text-right text-xs text-ink-soft tabular-nums">
                                    {textContent.length} / 20000
                                </div>
                            </div>
                            <Input
                                type="password"
                                placeholder="提取密码（留空则无需密码）"
                                value={textPassword}
                                onChange={(event) => setTextPassword(event.target.value)}
                            />
                            <NativeSelect
                                className="w-full"
                                value={textExpire}
                                onChange={(event) => setTextExpire(event.target.value)}
                            >
                                <option value="0">永久有效</option>
                                <option value="24">1 天</option>
                                <option value="168">7 天</option>
                                <option value="720">30 天</option>
                            </NativeSelect>
                            <p className="text-xs text-ink-soft">
                                正文保存在分享记录中，不会在网盘里额外创建文件。
                            </p>
                            <DialogFooter
                                onOk={createText}
                                okText="生成分享页"
                                okLoading={creatingText}
                            />
                        </div>
                    )}
                </DialogContent>
            </Dialog>
        </div>
    );
}
