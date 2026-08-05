import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { ChevronRight } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { Profile, RecentFile } from '../api';
import { Card, CardTitle } from '../components/ui/card';
import { Checkbox } from '../components/ui/input';
import KindIcon from '../components/KindIcon';
import { fileKind, formatTime } from '../util';

export default function StorageSettings({ profile }: { profile: Profile }) {
    const [recent, setRecent] = useState<RecentFile[]>([]);
    const [davDirect, setDavDirect] = useState<boolean | null>(null);

    useEffect(() => {
        api.storage()
            .then((r) => setRecent(r.recent ?? []))
            .catch(() => undefined);
        api.cloudSettings()
            .then((r) => setDavDirect(r.settings.davDirect))
            .catch(() => undefined);
    }, []);

    const toggleDavDirect = async (on: boolean) => {
        const previous = davDirect;
        setDavDirect(on);
        try {
            const r = await api.saveCloudSettings({ davDirect: on });
            setDavDirect(r.settings.davDirect);
            toast.success(on ? '已开启:播放器将直连存储桶' : '已关闭:改由本站中转');
        } catch (e) {
            setDavDirect(previous);
            toast.error(e instanceof Error ? e.message : '保存失败');
        }
    };

    const davURL = `${window.location.origin}/dav/`;

    return (
        <div>
            <h2 className="text-xl font-extrabold mb-4">储存策略</h2>

            <div className="grid md:grid-cols-2 gap-3 items-start">
                <Card>
                    <CardTitle>最近修改</CardTitle>
                    {recent.length === 0 ? (
                        <p className="text-sm text-ink-soft">暂无最近修改的文件</p>
                    ) : (
                        <div className="flex flex-col gap-1.5">
                            {recent.map((file) => (
                                <Link
                                    key={file.path}
                                    to={`/files/${
                                        file.path.includes('/')
                                            ? file.path.slice(0, file.path.lastIndexOf('/'))
                                            : ''
                                    }`}
                                    className="flex items-center gap-2 text-sm hover:bg-paper-2 rounded-lg px-2 py-1 -mx-2"
                                >
                                    <KindIcon kind={fileKind(file.name)} />
                                    <span className="flex-1 min-w-0 truncate font-bold">
                                        {file.name}
                                    </span>
                                    <span className="text-xs text-ink-soft shrink-0">
                                        {formatTime(file.mtime).slice(5)}
                                    </span>
                                </Link>
                            ))}
                        </div>
                    )}
                </Card>

                <div className="flex flex-col gap-3">
                    <Link to="/storage" className="block">
                        <Card className="hover:border-leaf/50 transition-colors">
                            <div className="flex items-center gap-3">
                                <div className="flex-1 min-w-0">
                                    <div className="font-bold text-[15px]">存储策略</div>
                                    <div className="text-xs text-ink-soft mt-0.5">
                                        管理 Cloudflare R2 / S3 兼容对象存储挂载
                                    </div>
                                </div>
                                <ChevronRight className="size-4 text-ink-soft shrink-0" />
                            </div>
                        </Card>
                    </Link>

                    <Card>
                        <CardTitle>WebDAV</CardTitle>
                        <div className="flex flex-col gap-2 text-sm">
                            <div className="flex gap-2 flex-wrap">
                                <span className="text-ink-soft w-16">地址</span>
                                <code className="bg-paper-2 rounded px-2 py-0.5 break-all">
                                    {davURL}
                                </code>
                            </div>
                            <div className="flex gap-2 flex-wrap">
                                <span className="text-ink-soft w-16">用户名</span>
                                <code className="bg-paper-2 rounded px-2 py-0.5">
                                    {profile.user}
                                </code>
                            </div>
                            <div className="flex gap-2 flex-wrap">
                                <span className="text-ink-soft w-16">密码</span>
                                <span>与网页登录密码相同</span>
                            </div>
                        </div>
                        {davDirect !== null && (
                            <div className="border-t border-line mt-3 pt-3">
                                <Checkbox
                                    label="外部存储直连(不经本站中转)"
                                    checked={davDirect}
                                    onChange={(e) => toggleDavDirect(e.target.checked)}
                                />
                                <p className="text-xs text-ink-soft mt-1.5">
                                    开启后,播放外部挂载文件不占本机流量;客户端不支持重定向时请关闭。
                                </p>
                            </div>
                        )}
                    </Card>
                </div>
            </div>
        </div>
    );
}
