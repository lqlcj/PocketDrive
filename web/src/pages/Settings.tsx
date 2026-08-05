import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { api } from '../api';
import type { Profile } from '../api';
import { Card, CardTitle } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Dialog, DialogContent, DialogFooter } from '../components/ui/dialog';
import Avatar from '../components/Avatar';
import { formatBytes } from '../util';

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

    const [oldPass, setOldPass] = useState('');
    const [newPass, setNewPass] = useState('');
    const [confirm, setConfirm] = useState('');
    const [saving, setSaving] = useState(false);

    // 头像:上传图片存在配置目录,不进网盘
    const avatarInput = useRef<HTMLInputElement>(null);
    const [avatarBusy, setAvatarBusy] = useState(false);

    // 导入会覆盖现有网盘,要二次确认并验密码
    const importInput = useRef<HTMLInputElement>(null);
    const [importFile, setImportFile] = useState<File | null>(null);
    const [importPass, setImportPass] = useState('');
    const [importing, setImporting] = useState(false);

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

                    <ErrorLogCard />
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
