import { useState } from 'react';
import { NavLink, Outlet, useLocation } from 'react-router-dom';
import {
    CloudDownload,
    FolderOpen,
    Github,
    Link2,
    LogOut,
    Menu,
    Moon,
    HardDrive,
    Settings,
    Sun,
    TreePalm,
    Trash2,
    X,
    Youtube,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { Profile } from '../api';
import { Button } from './ui/button';
import SearchBox from './SearchBox';
import Avatar from './Avatar';
import ErrorBoundary from './ErrorBoundary';
import { applyTheme, getTheme } from '../theme';
import type { Theme } from '../theme';
import { cn } from '../lib/utils';

const NAV: Array<{ to: string; label: string; icon: LucideIcon; end?: boolean }> = [
    { to: '/files', label: '我的文件', icon: FolderOpen },
    { to: '/downloads', label: '离线下载', icon: CloudDownload, end: true },
    { to: '/video', label: 'yt下载', icon: Youtube },
    { to: '/shares', label: '分享管理', icon: Link2 },
    { to: '/trash', label: '垃圾桶', icon: Trash2 },
    { to: '/storage-settings', label: '储存策略', icon: HardDrive },
    { to: '/settings', label: '设置', icon: Settings },
];

function NavList({ onNavigate }: { onNavigate?: () => void }) {
    const location = useLocation();

    return (
        <nav className="flex flex-col gap-0.5">
            {NAV.map((n) => (
                <NavLink
                    key={n.to}
                    to={n.to}
                    end={n.end}
                    onClick={onNavigate}
                    className={({ isActive }) =>
                        cn(
                            'flex items-center gap-2.5 px-3.5 py-2 rounded-full font-bold text-sm transition-colors',
                            isActive || (n.to === '/storage-settings' && location.pathname === '/storage')
                                ? 'bg-leaf text-white'
                                : 'text-ink hover:bg-paper-2',
                        )
                    }
                >
                    <n.icon className="size-4" />
                    {n.label}
                </NavLink>
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
    const location = useLocation();

    const toggleTheme = () => {
        const next: Theme = theme === 'light' ? 'dark' : 'light';
        setTheme(next);
        applyTheme(next, true);
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
                <TreePalm className="size-5" /> PocketDrive
            </div>
            <div className="flex-1 overflow-y-auto mt-4 -mx-1 px-1">
                <NavList onNavigate={() => setMobileOpen(false)} />
            </div>
            <a
                href="https://github.com/lqlcj/PocketDrive"
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-2.5 rounded-full px-3.5 py-2 text-sm font-bold text-ink transition-colors hover:bg-paper-2"
            >
                <Github className="size-4" />
                <span>PocketDrive</span>
            </a>
            <div className="mt-2 pt-3 border-t border-line/70 flex items-center gap-2 px-2">
                <Avatar profile={profile} size="sm" />
                <span className="flex-1 text-sm font-bold truncate">{profile.user}</span>
                <Button variant="ghost" size="sm" onClick={logout} aria-label="退出登录">
                    <LogOut className="size-3.5" /> 退出
                </Button>
            </div>
        </>
    );

    return (
        <div className="min-h-screen flex">
            {/* 桌面侧栏 */}
            <aside className="hidden md:flex flex-col w-56 shrink-0 sticky top-0 h-screen bg-paper border-r border-line/70 p-4">
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
                <header className="sticky top-0 z-30 bg-paper/90 backdrop-blur border-b border-line/70">
                    <div className="max-w-5xl mx-auto flex items-center gap-3 px-4 py-2.5">
                        <button
                            className="md:hidden text-ink"
                            aria-label="打开菜单"
                            onClick={() => setMobileOpen(true)}
                        >
                            <Menu className="size-5" />
                        </button>
                        <TreePalm className="md:hidden size-5 text-leaf-dark" />
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
                    {/* 单个页面崩了不牵连侧栏和搜索;换个路由自动复位 */}
                    <ErrorBoundary resetKey={location.pathname}>
                        <Outlet />
                    </ErrorBoundary>
                </main>
            </div>
        </div>
    );
}
