import { lazy, Suspense, useCallback, useEffect, useState } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { Loader2 } from 'lucide-react';
import { api } from './api';
import type { Profile } from './api';
import Layout from './components/Layout';
import UploadPanel from './components/UploadPanel';
import MusicPlayer from './components/MusicPlayer';
import { UploadProvider } from './upload/store';
import { PlayerProvider } from './player/store';

const Login = lazy(() => import('./pages/Login'));
const Files = lazy(() => import('./pages/Files'));
const NoteEditor = lazy(() => import('./pages/NoteEditor'));
const Downloads = lazy(() => import('./pages/Downloads'));
const DownloadSettings = lazy(() => import('./pages/DownloadSettings'));
const SharesPage = lazy(() => import('./pages/SharesPage'));
const Trash = lazy(() => import('./pages/Trash'));
const Settings = lazy(() => import('./pages/Settings'));
const StoragePage = lazy(() => import('./pages/StoragePage'));
const StorageSettings = lazy(() => import('./pages/StorageSettings'));
const SharePage = lazy(() => import('./pages/SharePage'));

function SuspenseWrap({ children }: { children: React.ReactNode }) {
    return (
        <Suspense
            fallback={
                <div className="min-h-screen flex items-center justify-center gap-2 text-ink-soft">
                    <Loader2 className="size-5 animate-spin" /> 加载中…
                </div>
            }
        >
            {children}
        </Suspense>
    );
}

function Private({
    profile,
    onProfile,
    onLogout,
}: {
    profile: Profile | null | undefined;
    onProfile: (p: Profile) => void;
    onLogout: () => void;
}) {
    if (profile === undefined) {
        return (
            <div className="min-h-screen flex items-center justify-center gap-2 text-ink-soft">
                <Loader2 className="size-5 animate-spin" /> 加载中…
            </div>
        );
    }
    if (profile === null) {
        return <Login onLogin={onProfile} />;
    }
    return (
        <UploadProvider>
            <PlayerProvider>
                <Routes>
                    <Route element={<Layout profile={profile} onLogout={onLogout} />}>
                        <Route path="/" element={<Navigate to="/files" replace />} />
                        <Route path="/files/*" element={<SuspenseWrap><Files /></SuspenseWrap>} />
                        <Route path="/note/*" element={<SuspenseWrap><NoteEditor /></SuspenseWrap>} />
                        <Route path="/downloads" element={<SuspenseWrap><Downloads /></SuspenseWrap>} />
                        <Route path="/downloads/settings" element={<SuspenseWrap><DownloadSettings /></SuspenseWrap>} />
                        <Route path="/shares" element={<SuspenseWrap><SharesPage /></SuspenseWrap>} />
                        <Route path="/trash" element={<SuspenseWrap><Trash /></SuspenseWrap>} />
                        <Route path="/storage" element={<SuspenseWrap><StoragePage /></SuspenseWrap>} />
                        <Route
                            path="/storage-settings"
                            element={<SuspenseWrap><StorageSettings profile={profile} /></SuspenseWrap>}
                        />
                        <Route
                            path="/settings"
                            element={<SuspenseWrap><Settings profile={profile} onProfile={onProfile} /></SuspenseWrap>}
                        />
                        <Route path="*" element={<Navigate to="/files" replace />} />
                    </Route>
                </Routes>
                {/*
                  右下角的面板栈,挂在路由外:切页面既不打断上传,也不打断音乐。
                  音乐在上、上传在下,两个都可能不存在(各自返回 null),
                  栈会自己塌下去,不用算偏移。
                */}
                <div className="fixed bottom-3 right-3 z-40 flex flex-col items-end gap-2">
                    <MusicPlayer />
                    <UploadPanel />
                </div>
            </PlayerProvider>
        </UploadProvider>
    );
}

export default function App() {
    // undefined = 会话状态未知(加载中);null = 未登录
    const [profile, setProfile] = useState<Profile | null | undefined>(undefined);

    const refresh = useCallback(() => {
        api.me()
            .then(setProfile)
            .catch(() => setProfile(null));
    }, []);

    useEffect(() => {
        refresh();
        const onUnauth = () => setProfile(null);
        window.addEventListener('pocketdrive:unauth', onUnauth);
        return () => window.removeEventListener('pocketdrive:unauth', onUnauth);
    }, [refresh]);

    return (
        <BrowserRouter>
            <Routes>
                {/* 公开分享页:免登录 */}
                <Route path="/s/:token" element={<SuspenseWrap><SharePage /></SuspenseWrap>} />
                <Route
                    path="*"
                    element={
                        <Private
                            profile={profile}
                            onProfile={setProfile}
                            onLogout={() => setProfile(null)}
                        />
                    }
                />
            </Routes>
        </BrowserRouter>
    );
}
