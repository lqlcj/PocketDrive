import { useState } from 'react';
import {
    Check,
    ChevronDown,
    ChevronUp,
    Cloud,
    HardDrive,
    Pause,
    Play,
    Upload,
    X,
} from 'lucide-react';
import { useUploads } from '../upload/store';
import type { UploadItem } from '../upload/store';
import { Button } from './ui/button';
import { formatBytes } from '../util';
import { cn } from '../lib/utils';

// 右下角的上传面板:文件在传时自动出现,可以收成一条。
// 每个文件单独暂停/继续/取消——暂停后重新开始会从断点续传。

function statusText(it: UploadItem): string {
    switch (it.status) {
        case 'queued':
            return '排队中';
        case 'paused':
            return '已暂停';
        case 'done':
            return '完成';
        case 'canceled':
            return '已取消';
        case 'error':
            return it.error || '失败';
        default:
            return `${formatBytes(it.sent)} / ${formatBytes(it.size)}`;
    }
}

export default function UploadPanel() {
    const { items, pause, resume, cancel, clearFinished } = useUploads();
    const [collapsed, setCollapsed] = useState(false);

    if (items.length === 0) return null;

    const active = items.filter((i) => i.status === 'uploading' || i.status === 'queued');
    const doneCount = items.filter((i) => i.status === 'done').length;
    const totalBytes = items.reduce((s, i) => s + i.size, 0);
    const sentBytes = items.reduce((s, i) => s + (i.status === 'done' ? i.size : i.sent), 0);
    const overall = totalBytes > 0 ? (sentBytes / totalBytes) * 100 : 0;

    return (
        <div className="fixed bottom-3 right-3 z-40 w-[min(92vw,22rem)]">
            <div className="bg-paper border border-line/70 rounded-[var(--radius-card)] shadow-lg overflow-hidden">
                {/* 标题条:点一下收起/展开 */}
                <button
                    className="w-full flex items-center gap-2 px-3 py-2 text-sm font-bold cursor-pointer hover:bg-paper-2/60 text-left"
                    onClick={() => setCollapsed((v) => !v)}
                >
                    <Upload className="size-4 shrink-0" />
                    <span className="flex-1 min-w-0 truncate">
                        {active.length > 0
                            ? `正在上传 ${active.length} 个文件`
                            : `已完成 ${doneCount} 个文件`}
                    </span>
                    <span className="text-xs text-ink-soft font-normal shrink-0">
                        {Math.round(overall)}%
                    </span>
                    {collapsed ? (
                        <ChevronUp className="size-4 shrink-0" />
                    ) : (
                        <ChevronDown className="size-4 shrink-0" />
                    )}
                </button>

                {/* 整体进度:收起时也留着,一眼能看到还剩多少 */}
                <div className="h-1 bg-paper-2">
                    <div
                        className="h-full bg-leaf transition-[width] duration-300"
                        style={{ width: `${overall}%` }}
                    />
                </div>

                {!collapsed && (
                    <>
                        <div className="max-h-[46vh] overflow-y-auto">
                            {items.map((it) => (
                                <div
                                    key={it.id}
                                    className="px-3 py-2 border-t border-line/50 flex items-center gap-2"
                                >
                                    <span className="shrink-0" title={it.store || '本机存储'}>
                                        {it.store ? (
                                            <Cloud className="size-3.5 text-sky-600 dark:text-sky-400" />
                                        ) : (
                                            <HardDrive className="size-3.5 text-ink-soft" />
                                        )}
                                    </span>
                                    <div className="flex-1 min-w-0">
                                        <div className="text-xs font-bold truncate" title={it.path}>
                                            {it.name}
                                        </div>
                                        <div
                                            className={cn(
                                                'text-[11px] truncate',
                                                it.status === 'error'
                                                    ? 'text-danger'
                                                    : 'text-ink-soft',
                                            )}
                                        >
                                            {statusText(it)}
                                        </div>
                                        {(it.status === 'uploading' || it.status === 'paused') && (
                                            <div className="h-0.5 bg-paper-2 rounded mt-1">
                                                <div
                                                    className="h-full bg-leaf rounded"
                                                    style={{
                                                        width: `${
                                                            it.size > 0
                                                                ? (it.sent / it.size) * 100
                                                                : 0
                                                        }%`,
                                                    }}
                                                />
                                            </div>
                                        )}
                                    </div>

                                    <div className="flex gap-0.5 shrink-0">
                                        {it.status === 'uploading' && (
                                            <Button
                                                variant="ghost"
                                                size="icon-sm"
                                                aria-label="暂停"
                                                title="暂停"
                                                onClick={() => pause(it.id)}
                                            >
                                                <Pause className="size-3.5" />
                                            </Button>
                                        )}
                                        {(it.status === 'paused' || it.status === 'error') && (
                                            <Button
                                                variant="ghost"
                                                size="icon-sm"
                                                aria-label="继续"
                                                title="继续(从断点续传)"
                                                onClick={() => resume(it.id)}
                                            >
                                                <Play className="size-3.5" />
                                            </Button>
                                        )}
                                        {it.status === 'done' ? (
                                            <span className="size-7 grid place-items-center text-leaf-dark">
                                                <Check className="size-3.5" />
                                            </span>
                                        ) : (
                                            it.status !== 'canceled' && (
                                                <Button
                                                    variant="ghost-danger"
                                                    size="icon-sm"
                                                    aria-label="取消"
                                                    title="取消"
                                                    onClick={() => cancel(it.id)}
                                                >
                                                    <X className="size-3.5" />
                                                </Button>
                                            )
                                        )}
                                    </div>
                                </div>
                            ))}
                        </div>

                        <div className="px-3 py-1.5 border-t border-line/50 flex justify-end">
                            <Button size="sm" variant="ghost" onClick={clearFinished}>
                                清空已完成
                            </Button>
                        </div>
                    </>
                )}
            </div>
        </div>
    );
}
