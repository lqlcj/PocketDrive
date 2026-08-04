import { useState } from 'react';
import type { FormEvent } from 'react';
import { Eye, EyeOff, TreePalm } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { Profile } from '../api';
import { Button } from '../components/ui/button';
import AnimatedCharacters from '../components/AnimatedCharacters';
import { cn } from '../lib/utils';

/**
 * 登录页:左边是会盯着你鼠标看的小家伙们(输密码时会集体扭头回避)。
 * 安全上沿用既有后端约束:JWT httpOnly cookie、CSRF Origin 校验、
 * 同 IP 连错 5 次封 5 分钟;前端不落任何密码痕迹(不存 localStorage、
 * autocomplete 走浏览器密码管理器)。
 */
export default function Login({ onLogin }: { onLogin: (p: Profile) => void }) {
    const [username, setUsername] = useState('');
    const [password, setPassword] = useState('');
    const [showPassword, setShowPassword] = useState(false);
    const [isTyping, setIsTyping] = useState(false);
    const [isPasswordFocused, setIsPasswordFocused] = useState(false);
    const [loading, setLoading] = useState(false);
    const [errorMsg, setErrorMsg] = useState('');

    const submit = async (e: FormEvent) => {
        e.preventDefault();
        const user = username.trim();
        if (!user || !password) {
            setErrorMsg('用户名和密码都要填哦');
            return;
        }
        setLoading(true);
        setErrorMsg('');
        try {
            const r = await api.login(user, password);
            toast.success('欢迎回岛!');
            onLogin({ user: r.user, avatar: r.avatar });
        } catch (err) {
            setErrorMsg(err instanceof Error ? err.message : '登录失败');
        } finally {
            setLoading(false);
        }
    };

    const inputCls =
        'w-full h-12 bg-transparent border-0 border-b-[1.5px] border-line text-[15px] text-ink placeholder:text-ink-soft/50 outline-none transition-colors focus:border-leaf';

    return (
        <div className="min-h-screen grid lg:grid-cols-2">
            {/* 左半屏:动画小家伙 */}
            <div className="relative hidden lg:flex flex-col justify-between overflow-hidden p-10 bg-gradient-to-br from-[#d9cfbe] via-[#cec3af] to-[#c2b6a0] dark:from-[#38352f] dark:via-[#302d28] dark:to-[#282521]">
                <div className="relative z-10 flex items-center gap-2.5 font-extrabold text-ink">
                    <span className="flex items-center justify-center size-8 rounded-lg bg-leaf text-white">
                        <TreePalm className="size-5" />
                    </span>
                    PocketDrive
                </div>
                <div className="relative z-10 flex items-end justify-center h-[420px]">
                    <AnimatedCharacters
                        isTyping={isTyping}
                        isPasswordFocused={isPasswordFocused}
                        showPassword={showPassword}
                        passwordLength={password.length}
                    />
                </div>
                <div className="relative z-10 text-[13px] text-ink-soft">
                    你的口袋小岛网盘 · 输密码时他们会把头扭过去的,放心
                </div>
                {/* 氛围光斑 */}
                <div className="absolute top-[18%] right-[12%] size-64 rounded-full bg-leaf/15 blur-[80px]" />
                <div className="absolute bottom-[12%] left-[8%] size-80 rounded-full bg-[#e4b15e]/20 blur-[100px]" />
            </div>

            {/* 右半屏:表单 */}
            <div className="flex items-center justify-center bg-paper px-6 py-10">
                <div className="w-full max-w-sm">
                    <div className="text-center mb-9">
                        <div className="lg:hidden mb-4 flex justify-center">
                            <span className="flex items-center justify-center size-12 rounded-2xl bg-leaf text-white">
                                <TreePalm className="size-7" />
                            </span>
                        </div>
                        <h1 className="text-[26px] font-extrabold tracking-tight">
                            欢迎回岛!
                        </h1>
                        <p className="text-sm text-ink-soft mt-1.5">登录你的 PocketDrive</p>
                    </div>

                    <form onSubmit={submit}>
                        <div className="mb-5">
                            <label
                                htmlFor="pd-user"
                                className="block text-[13px] font-bold text-ink-soft mb-1"
                            >
                                用户名
                            </label>
                            <input
                                id="pd-user"
                                className={inputCls}
                                value={username}
                                autoComplete="username"
                                placeholder="admin"
                                onChange={(e) => {
                                    setUsername(e.target.value);
                                    if (errorMsg) setErrorMsg('');
                                }}
                                onFocus={() => setIsTyping(true)}
                                onBlur={() => setIsTyping(false)}
                            />
                        </div>

                        <div className="mb-6">
                            <label
                                htmlFor="pd-pass"
                                className="block text-[13px] font-bold text-ink-soft mb-1"
                            >
                                密码
                            </label>
                            <div className="relative">
                                <input
                                    id="pd-pass"
                                    type={showPassword ? 'text' : 'password'}
                                    className={cn(inputCls, 'pr-10')}
                                    value={password}
                                    autoComplete="current-password"
                                    placeholder="********"
                                    onChange={(e) => {
                                        setPassword(e.target.value);
                                        if (errorMsg) setErrorMsg('');
                                    }}
                                    onFocus={() => setIsPasswordFocused(true)}
                                    onBlur={() => setIsPasswordFocused(false)}
                                />
                                <button
                                    type="button"
                                    className="absolute right-0 top-1/2 -translate-y-1/2 p-1.5 text-ink-soft hover:text-ink cursor-pointer"
                                    aria-label={showPassword ? '隐藏密码' : '显示密码'}
                                    onClick={() => setShowPassword((v) => !v)}
                                >
                                    {showPassword ? (
                                        <EyeOff className="size-5" />
                                    ) : (
                                        <Eye className="size-5" />
                                    )}
                                </button>
                            </div>
                        </div>

                        {errorMsg && (
                            <div className="mb-4 rounded-xl border border-danger/25 bg-danger-soft px-3.5 py-2.5 text-[13px] text-danger">
                                {errorMsg}
                            </div>
                        )}

                        <Button
                            variant="primary"
                            type="submit"
                            disabled={loading}
                            className="w-full h-12 text-[15px]"
                        >
                            {loading ? '登岛中…' : '登录'}
                        </Button>
                    </form>

                    <p className="text-center text-xs text-ink-soft mt-8">
                        连续输错 5 次会被暂时封禁 5 分钟
                    </p>
                </div>
            </div>
        </div>
    );
}
