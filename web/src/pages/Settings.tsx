import { useCallback, useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { ChevronRight } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type {
    ComponentInfo,
    DiskInfo,
    LocalUsage,
    MountUsage,
    Profile,
    RecentFile,
} from '../api';
import { Card, CardTitle } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Input, Checkbox } from '../components/ui/input';
import { Dialog, DialogContent, DialogFooter } from '../components/ui/dialog';
import { Badge, Progress } from '../components/ui/progress';
import KindIcon from '../components/KindIcon';
import Avatar from '../components/Avatar';
import { fileKind, formatBytes, formatTime, copyText } from '../util';

// compose 文件在服务器上的哪个目录,容器里是看不出来的(compose 只把
// 工作目录写进容器 label,进程自己读不到),所以让用户自己填、记在
// localStorage 里。默认是一键安装脚本用的路径。
const DEFAULT_COMPOSE_DIR = '/opt/pocketdrive';
// 1Panel 装的编排在这个目录下,按编排名分子目录——这是最常见的第二种情况
const ONEPANEL_COMPOSE_DIR = '/opt/1panel/docker/compose/pocketdrive';
const upgradeCmd = (dir: string) =>
    `cd ${dir || DEFAULT_COMPOSE_DIR} && docker compose pull && docker compose up -d`;

/**
 * 错误日志卡片。
 *
 * 只记 error、每天清空——所以这里看到的永远是「今天出过什么问题」。
 * 出了毛病但现场已经过去时,这是唯一还能翻的东西;不想 ssh 上服务器
 * 看 docker logs 的时候也用它。
 */
function ErrorLogCard() {
    const [text, setText] = useState('');
    const [size, setSize] = useState(0);
    const [enabled, setEnabled] = useState(true);
    const [open, setOpen] = useState(false);
    const [loading, setLoading] = useState(false);

    const load = useCallback(() => {
        setLoading(true);
        api.logs()
            .then((r) => {
                setText(r.text);
                setSize(r.size);
                setEnabled(r.enabled);
            })
            .catch(() => undefined)
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => {
        if (open) load();
    }, [open, load]);

    const clear = async () => {
        try {
            await api.clearLogs();
            setText('');
            setSize(0);
            toast.success('日志已清空');
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '清空失败');
        }
    };

    return (
        <Card>
            <CardTitle>错误日志</CardTitle>
            <p className="text-xs text-ink-soft leading-relaxed mb-2">
                只记录出错的事(上传失败、下载任务报错、服务器内部错误等),每天零点自动清空。
                遇到问题又不方便上服务器时,把这里的内容贴出来就能查。
            </p>
            <div className="flex items-center gap-2 flex-wrap">
                <Button size="sm" onClick={() => setOpen((v) => !v)}>
                    {open ? '收起' : '查看今天的日志'}
                </Button>
                {open && (
                    <>
                        <Button size="sm" variant="ghost" onClick={load} disabled={loading}>
                            刷新
                        </Button>
                        <a href={api.logsDownloadUrl()} download>
                            <Button size="sm" variant="ghost">
                                下载
                            </Button>
                        </a>
                        <Button size="sm" variant="ghost-danger" onClick={clear}>
                            清空
                        </Button>
                        <span className="text-xs text-ink-soft ml-auto">
                            {formatBytes(size)}
                        </span>
                    </>
                )}
            </div>
            {open && (
                <pre className="mt-2 max-h-72 overflow-auto bg-paper-2 rounded-xl p-2.5 text-[11px] whitespace-pre-wrap break-all">
                    {!enabled
                        ? '当前以本机开发方式运行,错误只写在控制台,没有落盘'
                        : loading && text === ''
                          ? '读取中…'
                          : text || '今天没有出错记录'}
                </pre>
            )}
        </Card>
    );
}

export default function Settings({
    profile,
    onProfile,
}: {
    profile: Profile;
    onProfile: (p: Profile) => void;
}) {
    const [username, setUsername] = useState(profile.user);

    // compose 目录记在浏览器里就够了:它只用来拼一条给用户复制的命令
    const [composeDir, setComposeDir] = useState(
        () => localStorage.getItem('pd_compose_dir') ?? DEFAULT_COMPOSE_DIR,
    );
    const saveComposeDir = (v: string) => {
        setComposeDir(v);
        localStorage.setItem('pd_compose_dir', v);
    };

    const [oldPass, setOldPass] = useState('');
    const [newPass, setNewPass] = useState('');
    const [confirm, setConfirm] = useState('');
    const [saving, setSaving] = useState(false);

    const [disk, setDisk] = useState<DiskInfo | null>(null);
    const [local, setLocal] = useState<LocalUsage | null>(null);
    const [recent, setRecent] = useState<RecentFile[]>([]);
    const [mounts, setMounts] = useState<MountUsage[]>([]);

    const [comps, setComps] = useState<ComponentInfo[] | null>(null);
    const [installing, setInstalling] = useState('');

    // WebDAV 读外部存储是否直连存储桶(null = 还没读到)
    const [davDirect, setDavDirect] = useState<boolean | null>(null);

    // 头像:上传图片存在配置目录,不进网盘
    const avatarInput = useRef<HTMLInputElement>(null);
    const [avatarBusy, setAvatarBusy] = useState(false);

    // 导入会覆盖现有网盘,要二次确认并验密码
    const importInput = useRef<HTMLInputElement>(null);
    const [importFile, setImportFile] = useState<File | null>(null);
    const [importPass, setImportPass] = useState('');
    const [importing, setImporting] = useState(false);

    const loadComponents = useCallback(() => {
        api.components()
            .then((r) => setComps(r.components))
            .catch(() => undefined);
    }, []);

    const loadStorage = useCallback(() => {
        api.storage()
            .then((r) => {
                setDisk(r.disk);
                setLocal(r.local);
                setRecent(r.recent ?? []);
                setMounts(r.mounts ?? []);
            })
            .catch(() => undefined);
    }, []);

    useEffect(() => {
        loadStorage();
        loadComponents();
    }, [loadStorage, loadComponents]);

    useEffect(() => {
        api.cloudSettings()
            .then((r) => setDavDirect(r.settings.davDirect))
            .catch(() => undefined);
    }, []);

    // 直连开关立即生效,不跟"保存资料"那个按钮混在一起;失败就回滚复选框
    const toggleDavDirect = async (on: boolean) => {
        const prev = davDirect;
        setDavDirect(on);
        try {
            const r = await api.saveCloudSettings({ davDirect: on });
            setDavDirect(r.settings.davDirect);
            toast.success(
                on ? '已开启:播放器将直连存储桶' : '已关闭:改由本站中转',
            );
        } catch (e) {
            setDavDirect(prev);
            toast.error(e instanceof Error ? e.message : '保存失败');
        }
    };

    // 资料 + 密码一个按钮保存:改了哪部分就提交哪部分
    const save = async () => {
        const profileChanged = username.trim() !== profile.user;
        const wantPass = oldPass !== '' || newPass !== '' || confirm !== '';
        if (wantPass) {
            if (!oldPass || !newPass) {
                toast.warning('修改密码需要填写当前密码和新密码');
                return;
            }
            if (newPass !== confirm) {
                toast.warning('两次输入的新密码不一致');
                return;
            }
        }
        if (!profileChanged && !wantPass) {
            toast.info('没有需要保存的改动');
            return;
        }
        setSaving(true);
        try {
            if (profileChanged) {
                const r = await api.updateProfile(username.trim(), '');
                onProfile({ ...profile, user: r.user });
            }
            if (wantPass) {
                await api.changePassword(oldPass, newPass);
                setOldPass('');
                setNewPass('');
                setConfirm('');
            }
            toast.success(
                wantPass
                    ? '已保存,新密码即刻生效(WebDAV 同步)'
                    : '资料已更新,WebDAV 用户名同步生效',
            );
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '保存失败');
        } finally {
            setSaving(false);
        }
    };

    const onAvatarPick = async (file: File | undefined) => {
        if (!file) return;
        setAvatarBusy(true);
        try {
            const r = await api.uploadAvatar(file);
            onProfile({ ...profile, hasAvatar: true, avatarVersion: r.version });
            toast.success('头像已更新');
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '头像上传失败');
        } finally {
            setAvatarBusy(false);
            if (avatarInput.current) avatarInput.current.value = '';
        }
    };

    const removeAvatar = async () => {
        setAvatarBusy(true);
        try {
            await api.deleteAvatar();
            onProfile({ ...profile, hasAvatar: false, avatarVersion: '' });
            toast.success('已恢复为首字母头像');
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '删除失败');
        } finally {
            setAvatarBusy(false);
        }
    };

    const installComponent = async (kind: string, title: string) => {
        setInstalling(kind);
        try {
            const r = await api.installComponent(kind);
            toast.success(`${title} 已更新到 ${r.version || '最新版'}`);
            loadComponents();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '更新失败');
        } finally {
            setInstalling('');
        }
    };

    const copyUpgradeCmd = async () => {
        if (await copyText(upgradeCmd(composeDir))) toast.success('命令已复制');
        else toast.warning('复制失败,请手动选中');
    };

    const doImport = async () => {
        if (!importFile) return;
        setImporting(true);
        try {
            const r = await api.importBackup(importFile, importPass);
            toast.success(`已恢复 ${r.files} 个文件`, { description: r.note, duration: 10000 });
            setImportFile(null);
            setImportPass('');
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '导入失败');
        } finally {
            setImporting(false);
        }
    };

    const davURL = `${window.location.origin}/dav/`;

    const kv = (label: string, value: ReactNode) => (
        <div className="flex items-center gap-2.5 py-1.5 text-sm flex-wrap">
            <span className="text-ink-soft min-w-32">{label}</span>
            {value}
        </div>
    );

    return (
        <div>
            <h2 className="text-xl font-extrabold mb-4">设置</h2>

            {/* 两列各自成流:卡片高度悬殊时不会互相拉出空洞 */}
            <div className="grid md:grid-cols-2 gap-3 items-start">
                {/* 左列:账户 */}
                <div className="flex flex-col gap-3">
                    {/* 个人资料 + 修改密码:一个按钮统一保存 */}
                    <Card>
                        <CardTitle>个人资料</CardTitle>
                        <div className="flex items-center gap-3">
                            <Avatar profile={profile} size="lg" />
                            <div className="flex-1 min-w-0">
                                <input
                                    ref={avatarInput}
                                    type="file"
                                    accept="image/*"
                                    hidden
                                    onChange={(e) => onAvatarPick(e.target.files?.[0])}
                                />
                                <div className="flex gap-2 flex-wrap">
                                    <Button
                                        size="sm"
                                        disabled={avatarBusy}
                                        onClick={() => avatarInput.current?.click()}
                                    >
                                        {avatarBusy ? '处理中…' : '上传头像'}
                                    </Button>
                                    {profile.hasAvatar && (
                                        <Button
                                            size="sm"
                                            variant="ghost-danger"
                                            disabled={avatarBusy}
                                            onClick={removeAvatar}
                                        >
                                            移除
                                        </Button>
                                    )}
                                </div>
                                <p className="text-xs text-ink-soft mt-1.5">
                                    会裁成正方形缩到 256px。不上传就用用户名首字母。
                                    头像存在配置目录,不会出现在网盘和 WebDAV 里
                                </p>
                            </div>
                        </div>
                        <div className="flex flex-col gap-2.5 mt-3">
                            <Input
                                placeholder="用户名(2-32 字符)"
                                value={username}
                                onChange={(e) => setUsername(e.target.value)}
                            />
                        </div>

                        <div className="border-t border-line/70 mt-4 pt-4">
                            <div className="font-bold text-sm mb-2.5">
                                修改密码
                                <span className="text-xs text-ink-soft font-normal ml-1.5">
                                    (不改密码就留空)
                                </span>
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
                            </div>
                        </div>

                        <div className="flex flex-col gap-1.5 mt-4">
                            <Button variant="primary" disabled={saving} onClick={save}>
                                {saving ? '保存中…' : '保存'}
                            </Button>
                            <p className="text-xs text-ink-soft">
                                改用户名/密码后,WebDAV 的登录信息同步变化,手机端记得更新
                            </p>
                        </div>
                    </Card>

                    <Card>
                        <CardTitle>最近修改</CardTitle>
                        {recent.length === 0 ? (
                            <p className="text-sm text-ink-soft">暂无最近修改的文件</p>
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
                                        className="flex items-center gap-2 text-sm hover:bg-paper-2 rounded-lg px-2 py-1 -mx-2"
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

                {/* 右列:存储与组件 */}
                <div className="flex flex-col gap-3">
                    <Card>
                        <CardTitle>仓库容量</CardTitle>
                        {disk ? (
                            <>
                                <div className="text-xs font-bold mb-1">本机存储</div>
                                <Progress percent={disk.usedPercent} />
                                <p className="text-sm text-ink-soft mt-1.5">
                                    已用 {formatBytes(disk.used)} / 共 {formatBytes(disk.total)}
                                    ,剩余 {formatBytes(disk.free)}
                                </p>
                                <div className="text-xs font-bold mb-1 mt-3">网盘目录</div>
                                {local === null ? (
                                    <p className="text-sm text-ink-soft">读取中…</p>
                                ) : local.pending ? (
                                    <p className="text-sm text-ink-soft">用量统计中…</p>
                                ) : local.quota > 0 ? (
                                    <>
                                        <Progress percent={(local.bytes / local.quota) * 100} />
                                        <p className="text-sm text-ink-soft mt-1.5">
                                            已用 {formatBytes(local.bytes)} / 上限{' '}
                                            {formatBytes(local.quota)}
                                            {local.files > 0 && `,${local.files} 个文件`}
                                            {local.bytes > local.quota && (
                                                <span className="text-danger"> · 已超出</span>
                                            )}
                                        </p>
                                    </>
                                ) : (
                                    <p className="text-sm text-ink-soft">
                                        已用 {formatBytes(local.bytes)}
                                        {local.files > 0 && `,${local.files} 个文件`}
                                        <span className="text-xs">(未设上限)</span>
                                    </p>
                                )}
                            </>
                        ) : (
                            <p className="text-sm text-ink-soft">读取中…</p>
                        )}

                        {/* 挂载的外部存储各自一行;用量是后台统计的缓存值 */}
                        {mounts.map((m) => (
                            <div key={m.name} className="mt-3 pt-3 border-t border-line/70">
                                <div className="text-xs font-bold mb-1">@{m.name}</div>
                                {m.pending ? (
                                    <p className="text-sm text-ink-soft">统计中…</p>
                                ) : m.quota > 0 ? (
                                    <>
                                        <Progress percent={(m.bytes / m.quota) * 100} />
                                        <p className="text-sm text-ink-soft mt-1.5">
                                            已用 {formatBytes(m.bytes)} / 上限{' '}
                                            {formatBytes(m.quota)}
                                            {m.files > 0 && `,${m.files} 个文件`}
                                        </p>
                                    </>
                                ) : (
                                    <p className="text-sm text-ink-soft">
                                        已用 {formatBytes(m.bytes)}
                                        {m.files > 0 && `,${m.files} 个文件`}
                                        <span className="text-xs">(未设上限)</span>
                                    </p>
                                )}
                            </div>
                        ))}
                    </Card>

                    <Link to="/storage" className="block">
                        <Card className="hover:border-leaf/50 transition-colors">
                            <div className="flex items-center gap-3">
                                <div className="flex-1 min-w-0">
                                    <div className="font-bold text-[15px]">存储策略</div>
                                    <div className="text-xs text-ink-soft mt-0.5">
                                        挂载 Cloudflare R2 / S3 兼容对象存储,显示为 @名称
                                        文件夹,网页与 WebDAV 通用
                                    </div>
                                </div>
                                <ChevronRight className="size-4 text-ink-soft shrink-0" />
                            </div>
                        </Card>
                    </Link>

                    <Card>
                        <CardTitle>WebDAV</CardTitle>
                        <p className="text-sm text-ink-soft mb-2">
                            手机播放器/文件管理器里添加 WebDAV 服务,即可直连整个网盘
                            (含 @外部存储挂载):
                        </p>
                        {kv('地址', <code className="bg-paper-2 rounded px-2 py-0.5 break-all">{davURL}</code>)}
                        {kv('用户名', <code className="bg-paper-2 rounded px-2 py-0.5">{profile.user}</code>)}
                        {kv('密码', <span className="text-sm">与网页登录密码相同</span>)}
                        {davDirect !== null && (
                            <div className="border-t border-line mt-2.5 pt-2.5">
                                <Checkbox
                                    label="外部存储直连(不经本站中转)"
                                    checked={davDirect}
                                    onChange={(e) => toggleDavDirect(e.target.checked)}
                                />
                                <p className="text-xs text-ink-soft mt-1.5">
                                    开启后,播放 @挂载 里的文件由播放器直接连存储桶,
                                    不占本机流量(网页端一直是这样)。极少数客户端不认
                                    重定向(如 Windows 资源管理器),播不了就关掉。
                                </p>
                            </div>
                        )}
                    </Card>

                    <Card>
                        <CardTitle>备份与迁移</CardTitle>
                        <p className="text-sm text-ink-soft mb-2.5">
                            导出会把网盘文件和配置库(分享链接、下载历史、文件夹图标、
                            存储策略)打成一个 tar.gz。换 VPS 时在新机器上导入即可。
                            外部存储(@挂载)的内容不打包,导入后按原策略自动挂上。
                        </p>
                        <div className="flex gap-2 flex-wrap">
                            <a href={api.exportUrl()} download>
                                <Button size="sm">导出整盘备份</Button>
                            </a>
                            <input
                                ref={importInput}
                                type="file"
                                accept=".gz,.tgz"
                                hidden
                                onChange={(e) => {
                                    const f = e.target.files?.[0];
                                    if (f) setImportFile(f);
                                    e.target.value = '';
                                }}
                            />
                            <Button size="sm" onClick={() => importInput.current?.click()}>
                                从备份导入…
                            </Button>
                        </div>
                        <p className="text-xs text-ink-soft mt-2">
                            备份包里含存储策略的密钥,请妥善保管
                        </p>
                    </Card>

                    <Card>
                        <CardTitle>组件状态</CardTitle>
                        {comps === null ? (
                            <p className="text-sm text-ink-soft">读取中…</p>
                        ) : (
                            <div className="flex flex-col gap-2.5">
                                {comps.map((c) => (
                                    <div
                                        key={c.kind}
                                        className="flex items-center gap-2 flex-wrap"
                                    >
                                        <div className="flex-1 min-w-0">
                                            <div className="text-sm font-bold flex items-center gap-1.5 flex-wrap">
                                                {c.title}
                                                {/* aria2 在另一个容器里,连不上和没装是两回事 */}
                                                {!c.running ? (
                                                    <Badge tone="red">
                                                        {c.kind === 'aria2'
                                                            ? '未连接'
                                                            : '未运行'}
                                                    </Badge>
                                                ) : !c.installed ? (
                                                    <Badge tone="red">未安装</Badge>
                                                ) : c.outdated ? (
                                                    <Badge tone="orange">
                                                        有新版 {c.latest}
                                                    </Badge>
                                                ) : (
                                                    <Badge tone="green">{c.version}</Badge>
                                                )}
                                            </div>
                                            <div className="text-xs text-ink-soft">
                                                {c.note}
                                                {c.installed && c.outdated && (
                                                    <> · 当前 {c.version}</>
                                                )}
                                                {c.lastUpdated && (
                                                    <> · 上次更新 {formatTime(c.lastUpdated)}</>
                                                )}
                                            </div>
                                        </div>
                                        {c.channel === 'managed' ? (
                                            <Button
                                                size="sm"
                                                variant={
                                                    c.outdated || !c.installed
                                                        ? 'primary'
                                                        : 'default'
                                                }
                                                disabled={installing !== ''}
                                                onClick={() => installComponent(c.kind, c.title)}
                                            >
                                                {installing === c.kind
                                                    ? '下载中…'
                                                    : !c.installed
                                                      ? '安装'
                                                      : c.outdated
                                                        ? '升级'
                                                        : '重新下载'}
                                            </Button>
                                        ) : (
                                            <span className="text-xs text-ink-soft whitespace-nowrap">
                                                {c.updateHint}
                                            </span>
                                        )}
                                    </div>
                                ))}

                                {/* 「以后要更新了怎么办」必须写在界面上,别让人到时候去翻文档 */}
                                {comps.some((c) => c.channel === 'managed') ? (
                                    <div className="border-t border-line pt-2.5 flex flex-col gap-2">
                                        <p className="text-xs text-ink-soft leading-relaxed">
                                            yt-dlp 装在 config 卷里,在这儿点一下就能升级,重启容器也不会退回旧版
                                            —— 它更新最频繁,视频站点一改规则就得跟。
                                            aria2 和 ffmpeg 跟着容器镜像走,在服务器上执行这条命令即可,
                                            网盘文件和配置都不受影响:
                                        </p>
                                        <div className="flex items-center gap-2">
                                            <code className="flex-1 min-w-0 bg-paper-2 rounded-lg px-2.5 py-1.5 text-xs break-all">
                                                {upgradeCmd(composeDir)}
                                            </code>
                                            <Button
                                                size="sm"
                                                variant="ghost"
                                                onClick={copyUpgradeCmd}
                                            >
                                                复制
                                            </Button>
                                        </div>
                                        {/* compose 目录因部署方式而异,容器里探测不到,让用户自己指一次 */}
                                        <div className="flex items-center gap-2 flex-wrap">
                                            <span className="text-xs text-ink-soft shrink-0">
                                                compose 目录
                                            </span>
                                            <Input
                                                className="flex-1 min-w-40"
                                                value={composeDir}
                                                placeholder={DEFAULT_COMPOSE_DIR}
                                                onChange={(e) => saveComposeDir(e.target.value)}
                                            />
                                            <Button
                                                size="sm"
                                                variant="ghost"
                                                onClick={() => saveComposeDir(DEFAULT_COMPOSE_DIR)}
                                            >
                                                一键脚本
                                            </Button>
                                            <Button
                                                size="sm"
                                                variant="ghost"
                                                onClick={() => saveComposeDir(ONEPANEL_COMPOSE_DIR)}
                                            >
                                                1Panel
                                            </Button>
                                        </div>
                                        <p className="text-xs text-ink-soft leading-relaxed">
                                            一键脚本装的在 <code>/opt/pocketdrive</code>;1Panel
                                            编排装的在 <code>/opt/1panel/docker/compose/编排名</code>
                                            (用 1Panel 的话,直接在「容器 → 编排」里点「拉取镜像并重建」
                                            更省事,不用敲命令)。找不到就跑一句{' '}
                                            <code>docker inspect pocketdrive --format '{'{{'}
                                            index .Config.Labels "com.docker.compose.project.working_dir"{'}}'}'</code>
                                        </p>
                                    </div>
                                ) : (
                                    <p className="text-xs text-ink-soft leading-relaxed">
                                        当前以本机开发方式运行,三个组件的版本都取决于你系统 PATH
                                        里的安装
                                    </p>
                                )}
                            </div>
                        )}
                    </Card>

                    <ErrorLogCard />
                </div>
            </div>

            {/* 导入确认:会覆盖现有网盘,所以要验密码 */}
            <Dialog open={importFile !== null} onOpenChange={(o) => !o && setImportFile(null)}>
                <DialogContent title="从备份导入">
                    <p className="text-sm mb-3">
                        将从「{importFile?.name}」恢复。
                        <b>同名文件会被覆盖</b>
                        ,配置库会在重启后整个替换掉现在的。建议先导出一份当前备份。
                    </p>
                    <Input
                        type="password"
                        placeholder="输入当前登录密码以确认"
                        value={importPass}
                        autoComplete="current-password"
                        onChange={(e) => setImportPass(e.target.value)}
                    />
                    <DialogFooter
                        okText={importing ? '导入中…' : '确认导入'}
                        okDanger
                        okLoading={importing}
                        onOk={doImport}
                    />
                </DialogContent>
            </Dialog>
        </div>
    );
}
