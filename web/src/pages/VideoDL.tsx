import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
    Clapperboard,
    Folder,
    FolderOpen,
    ListVideo,
    Music,
    ChevronDown,
    ChevronRight,
} from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { YtdlpTask, YtdlpSettings } from '../api';
import { Card } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Input, NativeSelect, Checkbox } from '../components/ui/input';
import { Progress, Badge } from '../components/ui/progress';
import FolderPicker from '../components/FolderPicker';
import { formatTime } from '../util';

const STATUS: Record<string, { text: string; tone: 'green' | 'blue' | 'orange' | 'red' | 'default' }> = {
    queued: { text: '排队中', tone: 'default' },
    running: { text: '下载中', tone: 'blue' },
    done: { text: '已完成', tone: 'green' },
    error: { text: '出错', tone: 'red' },
    canceled: { text: '已取消', tone: 'orange' },
};

const PRESETS = [
    { key: 'video_best', label: '最佳画质 (mp4)' },
    { key: 'video_1080', label: '1080p (mp4)' },
    { key: 'video_720', label: '720p (mp4)' },
    { key: 'video_480', label: '480p (mp4)' },
    { key: 'audio_m4a', label: '仅音频 (m4a)' },
    { key: 'audio_mp3', label: '仅音频 (mp3)' },
];

const PRESET_LABEL: Record<string, string> = {
    ...Object.fromEntries(PRESETS.map((p) => [p.key, p.label])),
    video: '最佳画质 (mp4)',
    audio: '仅音频 (m4a)',
};

// --extractor-args youtube:player_client= 的候选。不同客户端对 PO Token
// 和账号 cookie 的要求不一样,YouTube 还时不时改规则,所以留给用户试。
const PLAYER_CLIENTS = [
    { key: '', label: '默认(由 yt-dlp 自己挑)' },
    { key: 'tv', label: 'tv(配 cookies 时通常最稳)' },
    { key: 'tv_simply', label: 'tv_simply(不支持账号 cookies)' },
    { key: 'web_safari', label: 'web_safari' },
    { key: 'mweb', label: 'mweb' },
    { key: 'ios', label: 'ios(不支持账号 cookies)' },
    { key: 'android_vr', label: 'android_vr(不支持账号 cookies)' },
    { key: 'web_embedded', label: 'web_embedded(仅可嵌入的视频)' },
];

/**
 * 高级设置:cookies / 代理 / 播放器客户端。
 *
 * 存在的理由是 YouTube 对机房 IP 的反机器人策略——VPS 上直接下会报
 * "Sign in to confirm you're not a bot",官方给的可行解法就是带上浏览器
 * 导出的 cookies。cookies 存在服务器的配置目录里(和数据库同级),不进
 * 网盘、不进 WebDAV、不会被整盘导出带走。
 */
function Advanced() {
    const [open, setOpen] = useState(false);
    const [settings, setSettings] = useState<YtdlpSettings>({ proxy: '', playerClient: '' });
    const [hasCookies, setHasCookies] = useState(false);
    const [cookiesUpdated, setCookiesUpdated] = useState('');
    const [supported, setSupported] = useState(true);
    const [cookieText, setCookieText] = useState('');
    const [busy, setBusy] = useState(false);
    const fileRef = useRef<HTMLInputElement>(null);

    const load = useCallback(() => {
        api.ytdlpSettings()
            .then((r) => {
                setSettings(r.settings);
                setHasCookies(r.hasCookies);
                setCookiesUpdated(r.cookiesUpdated);
                setSupported(r.cookiesSupported);
            })
            .catch(() => undefined);
    }, []);

    useEffect(() => {
        if (open) load();
    }, [open, load]);

    const save = async () => {
        setBusy(true);
        try {
            await api.saveYtdlpSettings(settings);
            toast.success('已保存');
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '保存失败');
        } finally {
            setBusy(false);
        }
    };

    const saveCookies = async (content: string) => {
        setBusy(true);
        try {
            const r = await api.setYtdlpCookies(content);
            setHasCookies(r.hasCookies);
            setCookiesUpdated(r.cookiesUpdated ?? '');
            setCookieText('');
            toast.success(content.trim() ? 'cookies 已保存' : 'cookies 已删除');
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '保存失败');
        } finally {
            setBusy(false);
        }
    };

    const onFile = async (f: File | undefined) => {
        if (!f) return;
        if (f.size > 1 << 20) {
            toast.error('文件太大了,cookies.txt 不该有 1MB');
            return;
        }
        await saveCookies(await f.text());
        if (fileRef.current) fileRef.current.value = '';
    };

    return (
        <Card className="mb-4">
            <button
                className="flex items-center gap-1.5 text-sm font-bold cursor-pointer w-full text-left"
                onClick={() => setOpen((v) => !v)}
            >
                {open ? (
                    <ChevronDown className="size-4" />
                ) : (
                    <ChevronRight className="size-4" />
                )}
                高级设置
                {hasCookies && <Badge tone="green">已配置 cookies</Badge>}
                {settings.proxy && <Badge tone="blue">代理</Badge>}
            </button>

            {open && (
                <div className="mt-3 flex flex-col gap-4">
                    <div>
                        <div className="text-sm font-bold mb-1">YouTube cookies</div>
                        <p className="text-xs text-ink-soft mb-2 leading-relaxed">
                            报「Sign in to confirm you're not a bot」是因为 YouTube
                            把机房 IP 当成了机器人,传一份 cookies 就能过。做法:用浏览器的
                            cookies.txt 导出插件,在
                            <b>无痕窗口</b>登录 YouTube 后打开{' '}
                            <code className="bg-paper-2 rounded px-1">
                                youtube.com/robots.txt
                            </code>
                            ,导出 youtube.com 的 cookies,然后<b>关掉那个无痕窗口</b>
                            (不关的话 YouTube 会把这份 cookie 轮换掉)。建议用小号。
                        </p>
                        <div className="text-xs mb-2">
                            {!supported ? (
                                <span className="text-ink-soft">
                                    当前部署没有配置目录,存不了 cookies
                                </span>
                            ) : hasCookies ? (
                                <span className="text-leaf-dark">
                                    已保存
                                    {cookiesUpdated && `(${formatTime(cookiesUpdated)})`}
                                </span>
                            ) : (
                                <span className="text-ink-soft">未配置</span>
                            )}
                        </div>
                        <textarea
                            className="w-full h-24 bg-paper-2 rounded-xl p-2.5 text-[11px] font-mono resize-y outline-none border border-line/70 focus:border-leaf"
                            placeholder="把 cookies.txt 的内容整个粘贴进来,或用下面的按钮选文件"
                            value={cookieText}
                            disabled={!supported}
                            onChange={(e) => setCookieText(e.target.value)}
                        />
                        <div className="flex gap-2 mt-2 flex-wrap">
                            <Button
                                variant="primary"
                                size="sm"
                                disabled={busy || !supported || !cookieText.trim()}
                                onClick={() => saveCookies(cookieText)}
                            >
                                保存 cookies
                            </Button>
                            <Button
                                size="sm"
                                disabled={busy || !supported}
                                onClick={() => fileRef.current?.click()}
                            >
                                选择 cookies.txt
                            </Button>
                            {hasCookies && (
                                <Button
                                    variant="ghost-danger"
                                    size="sm"
                                    disabled={busy}
                                    onClick={() => saveCookies('')}
                                >
                                    删除已保存的
                                </Button>
                            )}
                            <input
                                ref={fileRef}
                                type="file"
                                accept=".txt,text/plain"
                                className="hidden"
                                onChange={(e) => void onFile(e.target.files?.[0])}
                            />
                        </div>
                    </div>

                    <div className="flex flex-col gap-2">
                        <div className="text-sm font-bold">代理(可选)</div>
                        <Input
                            placeholder="socks5://127.0.0.1:1080 或 http://127.0.0.1:8080,留空为直连"
                            value={settings.proxy}
                            onChange={(e) =>
                                setSettings((s) => ({ ...s, proxy: e.target.value }))
                            }
                        />
                        <p className="text-xs text-ink-soft">
                            走容器网络,127.0.0.1 指的是 PocketDrive 容器自己;宿主机上的代理要写宿主 IP
                        </p>
                    </div>

                    <div className="flex flex-col gap-2">
                        <div className="text-sm font-bold">播放器客户端(可选)</div>
                        <NativeSelect
                            value={settings.playerClient}
                            onChange={(e) =>
                                setSettings((s) => ({ ...s, playerClient: e.target.value }))
                            }
                        >
                            {PLAYER_CLIENTS.map((c) => (
                                <option key={c.key} value={c.key}>
                                    {c.label}
                                </option>
                            ))}
                        </NativeSelect>
                        <p className="text-xs text-ink-soft">
                            默认下不去时可以换一个试试;只对 YouTube 生效
                        </p>
                    </div>

                    <div>
                        <Button variant="primary" size="sm" disabled={busy} onClick={save}>
                            保存代理与客户端
                        </Button>
                    </div>
                </div>
            )}
        </Card>
    );
}

export default function VideoDL() {
    const navigate = useNavigate();
    const [url, setUrl] = useState('');
    const [dir, setDir] = useState('');
    const [pickerOpen, setPickerOpen] = useState(false);
    const [preset, setPreset] = useState('video_best');
    const [embedThumb, setEmbedThumb] = useState(false);
    const [embedMeta, setEmbedMeta] = useState(false);
    const [subs, setSubs] = useState(false);
    const [playlist, setPlaylist] = useState(false);
    const [tasks, setTasks] = useState<YtdlpTask[]>([]);
    const [available, setAvailable] = useState(true);
    const [version, setVersion] = useState('');
    const [adding, setAdding] = useState(false);
    const [logOpen, setLogOpen] = useState<number | null>(null);

    const load = useCallback(() => {
        api.ytdlp()
            .then((r) => {
                setTasks(r.tasks);
                setAvailable(r.available);
                setVersion(r.version);
            })
            .catch(() => undefined);
    }, []);

    useEffect(() => {
        load();
        // 默认保存目录跟随「下载设置」
        api.downloadSettings()
            .then((r) => setDir((d) => (d === '' ? r.settings.defaultDir : d)))
            .catch(() => undefined);
        const t = setInterval(load, 2000);
        return () => clearInterval(t);
    }, [load]);

    const add = async () => {
        if (!url.trim()) return;
        setAdding(true);
        try {
            await api.addYtdlp(url.trim(), dir, preset, {
                embedThumb,
                embedMeta,
                subs,
                playlist,
            });
            toast.success(playlist ? '播放列表任务已加入队列' : '任务已加入队列');
            setUrl('');
            load();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '添加失败');
        } finally {
            setAdding(false);
        }
    };

    const op = async (fn: () => Promise<unknown>) => {
        try {
            await fn();
            load();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '操作失败');
        }
    };

    const isPlaylistTask = (t: YtdlpTask) => {
        try {
            return Boolean(JSON.parse(t.options || '{}').playlist);
        } catch {
            return false;
        }
    };

    return (
        <div>
            <div className="flex items-center gap-3 mb-4 flex-wrap">
                <h2 className="text-xl font-extrabold">yt下载</h2>
                {available ? (
                    version && <Badge tone="green">yt-dlp {version}</Badge>
                ) : (
                    <Badge tone="red">yt-dlp 不可用</Badge>
                )}
            </div>

            <Card className="mb-4 flex flex-col gap-2.5">
                <Input
                    placeholder="粘贴视频页链接,支持 yt-dlp 的所有站点"
                    value={url}
                    onChange={(e) => setUrl(e.target.value)}
                />
                <div className="flex items-center gap-2 flex-wrap">
                    <div className="flex items-center gap-2 bg-paper-2 rounded-full px-3.5 py-1.5 flex-1 min-w-44">
                        <Folder className="size-4 text-ink-soft shrink-0" />
                        <span className="text-sm truncate flex-1">
                            {dir === '' ? '根目录' : dir}
                        </span>
                        <Button size="sm" onClick={() => setPickerOpen(true)}>
                            选择目录
                        </Button>
                    </div>
                    <NativeSelect value={preset} onChange={(e) => setPreset(e.target.value)}>
                        {PRESETS.map((p) => (
                            <option key={p.key} value={p.key}>
                                {p.label}
                            </option>
                        ))}
                    </NativeSelect>
                    <Button variant="primary" disabled={adding || !available} onClick={add}>
                        添加任务
                    </Button>
                </div>
                <div className="flex gap-4 flex-wrap">
                    <Checkbox
                        label="嵌入封面"
                        checked={embedThumb}
                        onChange={(e) => setEmbedThumb(e.target.checked)}
                    />
                    <Checkbox
                        label="嵌入元数据"
                        checked={embedMeta}
                        onChange={(e) => setEmbedMeta(e.target.checked)}
                    />
                    <Checkbox
                        label="下载字幕(中英)"
                        checked={subs}
                        onChange={(e) => setSubs(e.target.checked)}
                    />
                    <Checkbox
                        label="整个播放列表批量下载"
                        checked={playlist}
                        onChange={(e) => setPlaylist(e.target.checked)}
                    />
                </div>
                {playlist && (
                    <p className="text-xs text-ink-soft">
                        播放列表会存进「播放列表名」子文件夹,文件名带序号;仅对合集/列表链接生效
                    </p>
                )}
            </Card>

            <FolderPicker
                open={pickerOpen}
                initial={dir}
                hideMounts
                onClose={() => setPickerOpen(false)}
                onSelect={setDir}
            />

            <Advanced />

            {tasks.length === 0 ? (
                <Card className="text-center text-ink-soft py-10 text-sm">
                    暂无任务,贴个链接试试
                </Card>
            ) : (
                <div className="flex flex-col gap-3">
                    {tasks.map((t) => {
                        const s = STATUS[t.status] ?? { text: t.status, tone: 'default' as const };
                        const TaskIcon = isPlaylistTask(t)
                            ? ListVideo
                            : t.preset.startsWith('audio')
                              ? Music
                              : Clapperboard;
                        return (
                            <Card key={t.id}>
                                <div className="flex items-center gap-2 mb-1.5 flex-wrap">
                                    <span
                                        className="flex-1 min-w-0 font-bold truncate inline-flex items-center gap-1.5"
                                        title={t.title || t.url}
                                    >
                                        <TaskIcon className="size-4 text-leaf-dark shrink-0" />
                                        <span className="truncate">{t.title || t.url}</span>
                                    </span>
                                    <Badge>{PRESET_LABEL[t.preset] ?? t.preset}</Badge>
                                    <Badge tone={s.tone}>{s.text}</Badge>
                                </div>
                                {(t.status === 'running' || t.status === 'queued') && (
                                    <Progress percent={t.progress} />
                                )}
                                <div className="flex gap-3.5 flex-wrap text-xs text-ink-soft mt-2 items-center">
                                    <span className="inline-flex items-center gap-1">
                                        <Folder className="size-3" />
                                        {t.dir || '根目录'}
                                    </span>
                                    <span>{formatTime(t.createdAt)}</span>
                                </div>
                                {t.errorMsg && (
                                    <div className="text-danger text-xs mt-1.5 break-all">
                                        {t.errorMsg}
                                    </div>
                                )}
                                {t.logTail && (
                                    <div className="mt-1.5">
                                        <button
                                            className="text-xs text-ink-soft underline cursor-pointer"
                                            onClick={() =>
                                                setLogOpen(logOpen === t.id ? null : t.id)
                                            }
                                        >
                                            {logOpen === t.id ? '收起日志' : '输出日志'}
                                        </button>
                                        {logOpen === t.id && (
                                            <pre className="mt-1.5 max-h-48 overflow-auto bg-paper-2 rounded-xl p-2.5 text-[11px] whitespace-pre-wrap break-all">
                                                {t.logTail}
                                            </pre>
                                        )}
                                    </div>
                                )}
                                <div className="flex gap-1.5 mt-2.5">
                                    {(t.status === 'running' || t.status === 'queued') && (
                                        <Button
                                            size="sm"
                                            onClick={() => op(() => api.cancelYtdlp(t.id))}
                                        >
                                            取消
                                        </Button>
                                    )}
                                    <Button
                                        size="sm"
                                        onClick={() => navigate(`/files/${t.dir}`)}
                                    >
                                        <FolderOpen className="size-3.5" /> 打开目录
                                    </Button>
                                    <Button
                                        variant="ghost-danger"
                                        size="sm"
                                        onClick={() => op(() => api.deleteYtdlp(t.id))}
                                    >
                                        删除记录
                                    </Button>
                                </div>
                            </Card>
                        );
                    })}
                </div>
            )}
        </div>
    );
}
