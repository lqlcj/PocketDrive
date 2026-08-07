import { useRef, useState } from 'react';
import { toast } from 'sonner';
import { api } from '../api';
import type { Profile } from '../api';
import { Card, CardTitle } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import Avatar from '../components/Avatar';
import StorageSettingsCards from '../components/StorageSettingsCards';

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

    return (
        <div>
            <h2 className="text-xl font-extrabold mb-4">设置</h2>

            {/* 手机单列、桌面两列；四张卡片按行等高对齐。 */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3 items-stretch">
                    <Card className="h-full p-3 flex flex-col">
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

                        <div className="mt-auto pt-2.5 flex justify-end">
                            <Button size="sm" variant="primary" disabled={saving} onClick={save}>
                                {saving ? '保存中…' : '保存'}
                            </Button>
                        </div>
                    </Card>

                    <StorageSettingsCards profile={profile} />
            </div>
        </div>
    );
}
