import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { Copy, Download, Eye, FileText, Github, Leaf, Play } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { ShareInfo } from '../api';
import { Card } from '../components/ui/card';
import { Input } from '../components/ui/input';
import { Button } from '../components/ui/button';
import KindIcon from '../components/KindIcon';
import { videoPlaybackMode, copyText, fileKind, formatBytes, formatTime } from '../util';
import VideoPreview from '../components/VideoPreview';
import Preview from '../components/Preview';
import { PlayerProvider } from '../player/store';

export default function SharePage() {
    const { token = '' } = useParams();
    const [info, setInfo] = useState<ShareInfo | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [password, setPassword] = useState('');
    const [unlocked, setUnlocked] = useState(false);
    const [unlocking, setUnlocking] = useState(false);
    const [playing, setPlaying] = useState(false);
    const [previewOpen, setPreviewOpen] = useState(false);

    useEffect(() => {
        setInfo(null);
        setError(null);
        setPassword('');
        setUnlocked(false);
        setPlaying(false);
        setPreviewOpen(false);
        api.shareInfo(token)
            .then((result) => {
                setInfo(result);
                if (!result.needPassword) setUnlocked(true);
            })
            .catch((e) => setError(e instanceof Error ? e.message : '加载失败'));
    }, [token]);

    const unlock = async () => {
        setUnlocking(true);
        try {
            await api.shareUnlock(token, password);
            // 文本正文只会在通过密码验证后返回，因此解锁后重新取一次分享信息。
            const result = await api.shareInfo(token);
            setInfo(result);
            setUnlocked(true);
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '解锁失败');
        } finally {
            setUnlocking(false);
        }
    };

    const copySharedText = async () => {
        if (!info?.content) return;
        if (await copyText(info.content)) toast.success('文本已复制');
        else toast.warning('复制失败，请手动选中文本复制');
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

    const isText = info.type === 'text';
    const kind = fileKind(info.name);
    const url = api.shareDownloadUrl(token);
    const thumb = api.shareThumbUrl(token);

    return (
        <div className="min-h-screen flex items-center justify-center px-4 py-8">
            <Card className={`w-full ${isText ? 'max-w-2xl' : 'max-w-lg'} p-6`}>
                <div className="flex items-center gap-3">
                    {isText ? (
                        <FileText className="size-9 text-leaf-dark shrink-0" />
                    ) : (
                        <KindIcon kind={kind} className="size-9" />
                    )}
                    <div className="min-w-0">
                        <div className="font-extrabold text-lg break-all">{info.name}</div>
                        <div className="text-xs text-ink-soft">
                            {isText
                                ? `${info.size} 个字符 · ${formatTime(info.mtime)} 创建`
                                : `${formatBytes(info.size)} · ${formatTime(info.mtime)}`}
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
                            onKeyDown={(e) => e.key === 'Enter' && !unlocking && unlock()}
                        />
                        <Button variant="primary" disabled={unlocking} onClick={unlock}>
                            {unlocking ? '解锁中…' : '解锁'}
                        </Button>
                    </div>
                ) : isText ? (
                    <div className="mt-5">
                        <div className="rounded-xl border border-line/70 bg-paper-2 p-4">
                            <pre className="whitespace-pre-wrap break-words font-sans text-sm leading-7 text-ink">
                                {info.content}
                            </pre>
                        </div>
                        <Button
                            variant="primary"
                            className="w-full mt-4"
                            disabled={info.content === undefined}
                            onClick={copySharedText}
                        >
                            <Copy className="size-4" /> 复制文本
                        </Button>
                    </div>
                ) : (
                    <div className="mt-5">
                        {kind === 'image' && (
                            <button type="button" className="block w-full cursor-zoom-in" aria-label="查看图片" onClick={() => setPreviewOpen(true)}>
                            <img
                                src={url}
                                alt={info.name}
                                className="max-w-full max-h-[55vh] rounded-xl mx-auto"
                            />
                            </button>
                        )}
                        {['pdf', 'markdown', 'text', 'sheet', 'epub'].includes(kind) || info.name.toLowerCase().endsWith('.docx') ? (
                            <Button className="w-full" onClick={() => setPreviewOpen(true)}><Eye className="size-4" /> 预览文件</Button>
                        ) : null}
                        {kind === 'audio' && (
                            // eslint-disable-next-line jsx-a11y/media-has-caption
                            <audio src={url} controls className="w-full" />
                        )}
                        {kind === 'video' &&
                            videoPlaybackMode(info.name) &&
                            (playing ? (
                                <VideoPreview url={url} name={info.name} downloadUrl={api.shareDownloadUrl(token, true)} />
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
                            href={api.shareDownloadUrl(token, true)}
                            download
                            className="block mt-4"
                        >
                            <Button variant="primary" className="w-full">
                                <Download className="size-4" /> 下载文件
                            </Button>
                        </a>
                    </div>
                )}
                <a
                    href="https://github.com/lqlcj/PocketDrive"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-center text-xs text-ink-soft mt-5 inline-flex items-center gap-1 justify-center w-full hover:text-leaf-dark transition-colors"
                >
                    <Github className="size-3.5" /> 由 PocketDrive 分享
                </a>
            </Card>
            {unlocked && !isText && previewOpen && (
                <PlayerProvider>
                    <Preview
                        key={token}
                        entries={[{ name: info.name, size: info.size, mtime: info.mtime, dir: false }]}
                        index={0}
                        dirPath=""
                        source={{ url, downloadUrl: api.shareDownloadUrl(token, true) }}
                        onNavigate={() => {}}
                        onClose={() => setPreviewOpen(false)}
                    />
                </PlayerProvider>
            )}
        </div>
    );
}
