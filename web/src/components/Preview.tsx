import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { FileText, Music, NotebookPen, Pencil, Play, Save, X } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { FileEntry } from '../api';
import { fileKind, formatBytes, officePreviewable } from '../util';
import { usePlayer } from '../player/store';
import { Dialog, DialogContent } from './ui/dialog';
import { Button } from './ui/button';
import OfficePreview from './OfficePreview';
import SheetPreview from './SheetPreview';
import ImagePreview from './ImagePreview';
import VideoPreview from './VideoPreview';
import EpubPreview from './EpubPreview';
import { textDraft, textToSave } from '../lib/plaintext';
import { invalidateList, fetchList } from '../lib/listcache';

interface Props {
    entries: FileEntry[];
    index: number;
    dirPath: string;
    onNavigate: (idx: number) => void;
    onClose: () => void;
    source?: { url: string; downloadUrl: string };
}

export default function Preview({ entries, index, dirPath, onNavigate, onClose, source }: Props) {
    const navigate = useNavigate();
    const { playList } = usePlayer();
    const entry = entries[index]!;
    const path = dirPath === '' ? entry.name : `${dirPath}/${entry.name}`;
    const kind = fileKind(entry.name);
    const url = source?.url ?? api.downloadUrl(path);
    const downloadUrl = source?.downloadUrl ?? api.downloadUrl(path, true);
    const sharedUrl = source?.url;

    const [text, setText] = useState<string | null>(null);
    const [textErr, setTextErr] = useState<string | null>(null);
    const [editing, setEditing] = useState(false);
    const [draft, setDraft] = useState('');
    const [saving, setSaving] = useState(false);
    const dirty = editing && text !== null && draft !== textDraft(text);

    useEffect(() => {
        if (!dirty && !saving) return;
        const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
        window.addEventListener('beforeunload', warn);
        return () => window.removeEventListener('beforeunload', warn);
    }, [dirty, saving]);

    useEffect(() => {
        let cancelled = false;
        const controller = new AbortController();
        setText(null);
        setTextErr(null);
        setEditing(false);
        if (kind === 'markdown' || kind === 'text') {
            const read = sharedUrl ? (async () => {
                const response = await fetch(sharedUrl, { signal: controller.signal });
                if (!response.ok) throw new Error(`读取失败 (${response.status})`);
                const reader = response.body?.getReader();
                if (!reader) throw new Error('无法读取文件');
                const chunks: Uint8Array[] = [];
                let size = 0;
                try {
                    while (true) {
                        const { done, value } = await reader.read();
                        if (done) break;
                        size += value.byteLength;
                        if (size > 2 * 1024 * 1024) throw new Error('文本超过 2 MiB，请下载后查看');
                        chunks.push(value);
                    }
                } finally {
                    await reader.cancel();
                }
                const bytes = new Uint8Array(size);
                let offset = 0;
                for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length; }
                return new TextDecoder().decode(bytes);
            })() : api.content(path, kind === 'text');
            read.then((value) => { if (!cancelled) setText(value); })
                .catch((e) => { if (!cancelled) setTextErr(e instanceof Error ? e.message : '读取失败'); });
        }
        return () => { cancelled = true; controller.abort(); };
    }, [path, kind, sharedUrl]);

    const discard = () => !dirty || window.confirm('修改尚未保存，确定放弃吗？');
    const close = () => { if (!saving && discard()) onClose(); };
    const saveText = async () => {
        if (saving || text === null || source || !editing) return;
        const content = textToSave(text, draft);
        if (new TextEncoder().encode(content).byteLength > 2 * 1024 * 1024) {
            toast.error('文本超过 2 MiB，无法在此编辑器保存');
            return;
        }
        setSaving(true);
        try {
            await api.writeFile(path, content);
            setText(content);
            setEditing(false);
            invalidateList(dirPath);
            void fetchList(dirPath, true).catch(() => {});
            toast.success('已保存');
        } catch (error) {
            toast.error(error instanceof Error ? error.message : '保存失败');
        } finally {
            setSaving(false);
        }
    };

    if (kind === 'image') {
        return <ImagePreview key={sharedUrl ?? dirPath} entries={entries} index={index} dirPath={dirPath} onNavigate={onNavigate} onClose={onClose} source={source} />;
    }

    const downloadHint = (msg: string) => (
        <p className="text-center py-6 text-sm">
            {msg},可{' '}
            <a className="text-leaf-dark underline" href={downloadUrl} download>
                下载
            </a>{' '}
            到本地打开。
        </p>
    );

    let body;
    switch (kind) {
        case 'video':
            body = <VideoPreview key={url} url={url} name={entry.name} downloadUrl={downloadUrl} />;
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
            body = (
                <div className="pd-markdown-preview pd-note-preview-scroll">
                    <article className="pd-note-content">
                        {text === null ? (
                            <p className="text-sm text-ink-soft">{textErr ?? '加载中…'}</p>
                        ) : (
                            <ReactMarkdown remarkPlugins={[remarkGfm]}>{text}</ReactMarkdown>
                        )}
                    </article>
                </div>
            );
            break;
        case 'text':
            body =
                text === null ? (
                    <p className="text-sm text-ink-soft">{textErr ?? '加载中…'}</p>
                ) : editing ? (
                    <textarea
                        aria-label={`编辑 ${entry.name}`}
                        className="pd-plain-text-editor"
                        autoFocus
                        spellCheck={false}
                        autoCapitalize="off"
                        autoCorrect="off"
                        value={draft}
                        disabled={saving}
                        onChange={(event) => setDraft(event.target.value)}
                        onKeyDown={(event) => {
                            if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
                                event.preventDefault();
                                void saveText();
                            }
                            if (event.key === 'Tab' && !event.shiftKey) {
                                event.preventDefault();
                                const input = event.currentTarget;
                                const start = input.selectionStart;
                                const end = input.selectionEnd;
                                setDraft(draft.slice(0, start) + '\t' + draft.slice(end));
                                requestAnimationFrame(() => input.setSelectionRange(start + 1, start + 1));
                            }
                        }}
                    />
                ) : (
                    <pre className="m-0 text-sm leading-relaxed whitespace-pre-wrap [overflow-wrap:anywhere]">
                        {text}
                    </pre>
                );
            break;
        case 'pdf':
            body = (
                <iframe
                    src={url}
                    title={entry.name}
                    className="block w-full h-full min-h-0 border-0 bg-white"
                />
            );
            break;
        case 'sheet':
            body = <SheetPreview key={url} url={url} name={entry.name} />;
            break;
        case 'epub':
            body = <EpubPreview key={url} url={url} downloadUrl={downloadUrl} name={entry.name} />;
            break;
        case 'doc':
        case 'slide':
            body = officePreviewable(entry.name) ? (
                <OfficePreview url={url} name={entry.name} />
            ) : (
                downloadHint('为避免在浏览器中解析不可信 Office 文件，此格式不提供在线预览')
            );
            break;
        default:
            body = downloadHint(`此类型暂不支持预览(${formatBytes(entry.size)})`);
    }

    return (
        <Dialog open onOpenChange={(o) => !o && close()}>
            <DialogContent
                title={(
                    <span className="flex items-center gap-2 text-lg">
                        {kind === 'markdown' ? (
                            <NotebookPen className="size-4.5 text-leaf-dark shrink-0" />
                        ) : (
                            <FileText className="size-4.5 text-leaf-dark shrink-0" />
                        )}
                        <span className="truncate">{entry.name}</span>
                    </span>
                )}
                headerActions={kind === 'markdown' && text !== null && !source ? (
                    <Button size="sm" onClick={() => navigate(`/note/${path}`)}>
                        <Pencil className="size-3.5" /> 编辑
                    </Button>
                ) : kind === 'text' && text !== null && !source ? (
                    editing ? <>
                        <Button size="icon" title="取消编辑" aria-label="取消编辑" disabled={saving} onClick={() => { if (discard()) setEditing(false); }}><X className="size-4" /></Button>
                        <Button size="icon" title={saving ? '保存中' : '保存'} aria-label={saving ? '保存中' : '保存'} disabled={saving} onClick={() => void saveText()}><Save className="size-4" /></Button>
                    </> : <Button size="sm" onClick={() => { setDraft(textDraft(text)); setEditing(true); }}><Pencil className="size-3.5" /> 编辑</Button>
                ) : undefined}
                className="pd-note-preview-dialog"
                wide
            >
                {kind === 'markdown' ? body : (
                    <div className={`pd-note-preview-scroll${kind === 'epub' || (kind === 'text' && editing) ? ' pd-plain-text-scroll' : kind === 'video' ? ' pd-video-preview-scroll' : kind === 'pdf' ? ' pd-pdf-preview-scroll' : ' pd-file-preview-content'}`}>
                        {body}
                    </div>
                )}
            </DialogContent>
        </Dialog>
    );
}
