import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { toast } from 'sonner';
import { api } from '../api';
import type { LocalUsage, Profile, RecentFile } from '../api';
import { fileKind, formatBytes, formatTime } from '../util';
import KindIcon from './KindIcon';
import { Button } from './ui/button';
import { Card, CardTitle } from './ui/card';
import { Checkbox, Input } from './ui/input';
import { Progress } from './ui/progress';

export default function StorageSettingsCards({ profile }: { profile: Profile }) {
    const [recent, setRecent] = useState<RecentFile[]>([]);
    const [local, setLocal] = useState<LocalUsage | null>(null);
    const [davDirect, setDavDirect] = useState<boolean | null>(null);
    // 与服务端默认值一致，加载期间不会把 WebDAV 误显示为关闭。
    const [davEnabled, setDavEnabled] = useState<boolean | null>(true);
    const [davUser, setDavUser] = useState('');
    const [davPassword, setDavPassword] = useState('');
    const [savingDav, setSavingDav] = useState(false);

    useEffect(() => {
        api.storage()
            .then((r) => {
                setRecent(r.recent ?? []);
                setLocal(r.local);
            })
            .catch(() => undefined);
        api.cloudSettings()
            .then((r) => setDavDirect(r.settings.davDirect))
            .catch(() => undefined);
        api.webdavSettings()
            .then((r) => {
                setDavEnabled(r.settings.enabled);
                setDavUser(r.settings.user);
            })
            .catch(() => undefined);
    }, []);

    const toggleDavDirect = async (on: boolean) => {
        const previous = davDirect;
        setDavDirect(on);
        try {
            const r = await api.saveCloudSettings({ davDirect: on });
            setDavDirect(r.settings.davDirect);
            toast.success(on ? '已开启:播放器将直连存储桶' : '已关闭:改由本站中转');
        } catch (e) {
            setDavDirect(previous);
            toast.error(e instanceof Error ? e.message : '保存失败');
        }
    };

    const saveDav = async () => {
        setSavingDav(true);
        try {
            const r = await api.saveWebdavSettings({
                enabled: davEnabled ?? true,
                user: davUser,
                ...(davPassword ? { password: davPassword } : {}),
            });
            setDavEnabled(r.settings.enabled);
            setDavUser(r.settings.user);
            setDavPassword('');
            toast.success('WebDAV 设置已保存');
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '保存失败');
        } finally {
            setSavingDav(false);
        }
    };

    const davURL = `${window.location.origin}/dav/`;

    return (
        <>
            <Card className="h-full">
                <CardTitle>最近修改</CardTitle>
                {recent.length === 0 ? (
                    <p className="text-sm text-ink-soft">暂无最近修改的文件</p>
                ) : (
                    <div className="flex flex-col gap-1.5">
                        {recent.map((file) => (
                            <Link
                                key={file.path}
                                to={`/files/${
                                    file.path.includes('/')
                                        ? file.path.slice(0, file.path.lastIndexOf('/'))
                                        : ''
                                }`}
                                className="flex items-center gap-2 text-sm hover:bg-paper-2 rounded-lg px-2 py-1 -mx-2"
                            >
                                <KindIcon kind={fileKind(file.name)} />
                                <span className="flex-1 min-w-0 truncate font-bold">
                                    {file.name}
                                </span>
                                <span className="text-xs text-ink-soft shrink-0">
                                    {formatTime(file.mtime).slice(5)}
                                </span>
                            </Link>
                        ))}
                    </div>
                )}
            </Card>

            <Card className="h-full">
                <CardTitle>仓库容量</CardTitle>
                {local === null ? (
                    <p className="text-sm text-ink-soft">读取中…</p>
                ) : local.pending ? (
                    <p className="text-sm text-ink-soft">用量统计中…</p>
                ) : local.quota > 0 ? (
                    <>
                        <Progress percent={(local.bytes / local.quota) * 100} />
                        <p className="text-sm text-ink-soft mt-1.5">
                            已用 {formatBytes(local.bytes)} / 上限 {formatBytes(local.quota)}
                            {local.files > 0 && `,${local.files} 个文件`}
                            {local.bytes > local.quota && (
                                <span className="text-danger"> · 已超出</span>
                            )}
                        </p>
                    </>
                ) : (
                    <p className="text-sm text-ink-soft">
                        已用 {formatBytes(local.bytes)}
                        {local.files > 0 && `,${local.files} 个文件`}{' '}
                        <span className="text-xs">(未设上限)</span>
                    </p>
                )}
            </Card>

            <Card className="h-full">
                <CardTitle>WebDAV</CardTitle>
                <div className="flex items-center justify-between gap-3 mb-3">
                    <span className="text-sm font-bold">启用 WebDAV</span>
                    <Checkbox
                        label={davEnabled ? '已开启' : '已关闭'}
                        checked={davEnabled === true}
                        disabled={davEnabled === null}
                        onChange={(e) => setDavEnabled(e.target.checked)}
                    />
                </div>
                <div className="flex flex-col gap-2 text-sm">
                    <div className="flex gap-2 flex-wrap">
                        <span className="text-ink-soft w-16">地址</span>
                        <code className="bg-paper-2 rounded px-2 py-0.5 break-all">{davURL}</code>
                    </div>
                    <div className="flex gap-2 flex-wrap">
                        <span className="text-ink-soft w-16">用户名</span>
                        <Input
                            className="flex-1 min-w-40"
                            value={davUser}
                            placeholder={profile.user}
                            onChange={(e) => setDavUser(e.target.value)}
                        />
                    </div>
                    <div className="flex gap-2 flex-wrap">
                        <span className="text-ink-soft w-16">密码</span>
                        <Input
                            className="flex-1 min-w-40"
                            type="password"
                            autoComplete="new-password"
                            value={davPassword}
                            placeholder="留空不修改"
                            onChange={(e) => setDavPassword(e.target.value)}
                        />
                    </div>
                </div>
                {davDirect !== null && (
                    <div className="border-t border-line mt-3 pt-3">
                        <Checkbox
                            label="外部存储直连(不经本站中转)"
                            checked={davDirect}
                            onChange={(e) => toggleDavDirect(e.target.checked)}
                        />
                        <p className="text-xs text-ink-soft mt-1.5">
                            开启后,播放外部挂载文件不占本机流量;客户端不支持重定向时请关闭。
                        </p>
                    </div>
                )}
                <div className="border-t border-line mt-3 pt-3 flex items-end gap-2 flex-wrap">
                    <Button
                        size="sm"
                        variant="primary"
                        disabled={savingDav || davEnabled === null}
                        onClick={saveDav}
                    >
                        {savingDav ? '保存中…' : '保存 WebDAV 设置'}
                    </Button>
                </div>
            </Card>
        </>
    );
}
