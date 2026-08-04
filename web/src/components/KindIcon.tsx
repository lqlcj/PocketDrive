import type { ComponentType } from 'react';
import {
    Archive,
    File,
    FileCode2,
    FileSpreadsheet,
    FileText,
    Film,
    Folder,
    Image,
    Music,
    NotebookText,
    Presentation,
} from 'lucide-react';
import type { FileKind } from '../util';
import { cn } from '../lib/utils';

/** 文件类型 → lucide 图标 + 颜色(明暗两套) */
const MAP: Record<FileKind, { Icon: ComponentType<{ className?: string }>; cls: string }> = {
    folder: { Icon: Folder, cls: 'text-amber-600 dark:text-amber-400 fill-amber-600/15' },
    image: { Icon: Image, cls: 'text-sky-600 dark:text-sky-400' },
    video: { Icon: Film, cls: 'text-violet-600 dark:text-violet-400' },
    audio: { Icon: Music, cls: 'text-pink-600 dark:text-pink-400' },
    markdown: { Icon: NotebookText, cls: 'text-leaf-dark' },
    text: { Icon: FileCode2, cls: 'text-slate-500 dark:text-slate-400' },
    archive: { Icon: Archive, cls: 'text-orange-600 dark:text-orange-400' },
    doc: { Icon: FileText, cls: 'text-blue-600 dark:text-blue-400' },
    sheet: { Icon: FileSpreadsheet, cls: 'text-emerald-600 dark:text-emerald-400' },
    slide: { Icon: Presentation, cls: 'text-orange-600 dark:text-orange-400' },
    pdf: { Icon: FileText, cls: 'text-red-600 dark:text-red-400' },
    other: { Icon: File, cls: 'text-ink-soft' },
};

export default function KindIcon({
    kind,
    className,
}: {
    kind: FileKind;
    className?: string;
}) {
    const { Icon, cls } = MAP[kind] ?? MAP.other;
    return <Icon className={cn('size-4 shrink-0', cls, className)} />;
}

/** 文件/文件夹图标:文件夹可带用户自定义 emoji(设置过就显示 emoji) */
export function EntryIcon({
    kind,
    custom,
    className,
    emojiClassName,
}: {
    kind: FileKind;
    custom?: string;
    className?: string;
    emojiClassName?: string;
}) {
    if (kind === 'folder' && custom) {
        return <span className={cn('shrink-0 leading-none', emojiClassName)}>{custom}</span>;
    }
    return <KindIcon kind={kind} className={className} />;
}
