import {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useMemo,
    useRef,
    useState,
} from 'react';
import type { ReactNode } from 'react';
import { toast } from 'sonner';
import { api } from '../api';

// 上传队列。放在路由之上,切页面不会打断正在传的文件。
//
// 暂停/继续直接复用服务端的断点续传:暂停只是停止发送分片,继续时
// 重新 init 会拿回已传分片列表,从断点接着传——不需要额外的协议。

// 不得小于 5MiB:传到外部存储时每块直接映射为一个 S3 Part,
// S3 规定除末片外每片至少 5MiB,调小会导致合并分片失败
export const CHUNK_SIZE = 8 * 1024 * 1024;
export const BIG_FILE = 64 * 1024 * 1024; // 超过这个大小才走分片

export type UploadStatus =
    | 'queued'
    | 'uploading'
    | 'paused'
    | 'done'
    | 'error'
    | 'canceled';

export interface UploadItem {
    id: string;
    name: string;
    /** 完整目标路径,含存储前缀 */
    path: string;
    /** 所属存储:'' 表示本机,否则是 @挂载名 */
    store: string;
    size: number;
    sent: number;
    status: UploadStatus;
    error?: string;
}

interface Ctl {
    paused: boolean;
    canceled: boolean;
}

interface UploadAPI {
    items: UploadItem[];
    /** 把文件加入队列。dir 是目标目录(含 @挂载前缀) */
    add: (files: File[], dir: string) => void;
    pause: (id: string) => void;
    resume: (id: string) => void;
    cancel: (id: string) => void;
    clearFinished: () => void;
    /** 有文件传完时自增,文件页据此刷新列表 */
    completedTick: number;
}

const UploadCtx = createContext<UploadAPI | null>(null);

export function useUploads(): UploadAPI {
    const ctx = useContext(UploadCtx);
    if (!ctx) throw new Error('useUploads 必须在 UploadProvider 内使用');
    return ctx;
}

/** 从目标路径里取出所属存储名(@挂载 或 '' 表示本机) */
function storeOf(p: string): string {
    if (!p.startsWith('@')) return '';
    return p.split('/')[0];
}

let seq = 0;
const nextId = () => `u${++seq}-${Date.now()}`;

export function UploadProvider({ children }: { children: ReactNode }) {
    const [items, setItems] = useState<UploadItem[]>([]);
    const [completedTick, setCompletedTick] = useState(0);

    // 控制标志放 ref:上传循环里读到的必须是最新值,不能被闭包冻住
    const ctls = useRef(new Map<string, Ctl>());
    const files = useRef(new Map<string, File>());
    // 目标路径在入队后不再变化,单独存一份,worker 不必回头查 state
    const targets = useRef(new Map<string, string>());
    const running = useRef(false);
    // 队列在 ref 里也存一份,worker 不依赖 state 的异步更新
    const queue = useRef<string[]>([]);

    const patch = useCallback((id: string, up: Partial<UploadItem>) => {
        setItems((list) => list.map((it) => (it.id === id ? { ...it, ...up } : it)));
    }, []);

    const add = useCallback((fs: File[], dir: string) => {
        const added: UploadItem[] = [];
        for (const f of fs) {
            const id = nextId();
            // 文件夹上传时保留相对层级(webkitRelativePath 形如 "相册/2026/a.jpg")
            const rel = (f as File & { webkitRelativePath?: string }).webkitRelativePath;
            const name = rel && rel !== '' ? rel : f.name;
            const path = dir === '' ? name : `${dir}/${name}`;
            files.current.set(id, f);
            targets.current.set(id, path);
            ctls.current.set(id, { paused: false, canceled: false });
            added.push({
                id,
                name,
                path,
                store: storeOf(path),
                size: f.size,
                sent: 0,
                status: 'queued',
            });
            queue.current.push(id);
        }
        if (added.length > 0) setItems((list) => [...list, ...added]);
    }, []);

    const pause = useCallback(
        (id: string) => {
            const c = ctls.current.get(id);
            if (c) c.paused = true;
            patch(id, { status: 'paused' });
        },
        [patch],
    );

    const resume = useCallback(
        (id: string) => {
            const c = ctls.current.get(id);
            if (c) c.paused = false;
            patch(id, { status: 'queued', error: undefined });
            if (!queue.current.includes(id)) queue.current.push(id);
        },
        [patch],
    );

    const cancel = useCallback(
        (id: string) => {
            const c = ctls.current.get(id);
            if (c) {
                c.canceled = true;
                c.paused = false;
            }
            queue.current = queue.current.filter((x) => x !== id);
            patch(id, { status: 'canceled' });
        },
        [patch],
    );

    const clearFinished = useCallback(() => {
        setItems((list) =>
            list.filter((it) => it.status === 'uploading' || it.status === 'queued' || it.status === 'paused'),
        );
    }, []);

    // ---- 上传 worker:一次只传一个文件,小内存 VPS 上更稳 ----
    useEffect(() => {
        if (running.current) return;

        const uploadOne = async (id: string): Promise<void> => {
            const file = files.current.get(id);
            const ctl = ctls.current.get(id);
            const target = targets.current.get(id);
            if (!file || !ctl || !target || ctl.canceled) return;

            patch(id, { status: 'uploading' });

            if (file.size <= BIG_FILE) {
                // 小文件:一次 multipart,拿不到细粒度进度
                const slash = target.lastIndexOf('/');
                const dir = slash < 0 ? '' : target.slice(0, slash);
                await api.upload(dir, [file]);
                patch(id, { status: 'done', sent: file.size });
                return;
            }

            const init = await api.uploadInit(target, file.size, file.lastModified, CHUNK_SIZE);
            const size = init.chunkSize > 0 ? init.chunkSize : CHUNK_SIZE;
            const chunks = Math.ceil(file.size / size) || 1;
            const done = new Set(init.uploaded ?? []);
            let sent = done.size * size;
            if (sent > file.size) sent = file.size;
            patch(id, { sent });

            for (let i = 0; i < chunks; i++) {
                if (ctl.canceled) return;
                if (ctl.paused) {
                    patch(id, { status: 'paused' });
                    return; // 继续时会重新 init,拿回已传分片
                }
                if (done.has(i)) continue;

                const blob = file.slice(i * size, Math.min(file.size, (i + 1) * size));
                for (let tries = 0; ; tries++) {
                    try {
                        await api.uploadChunk(init.id, i, blob);
                        break;
                    } catch (e) {
                        if (ctl.canceled || ctl.paused) return;
                        // 指数退避:断网几十秒也能自己缓过来
                        if (tries >= 4) throw e;
                        await new Promise((r) => setTimeout(r, 1000 * 2 ** tries));
                    }
                }
                sent = Math.min(file.size, sent + blob.size);
                patch(id, { sent });
            }
            if (ctl.canceled) return;
            await api.uploadComplete(init.id, target, chunks);
            patch(id, { status: 'done', sent: file.size });
        };

        const loop = async () => {
            running.current = true;
            for (;;) {
                const id = queue.current.shift();
                if (id === undefined) break;
                const ctl = ctls.current.get(id);
                if (!ctl || ctl.canceled || ctl.paused) continue;
                try {
                    await uploadOne(id);
                    const after = ctls.current.get(id);
                    if (after && !after.canceled && !after.paused) {
                        setCompletedTick((v) => v + 1);
                        files.current.delete(id);
                    }
                } catch (e) {
                    patch(id, {
                        status: 'error',
                        error: e instanceof Error ? e.message : '上传失败',
                    });
                    toast.error(`上传失败:${e instanceof Error ? e.message : ''}`);
                }
            }
            running.current = false;
        };

        // 队列里有活就开工;items 变化时重新检查
        if (queue.current.length > 0) void loop();
    }, [items, patch]);

    // 还有文件在传时,关页面给个提示
    useEffect(() => {
        const busy = items.some((i) => i.status === 'uploading' || i.status === 'queued');
        if (!busy) return;
        const onBeforeUnload = (e: BeforeUnloadEvent) => e.preventDefault();
        window.addEventListener('beforeunload', onBeforeUnload);
        return () => window.removeEventListener('beforeunload', onBeforeUnload);
    }, [items]);

    const value = useMemo(
        () => ({ items, add, pause, resume, cancel, clearFinished, completedTick }),
        [items, add, pause, resume, cancel, clearFinished, completedTick],
    );
    return <UploadCtx.Provider value={value}>{children}</UploadCtx.Provider>;
}
