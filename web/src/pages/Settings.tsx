import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { api } from '../api';
import type {
    ComponentInfo,
    Profile,
} from '../api';
import { Card, CardTitle } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Dialog, DialogContent, DialogFooter } from '../components/ui/dialog';
import { Badge } from '../components/ui/progress';
import Avatar from '../components/Avatar';
import { formatBytes, formatTime, copyText } from '../util';

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

    const [comps, setComps] = useState<ComponentInfo[] | null>(null);
    const [installing, setInstalling] = useState('');
    const [updateEnabled, setUpdateEnabled] = useState<boolean | null>(null);
    const [updateOpen, setUpdateOpen] = useState(false);
    const [updatePassword, setUpdatePassword] = useState('');
    const [updating, setUpdating] = useState(false);

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

    useEffect(() => {
        loadComponents();
        api.updateStatus().then((r) => setUpdateEnabled(r.enabled)).catch(() => setUpdateEnabled(false));
    }, [loadComponents]);

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

    const triggerUpdate = async () => {
        if (!updatePassword) return;
        setUpdating(true);
        try {
            await api.triggerUpdate(updatePassword);
            setUpdateOpen(false);
            setUpdatePassword('');
            toast.success('升级已开始,服务会短暂重启;请稍后刷新页面', { duration: 8000 });
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '升级失败');
        } finally {
            setUpdating(false);
        }
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

    return (
        <div>
            <h2 className="text-xl font-extrabold mb-4">设置</h2>

            {/* 两两成对:同一行的两张卡片等高,底部对齐、间距一致 */}
            <div className="grid md:grid-cols-2 gap-3">
                    <Card className="p-3">
                        <CardTitle className="mb-2">个人资料</CardTitle>
                        <div className="flex items-center gap-2.5">
                            <Avatar profile={profile} size="md" />
                            <div className="flex-1 min-w-0">
                                <input
                                    ref={avatarInput}
                                    type="file"
                                    accept="image/*"
                                    hidden
                                    onChange={(e) => onAvatarPick(e.target.files?.[0])}
                                />
                                <div className="flex gap-1.5 flex-wrap">
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
                            </div>
                        </div>
                        <div className="mt-2.5">
                            <Input
                                placeholder="用户名(2-32 字符)"
                                value={username}
                                onChange={(e) => setUsername(e.target.value)}
                            />
                        </div>

                        <details className="border-t border-line/70 mt-2.5 pt-2.5">
                            <summary className="font-bold text-sm cursor-pointer select-none">
                                修改密码
                            </summary>
                            <div className="flex flex-col gap-2 mt-2.5">
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
                        </details>

                        <div className="mt-2.5 flex justify-end">
                            <Button size="sm" variant="primary" disabled={saving} onClick={save}>
                                {saving ? '保存中…' : '保存'}
                            </Button>
                        </div>
                    </Card>

                    <Card>
                        <CardTitle>备份与迁移</CardTitle>
                        <p className="text-sm text-ink-soft mb-2.5">
                            导出会把网盘文件和配置库(分享链接、下载历史、文件夹图标、
                            存储策略)打成一个 tar.gz。换 VPS 时在新机器上导入即可。
                            外部存储(@挂载)的内容不打包,导入后按原策略自动挂上。
                        </p>
                        <div className="flex gap-2 flex-wrap">
                            <a href={api.exportUrl()} download><Button size="sm">导出整盘备份</Button></a>
                            <input ref={importInput} type="file" accept=".gz,.tgz" hidden onChange={(e) => { const f = e.target.files?.[0]; if (f) setImportFile(f); e.target.value = ''; }} />
                            <Button size="sm" onClick={() => importInput.current?.click()}>从备份导入…</Button>
                        </div>
                        <p className="text-xs text-ink-soft mt-2">备份包里含存储策略的密钥,请妥善保管</p>
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
                                                {c.lastUpdated && <> · {formatTime(c.lastUpdated)}</>}
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
                                                    : '升级'}
                                            </Button>
                                        ) : (
                                            <span className="text-xs text-ink-soft whitespace-nowrap">
                                                {c.updateHint}
                                            </span>
                                        )}
                                    </div>
                                ))}

                                {comps.some((c) => c.channel === 'managed') ? (
                                    <details className="border-t border-line pt-2.5 text-xs text-ink-soft">
                                        <summary className="cursor-pointer font-bold text-ink">
                                            升级说明
                                        </summary>
                                        <div className="mt-2.5 flex flex-col gap-2.5">
                                            <p>yt-dlp 可直接点上面的“升级”。aria2 和 ffmpeg 随容器镜像升级。</p>
                                            <div className="flex items-center gap-2">
                                                <span className="shrink-0">服务器命令</span>
                                                <code className="flex-1 min-w-0 bg-paper-2 rounded-lg px-2.5 py-1.5 break-all">
                                                    {upgradeCmd(composeDir)}
                                                </code>
                                                <Button size="sm" variant="ghost" onClick={copyUpgradeCmd}>复制</Button>
                                            </div>
                                            <div className="flex items-center gap-2 flex-wrap">
                                                <span className="shrink-0">目录</span>
                                                <Input
                                                    className="flex-1 min-w-40"
                                                    value={composeDir}
                                                    placeholder={DEFAULT_COMPOSE_DIR}
                                                    onChange={(e) => saveComposeDir(e.target.value)}
                                                />
                                                <Button size="sm" variant="ghost" onClick={() => saveComposeDir(DEFAULT_COMPOSE_DIR)}>一键脚本</Button>
                                                <Button size="sm" variant="ghost" onClick={() => saveComposeDir(ONEPANEL_COMPOSE_DIR)}>1Panel</Button>
                                            </div>
                                            <p>
                                                一键脚本目录是 <code>/opt/pocketdrive</code>。1Panel 请直接在“容器 → 编排”中点击“拉取镜像并重建”,或把上面的目录改成实际编排目录。
                                            </p>
                                        </div>
                                    </details>
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

                    {/* 5 张卡片里更新占整行,避免出现孤立的半个空位 */}
                    <Card className="md:col-span-2">
                        <CardTitle>PocketDrive 更新</CardTitle>
                        <p className="text-sm text-ink-soft mb-2.5">从官方镜像拉取最新版本并重建容器。网盘文件、配置和密码保存在持久化卷中,不会被更新删除。</p>
                        <div className="flex items-center gap-2 flex-wrap">
                            <Button size="sm" variant="primary" disabled={updateEnabled !== true || updating} onClick={() => setUpdateOpen(true)}>{updating ? '升级中…' : '检查并升级'}</Button>
                            {updateEnabled === false && <span className="text-xs text-ink-soft">当前编排未启用安全更新服务,请在 1Panel 中拉取镜像并重建</span>}
                            {updateEnabled === true && <span className="text-xs text-ink-soft">升级前需要再次输入当前密码确认</span>}
                        </div>
                    </Card>
            </div>

            <Dialog open={updateOpen} onOpenChange={(o) => { if (!o && !updating) { setUpdateOpen(false); setUpdatePassword(''); } }}>
                <DialogContent title="确认升级">
                    <p className="text-sm mb-3">将拉取最新官方镜像并重启 PocketDrive。正在进行的请求会短暂中断,数据不会删除。</p>
                    <Input type="password" placeholder="输入当前登录密码" autoComplete="current-password" value={updatePassword} onChange={(e) => setUpdatePassword(e.target.value)} />
                    <DialogFooter okText={updating ? '升级中…' : '确认升级'} okLoading={updating} onOk={triggerUpdate} />
                </DialogContent>
            </Dialog>

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
