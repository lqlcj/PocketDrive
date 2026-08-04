import { useSyncExternalStore } from 'react';

// 网盘内部的拖拽移动。
//
// HTML5 拖放在 dragover 阶段读不到 dataTransfer 里的内容(浏览器只在 drop
// 那一刻才把数据交出来),可「这个文件夹收不收」必须在鼠标悬停的当下就
// 回答——高亮、禁止光标、悬停展开都靠它。所以拖起来的那批路径同时存一份
// 在这个模块里,拖拽期间页面上任何组件都能同步读到。
//
// dataTransfer 里仍然照常写一份,拖到别的窗口/别的程序时不至于是空的。

export const DND_MIME = 'application/x-pd-path';

export interface DragPayload {
    /** 被拖动项的完整路径(含 @挂载前缀) */
    paths: string[];
    /** 所属存储:'' 本机,'@名字' 外部挂载 */
    store: string;
    /** 拖影上显示的名字(多选时是第一项) */
    label: string;
}

let payload: DragPayload | null = null;
const listeners = new Set<() => void>();

const subscribe = (fn: () => void) => {
    listeners.add(fn);
    return () => {
        listeners.delete(fn);
    };
};

export function startDrag(p: DragPayload) {
    payload = p;
    listeners.forEach((fn) => fn());
}

export function endDrag() {
    if (payload === null) return;
    payload = null;
    listeners.forEach((fn) => fn());
}

/** 订阅当前拖拽:开始/结束时重渲染,用来点亮能放的地方 */
export function useDragPayload(): DragPayload | null {
    return useSyncExternalStore(
        subscribe,
        () => payload,
        () => null,
    );
}

export function storeOf(p: string): string {
    return p.startsWith('@') ? p.split('/')[0]! : '';
}

export function parentOf(p: string): string {
    const i = p.lastIndexOf('/');
    return i < 0 ? '' : p.slice(0, i);
}

export function baseOf(p: string): string {
    const i = p.lastIndexOf('/');
    return i < 0 ? p : p.slice(i + 1);
}

/**
 * 这批东西能不能放进 dest。返回拒绝理由,null = 可以放。
 * 拒绝的目标不高亮、鼠标显示禁止圈,不用等到松手才知道白拖一趟。
 */
export function dropReject(dest: string, p: DragPayload | null): string | null {
    if (!p || p.paths.length === 0) return '没有正在拖动的内容';
    // 后端只支持同一存储内的 rename,跨存储得下载再上传
    if (storeOf(dest) !== p.store) return '不能跨存储拖动';
    for (const src of p.paths) {
        if (dest === src) return '不能放进自己';
        if (dest.startsWith(`${src}/`)) return '不能把文件夹放进它自己里面';
        if (parentOf(src) === dest) return '已经在这个文件夹里了';
    }
    return null;
}

export function canDrop(dest: string, p: DragPayload | null): boolean {
    return dropReject(dest, p) === null;
}

/** dataTransfer 里读回路径列表(drop 时用,兼容拖到别处的情况) */
export function readPaths(dt: DataTransfer): string[] {
    const raw = dt.getData(DND_MIME);
    if (!raw) return [];
    try {
        const v: unknown = JSON.parse(raw);
        return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [];
    } catch {
        return [raw]; // 旧格式:单条纯路径
    }
}

/**
 * 自定义拖影:默认拖影是整行的截图,拖着一大片半透明表格既看不清也挡视线。
 * 换成一枚跟手的小药丸,多选时右边挂个数字。
 */
export function setDragImage(dt: DataTransfer, label: string, count: number, icon: string) {
    const el = document.createElement('div');
    el.style.cssText = [
        'position:fixed',
        'top:-1000px',
        'left:-1000px',
        'display:flex',
        'align-items:center',
        'gap:8px',
        'padding:7px 14px',
        'border-radius:999px',
        'background:var(--leaf)',
        'color:#fff',
        'font-family:var(--font-sans, system-ui, sans-serif)',
        'font-size:13px',
        'font-weight:700',
        'line-height:1',
        'white-space:nowrap',
        'max-width:280px',
        'overflow:hidden',
        'box-shadow:0 8px 24px rgb(0 0 0 / 0.28)',
    ].join(';');

    const name = document.createElement('span');
    name.style.cssText = 'overflow:hidden;text-overflow:ellipsis;max-width:200px';
    name.textContent = `${icon} ${label}`;
    el.appendChild(name);

    if (count > 1) {
        const badge = document.createElement('span');
        badge.style.cssText =
            'background:rgb(255 255 255 / 0.28);border-radius:999px;padding:2px 8px;font-size:12px';
        badge.textContent = `+${count - 1}`;
        el.appendChild(badge);
    }

    document.body.appendChild(el);
    dt.setDragImage(el, 16, 16);
    // 浏览器已经把它画进拖影快照了,DOM 里可以立刻撤掉
    setTimeout(() => el.remove(), 0);
}
