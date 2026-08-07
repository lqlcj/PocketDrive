import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { ArrowLeft, Folder } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { DownloadSettings as DS } from '../api';
import { Card, CardTitle } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { NativeSelect, Checkbox, Textarea } from '../components/ui/input';
import FolderPicker from '../components/FolderPicker';
import { formatTime } from '../util';

const SPEEDS = [
    { v: '0', label: '不限速' },
    { v: '512K', label: '512 KB/s' },
    { v: '1M', label: '1 MB/s' },
    { v: '2M', label: '2 MB/s' },
    { v: '5M', label: '5 MB/s' },
    { v: '10M', label: '10 MB/s' },
    { v: '20M', label: '20 MB/s' },
];

const SEED_TIMES = [
    { v: 0, label: '下载完即停止(不做种)' },
    { v: 30, label: '做种 30 分钟' },
    { v: 60, label: '做种 1 小时' },
    { v: 360, label: '做种 6 小时' },
    { v: 1440, label: '做种 24 小时' },
];

export default function DownloadSettings() {
    const [s, setS] = useState<DS | null>(null);
    const [aria2Version, setAria2Version] = useState('');
    const [trackerCount, setTrackerCount] = useState(0);
    const [trackerAutoCount, setTrackerAutoCount] = useState(0);
    const [trackerCustomCount, setTrackerCustomCount] = useState(0);
    const [trackerAt, setTrackerAt] = useState('');
    const [trackerSource, setTrackerSource] = useState('');
    const [trackerError, setTrackerError] = useState('');
    const [pickerOpen, setPickerOpen] = useState(false);
    const [saving, setSaving] = useState(false);
    const [updating, setUpdating] = useState(false);

    useEffect(() => {
        api.downloadSettings()
            .then((r) => {
                setS(r.settings);
                setAria2Version(r.aria2Version);
                setTrackerCount(r.trackerCount);
                setTrackerAutoCount(r.trackerAutoCount);
                setTrackerCustomCount(r.trackerCustomCount);
                setTrackerAt(r.trackerUpdatedAt);
                setTrackerSource(r.trackerSource);
                setTrackerError(r.trackerLastError);
            })
            .catch((e) => toast.error(e instanceof Error ? e.message : '加载失败'));
    }, []);

    const save = async () => {
        if (!s) return;
        setSaving(true);
        try {
            const saved = await api.saveDownloadSettings(s);
            setS(saved.settings);
            const latest = await api.downloadSettings();
            setAria2Version(latest.aria2Version);
            setTrackerCount(latest.trackerCount);
            setTrackerAutoCount(latest.trackerAutoCount);
            setTrackerCustomCount(latest.trackerCustomCount);
            setTrackerAt(latest.trackerUpdatedAt);
            setTrackerSource(latest.trackerSource);
            setTrackerError(latest.trackerLastError);
            toast.success('已保存并即时应用到 aria2');
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '保存失败');
        } finally {
            setSaving(false);
        }
    };

    const refreshTrackers = async () => {
        setUpdating(true);
        try {
            const r = await api.updateTrackers();
            const latest = await api.downloadSettings();
            setS(latest.settings);
            setTrackerCount(latest.trackerCount);
            setTrackerAutoCount(latest.trackerAutoCount);
            setTrackerCustomCount(latest.trackerCustomCount);
            setTrackerAt(r.updatedAt);
            setTrackerSource(r.source);
            setTrackerError('');
            toast.success(`已更新 ${r.count} 条 tracker`);
        } catch (e) {
            // 后端会保留旧缓存并记录各镜像源的失败原因；即使本次请求
            // 返回 502，也立即刷新状态让用户看见“旧列表仍在使用”。
            try {
                const latest = await api.downloadSettings();
                setTrackerCount(latest.trackerCount);
                setTrackerAutoCount(latest.trackerAutoCount);
                setTrackerCustomCount(latest.trackerCustomCount);
                setTrackerAt(latest.trackerUpdatedAt);
                setTrackerSource(latest.trackerSource);
                setTrackerError(latest.trackerLastError);
            } catch {
                // 保留当前页面状态，原始更新错误仍会通过 toast 显示。
            }
            toast.error(e instanceof Error ? e.message : '更新失败');
        } finally {
            setUpdating(false);
        }
    };

    if (!s) {
        return <div className="text-center text-ink-soft py-16">加载中…</div>;
    }

    const trackerSourceLabel = (() => {
        if (!trackerSource) return '';
        try {
            return new URL(trackerSource).host;
        } catch {
            return trackerSource;
        }
    })();

    const row = (label: string, control: ReactNode, hint?: string) => (
        <div className="py-2.5 border-b border-line/50 last:border-b-0">
            <div className="flex items-center gap-3 flex-wrap">
                <span className="font-bold text-sm min-w-36">{label}</span>
                {control}
            </div>
            {hint && <p className="text-xs text-ink-soft mt-1">{hint}</p>}
        </div>
    );

    return (
        <div>
            <div className="flex items-center gap-3 mb-4">
                <h2 className="text-xl font-extrabold">下载设置</h2>
                <Link to="/downloads" className="ml-auto">
                    <Button size="sm">
                        <ArrowLeft className="size-3.5" /> 返回下载
                    </Button>
                </Link>
            </div>

            <Card>
                <CardTitle>全局(保存后即时生效,无需重启 aria2)</CardTitle>
                <p className="text-xs text-ink-soft mb-2">
                    下载引擎：{aria2Version ? `原版 aria2 ${aria2Version}` : 'aria2 暂时不可达'}
                </p>
                {row(
                    '最大同时下载数',
                    <NativeSelect
                        value={String(s.maxConcurrent)}
                        onChange={(e) => setS({ ...s, maxConcurrent: parseInt(e.target.value, 10) })}
                    >
                        {[1, 2, 3, 4, 5, 6, 8, 10].map((n) => (
                            <option key={n} value={n}>
                                {n}
                            </option>
                        ))}
                    </NativeSelect>,
                    '2G 内存 VPS 建议 3 个以内',
                )}
                {row(
                    '最大下载速度',
                    <NativeSelect
                        value={s.maxDownloadLimit}
                        onChange={(e) => setS({ ...s, maxDownloadLimit: e.target.value })}
                    >
                        {SPEEDS.map((sp) => (
                            <option key={sp.v} value={sp.v}>
                                {sp.label}
                            </option>
                        ))}
                    </NativeSelect>,
                )}
                {row(
                    '最大上传速度',
                    <NativeSelect
                        value={s.maxUploadLimit}
                        onChange={(e) => setS({ ...s, maxUploadLimit: e.target.value })}
                    >
                        {SPEEDS.map((sp) => (
                            <option key={sp.v} value={sp.v}>
                                {sp.label}
                            </option>
                        ))}
                    </NativeSelect>,
                    'BT 做种/上传会占用 VPS 带宽,可在这里限住',
                )}
            </Card>

            <Card className="mt-4">
                <CardTitle>BT 任务(对新添加的任务生效)</CardTitle>
                {row(
                    '做种策略',
                    <NativeSelect
                        value={String(s.seedTimeMin)}
                        onChange={(e) => setS({ ...s, seedTimeMin: parseInt(e.target.value, 10) })}
                    >
                        {SEED_TIMES.map((st) => (
                            <option key={st.v} value={st.v}>
                                {st.label}
                            </option>
                        ))}
                    </NativeSelect>,
                )}
                {row(
                    'Tracker 列表',
                    <div className="flex items-center gap-3 flex-wrap">
                        <Checkbox
                            label="每日自动更新"
                            checked={s.trackerAuto}
                            onChange={(e) => setS({ ...s, trackerAuto: e.target.checked })}
                        />
                        <Button size="sm" disabled={updating} onClick={refreshTrackers}>
                            {updating ? '更新中…' : '立即更新'}
                        </Button>
                        <span className="text-xs text-ink-soft">
                            生效 {trackerCount} 条 · 自动列表 {trackerAutoCount} 条 · 自定义{' '}
                            {trackerCustomCount} 条
                            {trackerAt && ` · ${formatTime(trackerAt)} 更新`}
                            {trackerSourceLabel && ` · 来源 ${trackerSourceLabel}`}
                        </span>
                    </div>,
                    '原版 aria2 不负责定时抓取列表；PocketDrive 每日从三个镜像源依次更新，首次启动与更新失败时保留兜底/旧列表',
                )}
                {trackerError && (
                    <p className="text-xs text-danger mt-1.5 break-all">
                        最近一次自动更新失败（旧列表仍在使用）：{trackerError}
                    </p>
                )}
                {row(
                    '自定义 Tracker',
                    <div className="w-full max-w-2xl">
                        <Textarea
                            rows={5}
                            spellCheck={false}
                            maxLength={64 * 1024}
                            value={s.customTrackers}
                            onChange={(e) => setS({ ...s, customTrackers: e.target.value })}
                            placeholder={'每行一条，例如：\nudp://tracker.example.com:6969/announce\nhttps://tracker.example.com/announce'}
                            className="font-mono text-xs"
                        />
                    </div>,
                    '支持 http、https、udp；自动去重。即使关闭每日自动更新，自定义列表仍会附加到新 BT 任务',
                )}
                <p className="text-xs text-ink-soft mt-2">
                    项目维护的 aria2 镜像默认开启 IPv4 DHT、PEX，并持久化 DHT 路由表；
                    如服务器具备公网 IPv6，可在部署目录的 .env 设置
                    ARIA2_ENABLE_DHT6=true 后重建容器。
                </p>
            </Card>

            <Card className="mt-4">
                <CardTitle>默认下载目录</CardTitle>
                <div className="flex items-center gap-2 flex-wrap">
                    <span className="bg-paper-2 rounded-full px-3.5 py-1.5 text-sm inline-flex items-center gap-1.5">
                        <Folder className="size-3.5 text-ink-soft" />
                        {s.defaultDir === '' ? '根目录' : s.defaultDir}
                    </span>
                    <Button size="sm" onClick={() => setPickerOpen(true)}>
                        选择目录
                    </Button>
                </div>
                <p className="text-xs text-ink-soft mt-2">
                    离线下载新建任务时默认使用这个目录
                </p>
            </Card>

            <div className="mt-4">
                <Button variant="primary" disabled={saving} onClick={save}>
                    {saving ? '保存中…' : '保存设置'}
                </Button>
            </div>

            <FolderPicker
                open={pickerOpen}
                initial={s.defaultDir}
                hideMounts
                onClose={() => setPickerOpen(false)}
                onSelect={(dir) => setS({ ...s, defaultDir: dir })}
            />
        </div>
    );
}
