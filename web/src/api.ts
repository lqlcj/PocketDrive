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

/** BT 任务的文件清单里的一行(index 是 aria2 的 1-based 文件序号) */
export interface TorrentFile {
    index: number;
    path: string;
    length: number;
}

export interface YtdlpOptions {
    embedMeta?: boolean;
    embedThumb?: boolean;
    subs?: boolean;
    playlist?: boolean;
}

/** yt下载的高级设置:机房 IP 被 YouTube 判定为机器人时要用 */
export interface YtdlpSettings {
    proxy: string;
    playerClient: string;
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
    /** 是否上传过自定义头像;没有则前端用用户名首字母 */
    hasAvatar?: boolean;
    /** 头像版本号,换头像后用它破缓存 */
    avatarVersion?: string;
}

export interface DownloadSettings {
    maxConcurrent: number;
    maxDownloadLimit: string;
    maxUploadLimit: string;
    seedTimeMin: number;
    trackerAuto: boolean;
    defaultDir: string;
}

export interface DiskInfo {
    total: number;
    used: number;
    free: number;
    usedPercent: number;
}

/** 压缩/解压任务(异步,前端轮询进度) */
export interface ArchiveTask {
    id: number;
    kind: 'compress' | 'extract';
    status: 'running' | 'done' | 'error';
    src: string;
    dest: string;
    format: string;
    total: number;
    done: number;
    errorMsg: string;
    createdAt: string;
    updatedAt: string;
}

/** 外部组件(yt-dlp / aria2 / ffmpeg)的状态,可在网页里各自升级 */
/** 组件的升级渠道:只有 managed 能在网页里点一下就升级 */
export type ComponentChannel = 'managed' | 'image' | 'sidecar' | 'system';

export interface ComponentInfo {
    kind: string;
    title: string;
    note: string;
    installed: boolean;
    version: string;
    latest: string;
    outdated: boolean;
    channel: ComponentChannel;
    running: boolean;
    lastUpdated: string;
    /** 不能在网页里升级时,替代按钮显示的一句话 */
    updateHint: string;
}

/** 外部存储挂载的用量 */
export interface MountUsage {
    name: string;
    bytes: number;
    files: number;
    quota: number;
    pending: boolean;
}

/** 本机网盘目录的用量与容量上限(与外部存储同一套语义) */
export interface LocalUsage {
    bytes: number;
    files: number;
    /** 容量上限(字节),0 = 不限 */
    quota: number;
    /** 用量还在后台统计中 */
    pending: boolean;
}

export interface RecentFile {
    path: string;
    name: string;
    size: number;
    mtime: number;
}

export interface StoragePolicy {
    id: number;
    name: string;
    type: string;
    endpoint: string;
    region: string;
    bucket: string;
    accessKey: string;
    basePath: string;
    connected: boolean;
    /** 容量上限(字节),0 = 不限 */
    quotaBytes: number;
    usedBytes: number;
    usedFiles: number;
    /** 用量还在后台统计中 */
    usagePending: boolean;
}

export interface StoragePolicyInput {
    id?: number;
    name: string;
    endpoint: string;
    region: string;
    bucket: string;
    accessKey: string;
    secretKey: string;
    basePath: string;
    /** 容量上限(GB),0 或留空 = 不限 */
    quotaGB?: number;
}

export interface CloudSettings {
    /** WebDAV 读 @挂载 里的文件时 302 直连存储桶,不经 VPS 中转 */
    davDirect: boolean;
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

    icons: () => req<{ icons: Record<string, string> }>('/api/v1/icons'),
    setIcon: (path: string, icon: string) =>
        post<{ ok: boolean }>('/api/v1/icons', { path, icon }),

    storages: () => req<{ policies: StoragePolicy[] }>('/api/v1/storages'),
    saveStorage: (p: StoragePolicyInput) =>
        post<{ ok: boolean; id: number }>('/api/v1/storages', p),
    deleteStorage: (id: number) => post<{ ok: boolean }>('/api/v1/storages/delete', { id }),
    testStorage: (p: Partial<StoragePolicyInput>) =>
        post<{ ok: boolean }>('/api/v1/storages/test', p),
    cloudSettings: () => req<{ settings: CloudSettings }>('/api/v1/storages/settings'),
    saveCloudSettings: (s: Partial<CloudSettings>) =>
        post<{ ok: boolean; settings: CloudSettings }>('/api/v1/storages/settings', s),

    /**
     * 开始(或找回)一次分片上传。带上文件大小与修改时间,服务端就能认出
     * 「同一个文件传到同一个位置」,把已传的分片序号回给我们——断点续传。
     */
    uploadInit: (path: string, size: number, lastModified: number, chunkSize: number) =>
        post<{ id: string; uploaded: number[]; chunkSize: number }>(
            '/api/v1/files/upload/init',
            { path, size, lastModified, chunkSize },
        ),
    uploadChunk: async (id: string, index: number, blob: Blob) => {
        const resp = await fetch(
            `/api/v1/files/upload/chunk?id=${id}&index=${index}`,
            { method: 'POST', body: blob },
        );
        if (!resp.ok) {
            const body = await resp.json().catch(() => null);
            throw new ApiError(resp.status, body?.error ?? `分片 ${index} 上传失败`);
        }
    },
    uploadComplete: (id: string, path: string, chunks: number) =>
        post<{ ok: boolean }>('/api/v1/files/upload/complete', { id, path, chunks }),

    downloadSettings: () =>
        req<{
            settings: DownloadSettings;
            trackerCount: number;
            trackerUpdatedAt: string;
        }>('/api/v1/downloads/settings'),
    saveDownloadSettings: (s: DownloadSettings) =>
        post<{ ok: boolean }>('/api/v1/downloads/settings', s),
    updateTrackers: () =>
        post<{ ok: boolean; count: number }>('/api/v1/downloads/trackers/update', {}),

    storage: () =>
        req<{
            disk: DiskInfo;
            recent: RecentFile[] | null;
            mounts: MountUsage[] | null;
            local: LocalUsage;
        }>('/api/v1/storage'),
    /** 本机容量上限(GB),0 或留空 = 不限 */
    saveLocalQuota: (quotaGB: number) =>
        post<{ ok: boolean; quota: number }>('/api/v1/storage/quota', { quotaGB }),

    components: () =>
        req<{ components: ComponentInfo[]; managed: boolean }>('/api/v1/components'),    installComponent: (kind: string) =>
        post<{ ok: boolean; version: string }>('/api/v1/components/install', { kind }),

    /** 错误日志:只记 error,每天清空 */
    logs: () =>
        req<{ enabled: boolean; text: string; size: number }>('/api/v1/logs'),
    clearLogs: () => post<{ ok: boolean }>('/api/v1/logs/clear', {}),
    logsDownloadUrl: () => '/api/v1/logs/download',

    avatarUrl: (version: string) =>
        `/api/v1/public/avatar${version ? `?v=${encodeURIComponent(version)}` : ''}`,
    uploadAvatar: async (file: File) => {
        const resp = await fetch('/api/v1/auth/avatar', { method: 'POST', body: file });
        const body = await resp.json().catch(() => null);
        if (!resp.ok) throw new ApiError(resp.status, body?.error ?? '头像上传失败');
        return body as { ok: boolean; version: string };
    },
    deleteAvatar: () => post<{ ok: boolean }>('/api/v1/auth/avatar/delete', {}),

    archiveTasks: () => req<{ tasks: ArchiveTask[] }>('/api/v1/archive'),
    compress: (paths: string[], dest: string, format: string) =>
        post<{ ok: boolean; task: ArchiveTask }>('/api/v1/archive/compress', {
            paths,
            dest,
            format,
        }),
    extract: (path: string, dest: string) =>
        post<{ ok: boolean; task: ArchiveTask }>('/api/v1/archive/extract', { path, dest }),
    deleteArchiveTask: (id: number) => post<{ ok: boolean }>('/api/v1/archive/delete', { id }),

    exportUrl: () => '/api/v1/admin/export',
    importBackup: async (file: File, password: string) => {
        const resp = await fetch('/api/v1/admin/import', {
            method: 'POST',
            headers: { 'X-PD-Password': password },
            body: file,
        });
        const body = await resp.json().catch(() => null);
        if (!resp.ok) throw new ApiError(resp.status, body?.error ?? '导入失败');
        return body as { ok: boolean; files: number; database: boolean; note: string };
    },

    downloads: () =>
        req<{ degraded: boolean; tasks: DownloadTask[] }>('/api/v1/downloads'),
    addDownload: (url: string, dir: string) =>
        post<{ ok: boolean; task: DownloadTask }>('/api/v1/downloads', { url, dir }),
    /** paused 为真时任务先挂起,等用户在弹框里勾选文件后再继续 */
    addTorrent: (torrent: string, name: string, dir: string, paused = false) =>
        post<{ ok: boolean; task: DownloadTask }>('/api/v1/downloads/torrent', {
            torrent,
            name,
            dir,
            paused,
        }),
    /** 某个 BT 任务(种子/磁力)的文件清单,供弹框里打勾 */
    downloadFiles: (gid: string) =>
        req<{ name: string; files: TorrentFile[] }>(`/api/v1/downloads/${gid}/files`),
    /** 确认弹框勾选的文件并开始下载;files 为空 = 下载全部 */
    selectDownloadFiles: (gid: string, files: number[]) =>
        post<{ ok: boolean }>(`/api/v1/downloads/${gid}/select`, { files }),
    pauseDownload: (gid: string) => post<{ ok: boolean }>('/api/v1/downloads/pause', { gid }),
    unpauseDownload: (gid: string) =>
        post<{ ok: boolean }>('/api/v1/downloads/unpause', { gid }),
    /** deleteFiles 为真时连已下载的文件一起删 */
    removeDownload: (gid: string, deleteFiles = false) =>
        post<{ ok: boolean; deletedFiles: number }>('/api/v1/downloads/remove', {
            gid,
            deleteFiles,
        }),

    ytdlp: () =>
        req<{ available: boolean; version: string; tasks: YtdlpTask[] }>('/api/v1/ytdlp'),
    addYtdlp: (url: string, dir: string, preset: string, options: YtdlpOptions) =>
        post<{ ok: boolean }>('/api/v1/ytdlp', { url, dir, preset, options }),
    cancelYtdlp: (id: number) => post<{ ok: boolean }>('/api/v1/ytdlp/cancel', { id }),
    deleteYtdlp: (id: number) => post<{ ok: boolean }>('/api/v1/ytdlp/delete', { id }),
    ytdlpSettings: () =>
        req<{
            settings: YtdlpSettings;
            hasCookies: boolean;
            cookiesUpdated: string;
            cookiesSupported: boolean;
        }>('/api/v1/ytdlp/settings'),
    saveYtdlpSettings: (s: YtdlpSettings) =>
        post<{ ok: boolean }>('/api/v1/ytdlp/settings', s),
    /** content 传空串 = 删除已保存的 cookies */
    setYtdlpCookies: (content: string) =>
        post<{ ok: boolean; hasCookies: boolean; cookiesUpdated: string }>(
            '/api/v1/ytdlp/cookies',
            { content },
        ),

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
    shareThumbUrl: (token: string, password: string) =>
        `/api/v1/public/share/${token}/thumb?password=${encodeURIComponent(password)}`,
};
