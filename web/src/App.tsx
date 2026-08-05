import { useCallback, useEffect, useState } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { Loader2 } from 'lucide-react';
import { api } from './api';
import type { Profile } from './api';
import Layout from './components/Layout';
import Login from './pages/Login';
import Files from './pages/Files';
import NoteEditor from './pages/NoteEditor';
import Downloads from './pages/Downloads';
import DownloadSettings from './pages/DownloadSettings';
import VideoDL from './pages/VideoDL';
import SharesPage from './pages/SharesPage';
import Trash from './pages/Trash';
import Settings from './pages/Settings';
import StoragePage from './pages/StoragePage';
import SharePage from './pages/SharePage';
import UploadPanel from './components/UploadPanel';
import MusicPlayer from './components/MusicPlayer';
import { UploadProvider } from './upload/store';
import { PlayerProvider } from './player/store';

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
                        {/* 我的文件即主页 */}
                        <Route path="/" element={<Navigate to="/files" replace />} />
                        <Route path="/files/*" element={<Files />} />
                        <Route path="/note/*" element={<NoteEditor />} />
                        <Route path="/downloads" element={<Downloads />} />
                        <Route path="/downloads/settings" element={<DownloadSettings />} />
                        <Route path="/video" element={<VideoDL />} />
                        <Route path="/shares" element={<SharesPage />} />
                        <Route path="/trash" element={<Trash />} />
                        <Route path="/storage" element={<StoragePage />} />
                        <Route
                            path="/settings"
                            element={<Settings profile={profile} onProfile={onProfile} />}
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
                <Route path="/s/:token" element={<SharePage />} />
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
