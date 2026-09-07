import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { ArrowLeft, Folder, Upload, RotateCcw, RefreshCw } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { DownloadSettings as DS, DownloadSettingsResponse } from '../api';
import { Card, CardTitle } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { NativeSelect, Checkbox } from '../components/ui/input';
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
    const [trackerCount, setTrackerCount] = useState(0);
    const [trackerAt, setTrackerAt] = useState('');
    const [trackerSource, setTrackerSource] = useState<'default' | 'custom'>('default');
    const trackerInput = useRef<HTMLInputElement>(null);
    const [pickerOpen, setPickerOpen] = useState(false);
    const [saving, setSaving] = useState(false);
    const [updating, setUpdating] = useState(false);

    const applyTrackers = (r: DownloadSettingsResponse) => {
        setTrackerCount(r.trackerCount);
        setTrackerAt(r.trackerUpdatedAt);
        setTrackerSource(r.trackerSource);
    };

    useEffect(() => {
        api.downloadSettings()
            .then((r) => {
                setS(r.settings);
                applyTrackers(r);
            })
            .catch((e) => toast.error(e instanceof Error ? e.message : '加载失败'));
    }, []);

    const save = async () => {
        if (!s) return;
        setSaving(true);
        try {
            await api.saveDownloadSettings(s);
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
            applyTrackers(await api.downloadSettings());
            toast.success(`已更新 ${r.count} 条 tracker`);
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '更新失败');
        } finally {
            setUpdating(false);
        }
    };

    const importTrackers = async (file?: File) => {
        if (!file) return;
        if (!file.name.toLowerCase().endsWith('.txt')) {
            toast.error('请选择 .txt 文件');
            return;
        }
        if (file.size > 1024 * 1024) {
            toast.error('Tracker 文件不能超过 1MB');
            return;
        }
        setUpdating(true);
        try {
            const r = await api.importTrackers(file);
            applyTrackers(r);
            toast.success(`已导入 ${r.trackerCount} 条 Tracker`);
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '导入失败');
        } finally {
            setUpdating(false);
        }
    };

    const resetTrackers = async () => {
        setUpdating(true);
        try {
            applyTrackers(await api.resetTrackers());
            toast.success('已恢复默认 Tracker');
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '恢复失败');
        } finally {
            setUpdating(false);
        }
    };

    if (!s) {
        return <div className="text-center text-ink-soft py-16">加载中…</div>;
    }

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
                        {trackerSource === 'default' && <Checkbox
                            label="每日自动更新"
                            checked={s.trackerAuto}
                            onChange={(e) => setS({ ...s, trackerAuto: e.target.checked })}
                        />}
                        {trackerSource === 'default' && <Button size="sm" disabled={updating} onClick={refreshTrackers}>
                            <RefreshCw className="size-3.5" />
                            {updating ? '更新中…' : '立即更新'}
                        </Button>}
                        <span className="text-xs text-ink-soft break-words">
                            {trackerSource === 'custom' ? '自定义列表' : '默认列表'} · {trackerCount} 条
                            {trackerAt && ` · ${formatTime(trackerAt)} 更新`}
                        </span>
                    </div>,
                )}
                {row(
                    '导入 Tracker',
                    <div className="flex items-center gap-3 flex-wrap">
                        <input
                            ref={trackerInput}
                            type="file"
                            accept=".txt,text/plain"
                            aria-label="Tracker 文本文件"
                            className="hidden"
                            disabled={updating}
                            onChange={(e) => {
                                const file = e.currentTarget.files?.[0];
                                e.currentTarget.value = '';
                                void importTrackers(file);
                            }}
                        />
                        <Button size="sm" disabled={updating} onClick={() => trackerInput.current?.click()}>
                            <Upload className="size-3.5" /> {updating ? '处理中…' : '导入 .txt'}
                        </Button>
                        {trackerSource === 'custom' && <Button size="sm" disabled={updating} onClick={resetTrackers}>
                            <RotateCcw className="size-3.5" /> 恢复默认
                        </Button>}
                    </div>,
                )}
                <p className="text-xs text-ink-soft mt-2">
                    DHT / IPv6 等属于 aria2 启动项,由 aria2 侧配置(Docker 版默认已开 DHT)。
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
