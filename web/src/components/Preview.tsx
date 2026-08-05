import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Music, Pencil, Play } from 'lucide-react';
import { api } from '../api';
import type { FileEntry } from '../api';
import { browserPlayable, fileKind, formatBytes, officePreviewable } from '../util';
import { usePlayer } from '../player/store';
import { Dialog, DialogContent } from './ui/dialog';
import { Button } from './ui/button';
import OfficePreview from './OfficePreview';

interface Props {
    entries: FileEntry[];
    index: number;
    dirPath: string;
    onNavigate: (idx: number) => void;
    onClose: () => void;
}

export default function Preview({ entries, index, dirPath, onNavigate, onClose }: Props) {
    const navigate = useNavigate();
    const { playList } = usePlayer();
    const entry = entries[index]!;
    const path = dirPath === '' ? entry.name : `${dirPath}/${entry.name}`;
    const kind = fileKind(entry.name);
    const url = api.downloadUrl(path);

    const [text, setText] = useState<string | null>(null);
    const [textErr, setTextErr] = useState<string | null>(null);

    useEffect(() => {
        setText(null);
        setTextErr(null);
        if (kind === 'markdown' || kind === 'text') {
            api.content(path)
                .then(setText)
                .catch((e) => setTextErr(e instanceof Error ? e.message : '读取失败'));
        }
    }, [path, kind]);

    const imageIdxs = entries
        .map((e, i) => ({ e, i }))
        .filter(({ e }) => !e.dir && fileKind(e.name) === 'image')
        .map(({ i }) => i);
    const imgPos = imageIdxs.indexOf(index);

    const downloadHint = (msg: string) => (
        <p className="text-center py-6 text-sm">
            {msg},可{' '}
            <a className="text-leaf-dark underline" href={api.downloadUrl(path, true)} download>
                下载
            </a>{' '}
            到本地打开。
        </p>
    );

    let body;
    switch (kind) {
        case 'image':
            body = (
                <div className="text-center">
                    <img
                        src={url}
                        alt={entry.name}
                        className="max-w-full max-h-[65vh] rounded-xl inline-block"
                    />
                    {imageIdxs.length > 1 && (
                        <div className="flex items-center justify-center gap-3 mt-3 text-ink-soft text-sm">
                            <Button
                                size="sm"
                                disabled={imgPos <= 0}
                                onClick={() => onNavigate(imageIdxs[imgPos - 1]!)}
                            >
                                ← 上一张
                            </Button>
                            <span>
                                {imgPos + 1} / {imageIdxs.length}
                            </span>
                            <Button
                                size="sm"
                                disabled={imgPos >= imageIdxs.length - 1}
                                onClick={() => onNavigate(imageIdxs[imgPos + 1]!)}
                            >
                                下一张 →
                            </Button>
                        </div>
                    )}
                </div>
            );
            break;
        case 'video':
            body = browserPlayable(entry.name) ? (
                // eslint-disable-next-line jsx-a11y/media-has-caption
                <video src={url} controls autoPlay className="w-full max-h-[65vh] rounded-xl bg-black" />
            ) : (
                <p className="text-sm">
                    此格式浏览器无法直接播放(仅支持 mp4/webm 等),请{' '}
                    <a
                        className="text-leaf-dark underline"
                        href={api.downloadUrl(path, true)}
                        download
                    >
                        下载
                    </a>{' '}
                    后本地观看。
                </p>
            );
            break;
        case 'audio':
            // 正常点开音乐是走不到这儿的(文件页直接交给全局播放器了),
            // 万一从别的入口进来,也别在弹框里放——关掉弹框歌就断了
            body = (
                <div className="text-center py-6">
                    <Music className="size-12 mx-auto mb-3 text-leaf-dark" />
                    <p className="text-sm text-ink-soft mb-4">
                        音乐交给右下角的播放器,逛到哪儿都不会断
                    </p>
                    <Button
                        variant="primary"
                        onClick={() => {
                            playList([{ path, name: entry.name }], 0);
                            onClose();
                        }}
                    >
                        <Play className="size-4" /> 播放
                    </Button>
                </div>
            );
            break;
        case 'markdown':
            body =
                text === null ? (
                    <p className="text-sm text-ink-soft">{textErr ?? '加载中…'}</p>
                ) : (
                    <div>
                        <div className="flex justify-end mb-2">
                            <Button size="sm" onClick={() => navigate(`/note/${path}`)}>
                                <Pencil className="size-3.5" /> 编辑
                            </Button>
                        </div>
                        <div className="prose-island max-h-[62vh] overflow-auto">
                            <ReactMarkdown remarkPlugins={[remarkGfm]}>{text}</ReactMarkdown>
                        </div>
                    </div>
                );
            break;
        case 'text':
            body =
                text === null ? (
                    <p className="text-sm text-ink-soft">{textErr ?? '加载中…'}</p>
                ) : (
                    <pre className="max-h-[65vh] overflow-auto bg-paper-2 rounded-xl p-3 text-xs whitespace-pre-wrap break-all">
                        {text}
                    </pre>
                );
            break;
        case 'pdf':
            body = (
                <iframe
                    src={url}
                    title={entry.name}
                    className="w-full h-[70vh] rounded-xl bg-white border border-line/70"
                />
            );
            break;
        case 'doc':
        case 'sheet':
        case 'slide':
            body = officePreviewable(entry.name) ? (
                <OfficePreview url={url} name={entry.name} />
            ) : (
                downloadHint('旧版二进制格式(.doc/.ppt)暂不支持在线预览')
            );
            break;
        default:
            body = downloadHint(`此类型暂不支持预览(${formatBytes(entry.size)})`);
    }

    return (
        <Dialog open onOpenChange={(o) => !o && onClose()}>
            <DialogContent title={entry.name} wide>
                {body}
            </DialogContent>
        </Dialog>
    );
}
