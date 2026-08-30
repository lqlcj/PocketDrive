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
import { cn } from './lib/utils';
import {
    importDownloads,
    importDownloadSettings,
    importFiles,
    importLogin,
    importNoteEditor,
    importSettings,
    importSharePage,
    importShares,
    importStorage,
    importTrash,
} from './routes';

// importer 都放在 routes.ts,侧栏悬停预取和这里的 lazy() 共用同一个
const Login = lazy(importLogin);
const Files = lazy(importFiles);
const NoteEditor = lazy(importNoteEditor);
const Downloads = lazy(importDownloads);
const DownloadSettings = lazy(importDownloadSettings);
const SharesPage = lazy(importShares);
const Trash = lazy(importTrash);
const Settings = lazy(importSettings);
const StoragePage = lazy(importStorage);
const SharePage = lazy(importSharePage);

/**
 * 懒加载兜底。
 *
 * 版心里的兜底(full=false)高度必须比主区矮:主区本来就被 min-h-screen
 * 撑到一屏,再塞个一屏高的转圈进去,页面总高就超过视口、逼出滚动条,
 * chunk 一到内容塌回去滚动条又撤掉——换一次页白抖两下。
 * 整页路由(登录页、分享页)不在版心里,占满一屏才不显得吊在半空。
 */
function SuspenseWrap({ children, full = false }: { children: React.ReactNode; full?: boolean }) {
    return (
        <Suspense
            fallback={
                <div
                    className={cn(
                        'flex items-center justify-center gap-2 text-ink-soft',
                        full ? 'min-h-screen' : 'min-h-[50vh]',
                    )}
                >
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
        // 登录页是懒加载的,得有自己的 Suspense 边界:它在 Layout 外面,
        // 上面再没有别的边界能接住它挂起
        return (
            <SuspenseWrap full>
                <Login onLogin={onProfile} />
            </SuspenseWrap>
        );
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
                            element={<Navigate to="/storage" replace />}
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
                <Route path="/s/:token" element={<SuspenseWrap full><SharePage /></SuspenseWrap>} />
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
