import { useState } from 'react';
import type { FormEvent } from 'react';
import { toast } from 'sonner';
import { api } from '../api';
import type { Profile } from '../api';
import { Card } from '../components/ui/card';
import { Input } from '../components/ui/input';
import { Button } from '../components/ui/button';

export default function Login({ onLogin }: { onLogin: (p: Profile) => void }) {
    const [username, setUsername] = useState('');
    const [password, setPassword] = useState('');
    const [loading, setLoading] = useState(false);

    const submit = async (e: FormEvent) => {
        e.preventDefault();
        if (!username || !password) return;
        setLoading(true);
        try {
            const r = await api.login(username, password);
            toast.success('欢迎回岛!');
            onLogin({ user: r.user, avatar: r.avatar });
        } catch (err) {
            toast.error(err instanceof Error ? err.message : '登录失败');
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="min-h-screen flex items-center justify-center px-4">
            <Card className="w-full max-w-sm p-6">
                <form onSubmit={submit} className="flex flex-col gap-3.5">
                    <div className="text-center mb-1">
                        <div className="text-4xl">🏝️</div>
                        <h1 className="text-2xl font-extrabold mt-1">PocketDrive</h1>
                        <p className="text-sm text-ink-soft">你的口袋小岛网盘</p>
                    </div>
                    <Input
                        placeholder="用户名"
                        value={username}
                        autoComplete="username"
                        onChange={(e) => setUsername(e.target.value)}
                    />
                    <Input
                        placeholder="密码"
                        type="password"
                        value={password}
                        autoComplete="current-password"
                        onChange={(e) => setPassword(e.target.value)}
                    />
                    <Button variant="primary" type="submit" disabled={loading}>
                        {loading ? '登岛中…' : '登录'}
                    </Button>
                </form>
            </Card>
        </div>
    );
}
