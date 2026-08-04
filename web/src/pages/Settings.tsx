import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';
import {
    Clock,
    HardDrive,
    KeyRound,
    Radio,
    UserRound,
    Wrench,
} from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { DiskInfo, Profile, RecentFile } from '../api';
import { Card, CardTitle } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Badge, Progress } from '../components/ui/progress';
import KindIcon from '../components/KindIcon';
import { fileKind, formatBytes, formatTime } from '../util';
import { cn } from '../lib/utils';

// 两排头像:上排动物,下排植物
const AVATAR_ANIMALS = ['🦝', '🦊', '🐱', '🐶', '🐸', '🦉', '🐰', '🐼'];
const AVATAR_PLANTS = ['🌻', '🌵', '🍄', '🌿', '🌸', '🍁', '🌳', '🍑'];

export default function Settings({
    profile,
    onProfile,
}: {
    profile: Profile;
    onProfile: (p: Profile) => void;
}) {
    const [username, setUsername] = useState(profile.user);
    const [avatar, setAvatar] = useState(profile.avatar);
    const [savingProfile, setSavingProfile] = useState(false);

    const [oldPass, setOldPass] = useState('');
    const [newPass, setNewPass] = useState('');
    const [confirm, setConfirm] = useState('');
    const [saving, setSaving] = useState(false);

    const [disk, setDisk] = useState<DiskInfo | null>(null);
    const [recent, setRecent] = useState<RecentFile[]>([]);

    const [aria2OK, setAria2OK] = useState<boolean | null>(null);
    const [ytOK, setYtOK] = useState<boolean | null>(null);
    const [ytVer, setYtVer] = useState('');
    const [updating, setUpdating] = useState(false);
    const [updateOut, setUpdateOut] = useState('');

    useEffect(() => {
        api.storage()
            .then((r) => {
                setDisk(r.disk);
                setRecent(r.recent ?? []);
            })
            .catch(() => undefined);
        api.downloads().then((r) => setAria2OK(!r.degraded)).catch(() => setAria2OK(false));
        api.ytdlp()
            .then((r) => {
                setYtOK(r.available);
                setYtVer(r.version);
            })
            .catch(() => setYtOK(false));
    }, []);

    const saveProfile = async () => {
        setSavingProfile(true);
        try {
            const r = await api.updateProfile(username.trim(), avatar);
            onProfile({ user: r.user, avatar: r.avatar });
            toast.success('资料已更新,WebDAV 用户名同步生效');
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '保存失败');
        } finally {
            setSavingProfile(false);
        }
    };

    const changePass = async () => {
        if (!oldPass || !newPass) return;
        if (newPass !== confirm) {
            toast.warning('两次输入的新密码不一致');
            return;
        }
        setSaving(true);
        try {
            await api.changePassword(oldPass, newPass);
            toast.success('密码已修改,WebDAV 也使用新密码');
            setOldPass('');
            setNewPass('');
            setConfirm('');
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '修改失败');
        } finally {
            setSaving(false);
        }
    };

    const updateYtdlp = async () => {
        setUpdating(true);
        setUpdateOut('');
        try {
            const r = await api.updateYtdlp();
            setUpdateOut(r.output);
            (r.ok ? toast.success : toast.warning)(
                r.ok ? '更新命令已执行' : '更新命令返回了错误,见输出',
            );
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '更新失败');
        } finally {
            setUpdating(false);
        }
    };

    const davURL = `${window.location.origin}/dav/`;

    const kv = (label: string, value: ReactNode) => (
        <div className="flex items-center gap-2.5 py-1.5 text-sm flex-wrap">
            <span className="text-ink-soft min-w-32">{label}</span>
            {value}
        </div>
    );

    const avatarRow = (list: string[]) => (
        <div className="grid grid-cols-8 gap-1.5">
            {list.map((a) => (
                <button
                    key={a}
                    className={cn(
                        'text-xl rounded-xl py-1.5 cursor-pointer border transition-colors',
                        a === avatar
                            ? 'border-leaf bg-leaf-soft'
                            : 'border-transparent bg-paper-2 hover:border-line',
                    )}
                    onClick={() => setAvatar(a)}
                >
                    {a}
                </button>
            ))}
        </div>
    );

    return (
        <div>
            <h2 className="text-xl font-extrabold mb-4">设置</h2>

            <div className="grid md:grid-cols-2 gap-4 items-start">
                {/* 个人资料 + 修改密码合并 */}
                <Card>
                    <CardTitle>
                        <UserRound className="size-4 text-leaf-dark" /> 个人资料
                    </CardTitle>
                    <div className="flex flex-col gap-1.5">
                        {avatarRow(AVATAR_ANIMALS)}
                        {avatarRow(AVATAR_PLANTS)}
                    </div>
                    <div className="flex flex-col gap-2.5 mt-3">
                        <Input
                            placeholder="用户名(2-32 字符)"
                            value={username}
                            onChange={(e) => setUsername(e.target.value)}
                        />
                        <Button variant="primary" disabled={savingProfile} onClick={saveProfile}>
                            保存资料
                        </Button>
                        <p className="text-xs text-ink-soft">
                            改用户名后,WebDAV 登录用户名也会变,手机端记得同步修改
                        </p>
                    </div>

                    <div className="border-t border-line/70 mt-4 pt-4">
                        <div className="font-bold text-sm mb-2.5 flex items-center gap-1.5">
                            <KeyRound className="size-3.5 text-leaf-dark" /> 修改密码
                        </div>
                        <div className="flex flex-col gap-2.5">
                            <Input
                                type="password"
                                placeholder="当前密码"
                                value={oldPass}
                                autoComplete="current-password"
                                onChange={(e) => setOldPass(e.target.value)}
                            />
                            <Input
                                type="password"
                                placeholder="新密码(至少 6 位)"
                                value={newPass}
                                autoComplete="new-password"
                                onChange={(e) => setNewPass(e.target.value)}
                            />
                            <Input
                                type="password"
                                placeholder="确认新密码"
                                value={confirm}
                                autoComplete="new-password"
                                onChange={(e) => setConfirm(e.target.value)}
                            />
                            <Button variant="primary" disabled={saving} onClick={changePass}>
                                修改密码
                            </Button>
                        </div>
                    </div>
                </Card>

                <div className="flex flex-col gap-4">
                    {/* 仓库容量(从主页迁来) */}
                    <Card>
                        <CardTitle>
                            <HardDrive className="size-4 text-leaf-dark" /> 仓库容量
                        </CardTitle>
                        {disk ? (
                            <>
                                <Progress percent={disk.usedPercent} />
                                <p className="text-sm text-ink-soft mt-2">
                                    已用 {formatBytes(disk.used)} / 共 {formatBytes(disk.total)}
                                    ,剩余 {formatBytes(disk.free)}
                                </p>
                            </>
                        ) : (
                            <p className="text-sm text-ink-soft">读取中…</p>
                        )}
                    </Card>

                    {/* 新鲜事(从主页迁来) */}
                    <Card>
                        <CardTitle>
                            <Clock className="size-4 text-leaf-dark" /> 新鲜事
                        </CardTitle>
                        {recent.length === 0 ? (
                            <p className="text-sm text-ink-soft">
                                网盘还空空的,去「我的文件」传点东西吧
                            </p>
                        ) : (
                            <div className="flex flex-col gap-1.5">
                                {recent.map((f) => (
                                    <Link
                                        key={f.path}
                                        to={`/files/${
                                            f.path.includes('/')
                                                ? f.path.slice(0, f.path.lastIndexOf('/'))
                                                : ''
                                        }`}
                                        className="flex items-center gap-2 text-sm hover:bg-paper-2 rounded-xl px-2 py-1 -mx-2"
                                    >
                                        <KindIcon kind={fileKind(f.name)} />
                                        <span className="flex-1 min-w-0 truncate font-bold">
                                            {f.name}
                                        </span>
                                        <span className="text-xs text-ink-soft shrink-0">
                                            {formatTime(f.mtime).slice(5)}
                                        </span>
                                    </Link>
                                ))}
                            </div>
                        )}
                    </Card>
                </div>

                <Card>
                    <CardTitle>
                        <Radio className="size-4 text-leaf-dark" /> WebDAV
                    </CardTitle>
                    <p className="text-sm text-ink-soft mb-2">
                        手机播放器/文件管理器里添加 WebDAV 服务,即可直连整个网盘:
                    </p>
                    {kv('地址', <code className="bg-paper-2 rounded-lg px-2 py-0.5 break-all">{davURL}</code>)}
                    {kv('用户名', <code className="bg-paper-2 rounded-lg px-2 py-0.5">{profile.user}</code>)}
                    {kv('密码', <span className="text-sm">与网页登录密码相同</span>)}
                </Card>

                <Card>
                    <CardTitle>
                        <Wrench className="size-4 text-leaf-dark" /> 组件状态
                    </CardTitle>
                    {kv(
                        'aria2(离线下载)',
                        aria2OK === null ? (
                            '…'
                        ) : aria2OK ? (
                            <Badge tone="green">已连接</Badge>
                        ) : (
                            <Badge tone="red">不可达</Badge>
                        ),
                    )}
                    {kv(
                        'yt-dlp(yt下载)',
                        ytOK === null ? (
                            '…'
                        ) : ytOK ? (
                            <Badge tone="green">{ytVer || '可用'}</Badge>
                        ) : (
                            <Badge tone="red">不可用</Badge>
                        ),
                    )}
                    <div className="mt-2.5">
                        <Button size="sm" disabled={updating || !ytOK} onClick={updateYtdlp}>
                            {updating ? '更新中…' : '更新 yt-dlp'}
                        </Button>
                    </div>
                    {updateOut && (
                        <pre className="mt-2 max-h-40 overflow-auto bg-paper-2 rounded-xl p-2.5 text-[11px] whitespace-pre-wrap break-all">
                            {updateOut}
                        </pre>
                    )}
                </Card>
            </div>
        </div>
    );
}
