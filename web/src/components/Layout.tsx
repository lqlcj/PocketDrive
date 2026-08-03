import { useState } from 'react';
import { NavLink, Outlet } from 'react-router-dom';
import { Menu, Moon, Sun, X } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { Profile } from '../api';
import { Button } from './ui/button';
import SearchBox from './SearchBox';
import { applyTheme, getTheme } from '../theme';
import type { Theme } from '../theme';
import { cn } from '../lib/utils';

const NAV: Array<{ section?: string; to: string; label: string; icon: string; end?: boolean }> = [
    { to: '/', label: '主页', icon: '🏝️', end: true },
    { to: '/files', label: '我的文件', icon: '📁' },
    { to: '/downloads', label: '离线下载', icon: '⬇️' },
    { to: '/video', label: 'yt下载', icon: '🎬' },
    { to: '/shares', label: '分享管理', icon: '🔗' },
    { to: '/trash', label: '垃圾桶', icon: '🗑️' },
    { to: '/settings', label: '设置', icon: '⚙️' },
];

function NavList({ onNavigate }: { onNavigate?: () => void }) {
    return (
        <nav className="flex flex-col gap-0.5">
            {NAV.map((n) => (
                <div key={n.to}>
                    {n.section && (
                        <div className="px-3 pt-3 pb-1 text-xs font-bold text-ink-soft">
                            {n.section}
                        </div>
                    )}
                    <NavLink
                        to={n.to}
                        end={n.end}
                        onClick={onNavigate}
                        className={({ isActive }) =>
                            cn(
                                'flex items-center gap-2.5 px-3.5 py-2 rounded-full font-bold text-sm transition-colors',
                                isActive
                                    ? 'bg-leaf text-white'
                                    : 'text-ink hover:bg-paper-2',
                            )
                        }
                    >
                        <span className="text-base">{n.icon}</span>
                        {n.label}
                    </NavLink>
                </div>
            ))}
        </nav>
    );
}

export default function Layout({
    profile,
    onLogout,
}: {
    profile: Profile;
    onLogout: () => void;
}) {
    const [mobileOpen, setMobileOpen] = useState(false);
    const [theme, setTheme] = useState<Theme>(getTheme());

    const toggleTheme = () => {
        const next: Theme = theme === 'light' ? 'dark' : 'light';
        setTheme(next);
        applyTheme(next);
    };

    const logout = async () => {
        try {
            await api.logout();
        } catch {
            // 即使请求失败也回到登录页
        }
        toast.success('已退出登录');
        onLogout();
    };

    const sidebarInner = (
        <>
            <div className="px-3 text-xl font-extrabold text-leaf-dark flex items-center gap-2">
                🏝️ PocketDrive
            </div>
            <div className="flex-1 overflow-y-auto mt-3 -mx-1 px-1">
                <NavList onNavigate={() => setMobileOpen(false)} />
            </div>
            <div className="pt-3 border-t-2 border-dashed border-line flex items-center gap-2 px-2">
                <span className="text-xl">{profile.avatar}</span>
                <span className="flex-1 text-sm font-bold truncate">{profile.user}</span>
                <Button variant="ghost" size="sm" onClick={logout}>
                    退出
                </Button>
            </div>
        </>
    );

    return (
        <div className="min-h-screen flex">
            {/* 桌面侧栏 */}
            <aside className="hidden md:flex flex-col w-56 shrink-0 sticky top-0 h-screen bg-paper border-r-2 border-dashed border-line p-4">
                {sidebarInner}
            </aside>

            {/* 移动端抽屉 */}
            {mobileOpen && (
                <div className="fixed inset-0 z-50 md:hidden">
                    <button
                        aria-label="关闭菜单"
                        className="absolute inset-0 bg-black/40"
                        onClick={() => setMobileOpen(false)}
                    />
                    <aside className="absolute left-0 top-0 bottom-0 w-64 bg-paper p-4 flex flex-col shadow-2xl">
                        <button
                            className="absolute right-3 top-3 text-ink-soft"
                            aria-label="关闭"
                            onClick={() => setMobileOpen(false)}
                        >
                            <X className="size-5" />
                        </button>
                        {sidebarInner}
                    </aside>
                </div>
            )}

            <div className="flex-1 min-w-0 flex flex-col">
                {/* 顶栏:搜索 + 主题切换 */}
                <header className="sticky top-0 z-30 bg-paper/90 backdrop-blur border-b-2 border-dashed border-line">
                    <div className="max-w-5xl mx-auto flex items-center gap-3 px-4 py-2.5">
                        <button
                            className="md:hidden text-ink"
                            aria-label="打开菜单"
                            onClick={() => setMobileOpen(true)}
                        >
                            <Menu className="size-5" />
                        </button>
                        <span className="md:hidden font-extrabold text-leaf-dark">
                            🏝️
                        </span>
                        <div className="flex-1 flex justify-center">
                            <SearchBox />
                        </div>
                        <Button
                            variant="ghost"
                            size="icon"
                            aria-label="切换主题"
                            onClick={toggleTheme}
                        >
                            {theme === 'light' ? (
                                <Moon className="size-4" />
                            ) : (
                                <Sun className="size-4" />
                            )}
                        </Button>
                    </div>
                </header>

                {/* 版心居中 */}
                <main className="flex-1 w-full max-w-5xl mx-auto px-4 py-6 pb-16">
                    <Outlet />
                </main>
            </div>
        </div>
    );
}
