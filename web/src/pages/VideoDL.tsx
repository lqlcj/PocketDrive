import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Clapperboard, Folder, FolderOpen, ListVideo, Music, Youtube } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { YtdlpTask } from '../api';
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
                <h2 className="text-xl font-extrabold flex items-center gap-2">
                    <Youtube className="size-5 text-leaf-dark" /> yt下载
                </h2>
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
                onClose={() => setPickerOpen(false)}
                onSelect={setDir}
            />

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
