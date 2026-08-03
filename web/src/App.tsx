import { useCallback, useEffect, useState } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { api } from './api';
import type { Profile } from './api';
import Layout from './components/Layout';
import Login from './pages/Login';
import Home from './pages/Home';
import Files from './pages/Files';
import Notes from './pages/Notes';
import NoteEditor from './pages/NoteEditor';
import Gallery from './pages/Gallery';
import Music from './pages/Music';
import Cinema from './pages/Cinema';
import Downloads from './pages/Downloads';
import VideoDL from './pages/VideoDL';
import Trash from './pages/Trash';
import Settings from './pages/Settings';
import SharePage from './pages/SharePage';

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
            <div className="min-h-screen flex items-center justify-center text-lg">
                🏝️ 加载中…
            </div>
        );
    }
    if (profile === null) {
        return <Login onLogin={onProfile} />;
    }
    return (
        <Routes>
            <Route element={<Layout profile={profile} onLogout={onLogout} />}>
                <Route path="/" element={<Home />} />
                <Route path="/files/*" element={<Files />} />
                <Route path="/notes" element={<Notes />} />
                <Route path="/note/*" element={<NoteEditor />} />
                <Route path="/gallery" element={<Gallery />} />
                <Route path="/music" element={<Music />} />
                <Route path="/cinema" element={<Cinema />} />
                <Route path="/downloads" element={<Downloads />} />
                <Route path="/video" element={<VideoDL />} />
                <Route path="/trash" element={<Trash />} />
                <Route
                    path="/settings"
                    element={<Settings profile={profile} onProfile={onProfile} />}
                />
                <Route path="*" element={<Navigate to="/" replace />} />
            </Route>
        </Routes>
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
