import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { Download, Leaf, Play, TreePalm } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { ShareInfo } from '../api';
import { Card } from '../components/ui/card';
import { Input } from '../components/ui/input';
import { Button } from '../components/ui/button';
import KindIcon from '../components/KindIcon';
import { browserPlayable, fileKind, formatBytes, formatTime } from '../util';

export default function SharePage() {
    const { token = '' } = useParams();
    const [info, setInfo] = useState<ShareInfo | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [password, setPassword] = useState('');
    const [unlocked, setUnlocked] = useState(false);
    const [playing, setPlaying] = useState(false);

    useEffect(() => {
        api.shareInfo(token)
            .then((r) => {
                setInfo(r);
                if (!r.needPassword) setUnlocked(true);
            })
            .catch((e) => setError(e instanceof Error ? e.message : '加载失败'));
    }, [token]);

    const unlock = async () => {
        try {
            const resp = await fetch(api.shareDownloadUrl(token, password), {
                headers: { Range: 'bytes=0-0' },
            });
            if (resp.ok || resp.status === 206) {
                setUnlocked(true);
            } else {
                const body = await resp.json().catch(() => null);
                toast.error(body?.error ?? '提取密码不正确');
            }
        } catch {
            toast.error('网络错误');
        }
    };

    if (error) {
        return (
            <div className="min-h-screen flex items-center justify-center px-4">
                <Card className="w-full max-w-sm text-center py-8">
                    <Leaf className="size-9 mx-auto text-ink-soft" />
                    <p className="mt-2 text-sm">{error}</p>
                </Card>
            </div>
        );
    }
    if (!info) {
        return (
            <div className="min-h-screen flex items-center justify-center text-ink-soft">
                加载中…
            </div>
        );
    }

    const kind = fileKind(info.name);
    const url = api.shareDownloadUrl(token, password);
    const thumb = api.shareThumbUrl(token, password);

    return (
        <div className="min-h-screen flex items-center justify-center px-4 py-8">
            <Card className="w-full max-w-lg p-6">
                <div className="flex items-center gap-3">
                    <KindIcon kind={kind} className="size-9" />
                    <div className="min-w-0">
                        <div className="font-extrabold text-lg break-all">{info.name}</div>
                        <div className="text-xs text-ink-soft">
                            {formatBytes(info.size)} · {formatTime(info.mtime)}
                            {info.expiresAt && ` · ${formatTime(info.expiresAt)} 过期`}
                        </div>
                    </div>
                </div>

                {!unlocked ? (
                    <div className="flex flex-col gap-2.5 mt-5">
                        <Input
                            type="password"
                            placeholder="请输入提取密码"
                            value={password}
                            onChange={(e) => setPassword(e.target.value)}
                            onKeyDown={(e) => e.key === 'Enter' && unlock()}
                        />
                        <Button variant="primary" onClick={unlock}>
                            解锁
                        </Button>
                    </div>
                ) : (
                    <div className="mt-5">
                        {kind === 'image' && (
                            <img
                                src={url}
                                alt={info.name}
                                className="max-w-full max-h-[55vh] rounded-xl mx-auto"
                            />
                        )}
                        {kind === 'audio' && (
                            // eslint-disable-next-line jsx-a11y/media-has-caption
                            <audio src={url} controls className="w-full" />
                        )}
                        {kind === 'video' &&
                            browserPlayable(info.name) &&
                            (playing ? (
                                // eslint-disable-next-line jsx-a11y/media-has-caption
                                <video
                                    src={url}
                                    controls
                                    autoPlay
                                    className="w-full max-h-[55vh] rounded-xl bg-black"
                                />
                            ) : (
                                <button
                                    className="relative w-full rounded-xl overflow-hidden bg-paper-2 cursor-pointer"
                                    onClick={() => setPlaying(true)}
                                >
                                    <img
                                        src={thumb}
                                        alt=""
                                        className="w-full max-h-[55vh] object-contain"
                                        onError={(e) => {
                                            (e.target as HTMLImageElement).style.display =
                                                'none';
                                        }}
                                    />
                                    <span className="absolute inset-0 flex items-center justify-center">
                                        <span className="flex items-center justify-center size-16 rounded-full bg-black/50 text-white">
                                            <Play className="size-8 fill-current" />
                                        </span>
                                    </span>
                                </button>
                            ))}
                        <a
                            href={api.shareDownloadUrl(token, password, true)}
                            download
                            className="block mt-4"
                        >
                            <Button variant="primary" className="w-full">
                                <Download className="size-4" /> 下载文件
                            </Button>
                        </a>
                    </div>
                )}
                <p className="text-center text-xs text-ink-soft mt-5 inline-flex items-center gap-1 justify-center w-full">
                    <TreePalm className="size-3.5" /> 由 PocketDrive 分享
                </p>
            </Card>
        </div>
    );
}
