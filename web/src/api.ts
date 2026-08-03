// 后端 API 客户端:cookie 认证,401 时广播事件让 App 回到登录页
export class ApiError extends Error {
    status: number;
    constructor(status: number, message: string) {
        super(message);
        this.status = status;
    }
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
    const resp = await fetch(path, init);
    if (resp.status === 401) {
        window.dispatchEvent(new Event('pocketdrive:unauth'));
        throw new ApiError(401, '未登录');
    }
    const ct = resp.headers.get('Content-Type') ?? '';
    const isJSON = ct.includes('application/json');
    const body = isJSON ? await resp.json() : await resp.text();
    if (!resp.ok) {
        const msg = isJSON && body.error ? body.error : `请求失败 (${resp.status})`;
        throw new ApiError(resp.status, msg);
    }
    return body as T;
}

function post<T>(path: string, data: unknown): Promise<T> {
    return req<T>(path, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
}

export interface FileEntry {
    name: string;
    size: number;
    dir: boolean;
    mtime: number;
}

export interface DownloadTask {
    gid: string;
    url: string;
    dir: string;
    name: string;
    status: string;
    totalLength: number;
    completedLength: number;
    errorMsg: string;
    createdAt: string;
    speed: number;
}

export interface YtdlpOptions {
    embedMeta?: boolean;
    embedThumb?: boolean;
    subs?: boolean;
}

export interface YtdlpTask {
    id: number;
    url: string;
    dir: string;
    preset: string;
    options: string;
    status: string;
    progress: number;
    title: string;
    logTail: string;
    errorMsg: string;
    createdAt: string;
}

export interface Share {
    id: number;
    token: string;
    path: string;
    type: string; // page | direct
    hasPassword: boolean;
    expiresAt: string | null;
    createdAt: string;
}

export interface TrashItem {
    id: number;
    origPath: string;
    name: string;
    size: number;
    dir: boolean;
    deletedAt: string;
}

export interface IndexItem {
    path: string;
    name: string;
    size: number;
    dir: boolean;
    mtime: number;
    kind: string;
}

export interface ShareInfo {
    name: string;
    size: number;
    mtime: number;
    needPassword: boolean;
    expiresAt: string | null;
}

export interface Profile {
    user: string;
    avatar: string;
}

export interface DiskInfo {
    total: number;
    used: number;
    free: number;
    usedPercent: number;
}

export interface RecentFile {
    path: string;
    name: string;
    size: number;
    mtime: number;
}

export const api = {
    login: (username: string, password: string) =>
        post<{ ok: boolean } & Profile>('/api/v1/auth/login', { username, password }),
    logout: () => post<{ ok: boolean }>('/api/v1/auth/logout', {}),
    me: () => req<Profile>('/api/v1/auth/me'),
    changePassword: (oldPassword: string, newPassword: string) =>
        post<{ ok: boolean }>('/api/v1/auth/password', { oldPassword, newPassword }),
    updateProfile: (username: string, avatar: string) =>
        post<{ ok: boolean } & Profile>('/api/v1/auth/profile', { username, avatar }),

    listFiles: (path: string) =>
        req<{ path: string; entries: FileEntry[] }>(
            `/api/v1/files?path=${encodeURIComponent(path)}`,
        ),
    mkdir: (path: string) => post<{ ok: boolean }>('/api/v1/files/mkdir', { path }),
    upload: (dir: string, files: FileList | File[]) => {
        const fd = new FormData();
        for (const f of Array.from(files)) fd.append('file', f);
        return req<{ ok: boolean; saved: string[] }>(
            `/api/v1/files/upload?path=${encodeURIComponent(dir)}`,
            { method: 'POST', body: fd },
        );
    },
    downloadUrl: (path: string, dl = false) =>
        `/api/v1/files/download?path=${encodeURIComponent(path)}${dl ? '&dl=1' : ''}`,
    thumbUrl: (path: string) => `/api/v1/files/thumb?path=${encodeURIComponent(path)}`,
    content: (path: string) =>
        req<string>(`/api/v1/files/content?path=${encodeURIComponent(path)}`),
    rename: (path: string, newName: string) =>
        post<{ ok: boolean }>('/api/v1/files/rename', { path, newName }),
    move: (path: string, dest: string) =>
        post<{ ok: boolean }>('/api/v1/files/move', { path, dest }),
    remove: (paths: string[]) => post<{ ok: boolean }>('/api/v1/files/delete', { paths }),
    writeFile: (path: string, content: string) =>
        post<{ ok: boolean }>('/api/v1/files/write', { path, content }),

    trash: () =>
        req<{ items: TrashItem[]; retentionDays: number }>('/api/v1/trash'),
    restoreTrash: (id: number) => post<{ ok: boolean }>('/api/v1/trash/restore', { id }),
    deleteTrash: (id: number) => post<{ ok: boolean }>('/api/v1/trash/delete', { id }),
    emptyTrash: () => post<{ ok: boolean }>('/api/v1/trash/empty', {}),

    search: (q: string) =>
        req<{ results: IndexItem[] }>(`/api/v1/search?q=${encodeURIComponent(q)}`),
    category: (kind: string) =>
        req<{ items: IndexItem[] }>(`/api/v1/category?kind=${encodeURIComponent(kind)}`),
    stats: () => req<{ stats: Record<string, number> }>('/api/v1/stats'),

    storage: () => req<{ disk: DiskInfo; recent: RecentFile[] | null }>('/api/v1/storage'),

    downloads: () =>
        req<{ degraded: boolean; tasks: DownloadTask[] }>('/api/v1/downloads'),
    addDownload: (url: string, dir: string) =>
        post<{ ok: boolean }>('/api/v1/downloads', { url, dir }),
    pauseDownload: (gid: string) => post<{ ok: boolean }>('/api/v1/downloads/pause', { gid }),
    unpauseDownload: (gid: string) =>
        post<{ ok: boolean }>('/api/v1/downloads/unpause', { gid }),
    removeDownload: (gid: string) =>
        post<{ ok: boolean }>('/api/v1/downloads/remove', { gid }),

    ytdlp: () =>
        req<{ available: boolean; version: string; tasks: YtdlpTask[] }>('/api/v1/ytdlp'),
    addYtdlp: (url: string, dir: string, preset: string, options: YtdlpOptions) =>
        post<{ ok: boolean }>('/api/v1/ytdlp', { url, dir, preset, options }),
    cancelYtdlp: (id: number) => post<{ ok: boolean }>('/api/v1/ytdlp/cancel', { id }),
    deleteYtdlp: (id: number) => post<{ ok: boolean }>('/api/v1/ytdlp/delete', { id }),
    updateYtdlp: () =>
        post<{ ok: boolean; output: string }>('/api/v1/ytdlp/update', {}),

    shares: () => req<{ shares: Share[] }>('/api/v1/shares'),
    createShare: (path: string, password: string, type: string, expiresHours: number) =>
        post<{ ok: boolean; share: Share }>('/api/v1/shares', {
            path,
            password,
            type,
            expiresHours,
        }),
    deleteShare: (id: number) => post<{ ok: boolean }>('/api/v1/shares/delete', { id }),

    shareInfo: (token: string) => req<ShareInfo>(`/api/v1/public/share/${token}`),
    shareDownloadUrl: (token: string, password: string, dl = false) =>
        `/api/v1/public/share/${token}/download?password=${encodeURIComponent(password)}${dl ? '&dl=1' : ''}`,
};
