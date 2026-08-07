import { useEffect, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { api, type TorrentFile } from '../api';
import { Button } from './ui/button';
import { Checkbox } from './ui/input';
import { formatBytes } from '../util';

export interface PendingSelect {
    gid: string;
    /** 磁力链:aria2 先取元数据,真实下载由 pause-metadata 保持暂停 */
    magnet: boolean;
}

/**
 * 「选完再下」的文件勾选弹框。
 *
 * 种子上传:addTorrent(paused=true) 后任务已挂起,这里拉一次文件清单即可。
 * 磁力链:aria2 正在获取元数据(只有几 KB),生成的真实任务会自动暂停；
 * 用户勾选后由父组件下发 select-file 并恢复。
 */
export default function DownloadFileSelect({
    item,
    busy,
    onConfirm,
    onCancel,
}: {
    item: PendingSelect;
    busy: boolean;
    onConfirm: (files: number[]) => void;
    onCancel: () => void;
}) {
    const [files, setFiles] = useState<TorrentFile[]>([]);
    const [name, setName] = useState('');
    const [loading, setLoading] = useState(true);
    const [failed, setFailed] = useState(false);
    const [checked, setChecked] = useState<Set<number>>(new Set());
    const [reload, setReload] = useState(0);
    // 磁力等元数据的秒数:超过时限还没拿到就提示,不无限转圈
    const [elapsed, setElapsed] = useState(0);
    const MAGNET_TIMEOUT = 120;

    useEffect(() => {
        setFiles([]);
        setName('');
        setLoading(true);
        setFailed(false);
        setChecked(new Set());
        setElapsed(0);
        let cancelled = false;
        let tries = 0;
        // 种子添加后立刻就有清单;磁力要等元数据,给个时限免得永远转圈
        const limit = item.magnet ? MAGNET_TIMEOUT : 8;
        const poll = async () => {
            if (cancelled) return;
            tries++;
            if (tries > limit) {
                setLoading(false);
                setFailed(true);
                return;
            }
            try {
                const r = await api.downloadFiles(item.gid);
                if (cancelled) return;
                const real = r.files.filter((f) => !f.path.includes('[METADATA]'));
                if (real.length === 0) {
                    setTimeout(poll, 1000);
                    return;
                }
                // pause-metadata 已保证真实任务暂停；再补一次 pause 作为兼容兜底。
                if (item.magnet) {
                    await api.pauseDownload(item.gid).catch(() => undefined);
                    if (cancelled) return;
                }
                setFiles(real);
                setName(r.name);
                setChecked(new Set(real.filter((f) => f.selected).map((f) => f.index)));
                setLoading(false);
            } catch {
                if (cancelled) return;
                // followedBy gid 迁移或 aria2 短暂重连时偶尔会有一次 RPC
                // 失败；在总时限内继续轮询，不让弹窗因为瞬时错误直接终止。
                setTimeout(poll, 1000);
            }
        };
        poll();
        return () => {
            cancelled = true;
        };
    }, [item.gid, item.magnet, reload]);

    // 磁力:每秒累计等待时长,loading 文案里显示
    useEffect(() => {
        if (!item.magnet || !loading) return;
        const id = setInterval(() => setElapsed((e) => e + 1), 1000);
        return () => clearInterval(id);
    }, [item.magnet, loading]);

    const allIndexes = files.map((f) => f.index);
    const allChecked = files.length > 0 && allIndexes.every((i) => checked.has(i));
    const selectedCount = checked.size;
    const selectedTotal = files.reduce(
        (sum, f) => (checked.has(f.index) ? sum + f.length : sum),
        0,
    );

    const toggleAll = () => setChecked(allChecked ? new Set() : new Set(allIndexes));
    const toggle = (i: number) =>
        setChecked((prev) => {
            const next = new Set(prev);
            if (next.has(i)) next.delete(i);
            else next.add(i);
            return next;
        });

    /** aria2 报的是绝对路径,去掉磁盘根和种子名那一段,只留种子里相对路径 */
    const relPath = (p: string, n: string): string => {
        if (n && p.includes(n)) {
            const r = p.slice(p.indexOf(n) + n.length).replace(/^[/\\]/, '');
            if (r) return r;
        }
        const parts = p.split(/[/\\]/).filter(Boolean);
        return parts[parts.length - 1] ?? p;
    };

    return (
        <div className="flex flex-col gap-3">
            {loading ? (
                <div className="flex flex-col items-center gap-2 py-8 text-sm text-ink-soft">
                    <RefreshCw className="size-5 animate-spin" />
                    <span>
                        {item.magnet
                            ? `正在获取种子信息…(磁力要先下载元数据,已等 ${elapsed} 秒)`
                            : '正在读取种子文件清单…'}
                    </span>
                    {item.magnet && elapsed >= 15 && (
                        <span className="text-xs max-w-md text-center leading-relaxed">
                            元数据由 aria2 通过 tracker + DHT 获取，通常几十秒内完成。
                            迟迟拿不到多半是磁力没人持有元数据、tracker 失效，
                            或服务器的 BT 网络受限。
                        </span>
                    )}
                </div>
            ) : failed ? (
                <div className="flex flex-col items-center gap-3 py-6 text-sm text-ink-soft">
                    <span>
                        {item.magnet
                            ? '超过 2 分钟还没拿到种子信息。磁力没人做种、tracker 失效,'
                              + '或 DHT 被防火墙挡了都可能这样。'
                            : '没能拿到种子文件列表(aria2 可能暂时不可达)'}
                    </span>
                    {item.magnet && (
                        <span className="text-xs max-w-md text-center leading-relaxed">
                            建议:到下载设置更新 Tracker 后移除并重新添加该磁力，
                            同时确认 6888(TCP+UDP) 已放行；仍失败时换用 .torrent 文件。
                        </span>
                    )}
                    <div className="flex gap-2">
                        <Button
                            size="sm"
                            disabled={busy}
                            onClick={() => setReload((v) => v + 1)}
                        >
                            重试
                        </Button>
                        <Button variant="ghost-danger" size="sm" disabled={busy} onClick={onCancel}>
                            移除任务
                        </Button>
                    </div>
                </div>
            ) : (
                <>
                    <div className="flex items-center gap-2 flex-wrap">
                        <span className="font-bold truncate flex-1 min-w-0" title={name}>
                            {name || '任务'}
                        </span>
                        <span className="text-xs text-ink-soft">
                            {files.length} 个文件,已选 {selectedCount} 个,共{' '}
                            {formatBytes(selectedTotal)}
                        </span>
                    </div>
                    <Checkbox
                        label={allChecked ? '全不选' : '全选'}
                        checked={allChecked}
                        onChange={toggleAll}
                    />
                    <div className="flex flex-col max-h-72 overflow-auto bg-paper-2 rounded-xl p-1.5 divide-y divide-line/60">
                        {files.map((f) => (
                            <label
                                key={f.index}
                                className="flex items-center gap-2.5 px-2 py-1.5 rounded-lg cursor-pointer hover:bg-paper text-sm min-w-0"
                            >
                                <input
                                    type="checkbox"
                                    className="size-4 accent-[var(--leaf)] shrink-0"
                                    checked={checked.has(f.index)}
                                    onChange={() => toggle(f.index)}
                                />
                                <span
                                    className="flex-1 min-w-0 truncate"
                                    title={relPath(f.path, name)}
                                >
                                    {relPath(f.path, name)}
                                </span>
                                <span className="text-xs text-ink-soft shrink-0">
                                    {formatBytes(f.length)}
                                </span>
                            </label>
                        ))}
                    </div>
                </>
            )}

            <div className="flex justify-end gap-2 mt-1">
                <Button variant="ghost" disabled={busy} onClick={onCancel}>
                    取消
                </Button>
                <Button
                    variant="primary"
                    disabled={busy || loading || failed || selectedCount === 0}
                    onClick={() => {
                        if (busy) return;
                        onConfirm(allIndexes.filter((i) => checked.has(i)));
                    }}
                >
                    {selectedCount === 0 && !loading && !failed
                        ? '请先勾选文件'
                        : `下载 ${loading || failed ? '' : `(${selectedCount}个)`}`}
                </Button>
            </div>
        </div>
    );
}
