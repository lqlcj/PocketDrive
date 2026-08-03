import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { toast } from 'sonner';
import { api } from '../api';
import type { FileEntry, Share } from '../api';
import { fileKind, formatBytes, formatTime, KIND_ICON } from '../util';
import Preview from '../components/Preview';
import FolderPicker from '../components/FolderPicker';
import { Card } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Input, NativeSelect } from '../components/ui/input';
import { Dialog, DialogContent, DialogFooter } from '../components/ui/dialog';
import { cn } from '../lib/utils';

type ViewMode = 'list' | 'grid';

const DND_TYPE = 'application/x-pd-path';

function Breadcrumb({ path }: { path: string }) {
    const parts = path === '' ? [] : path.split('/');
    let acc = '';
    return (
        <div className="text-sm text-ink-soft break-all mb-3">
            <Link className="text-leaf-dark font-bold" to="/files">
                🏝️ 根目录
            </Link>
            {parts.map((p, i) => {
                acc += (i === 0 ? '' : '/') + p;
                return (
                    <span key={acc}>
                        {' / '}
                        <Link className="text-leaf-dark font-bold" to={`/files/${acc}`}>
                            {p}
                        </Link>
                    </span>
                );
            })}
        </div>
    );
}

function Thumb({ path, name, dir }: { path: string; name: string; dir: boolean }) {
    const kind = fileKind(name, dir);
    const [failed, setFailed] = useState(false);
    if (!dir && (kind === 'image' || kind === 'video') && !failed) {
        return (
            <img
                className="w-full h-28 object-cover bg-paper-2"
                src={api.thumbUrl(path)}
                alt={name}
                loading="lazy"
                onError={() => setFailed(true)}
            />
        );
    }
    return (
        <div className="w-full h-28 flex items-center justify-center text-4xl bg-paper-2">
            {KIND_ICON[kind]}
        </div>
    );
}

export default function Files() {
    const params = useParams();
    const path = params['*'] ?? '';
    const navigate = useNavigate();

    const [entries, setEntries] = useState<FileEntry[]>([]);
    const [loading, setLoading] = useState(true);
    const [uploading, setUploading] = useState(false);
    const fileInput = useRef<HTMLInputElement>(null);
    const [view, setView] = useState<ViewMode>(
        () => (localStorage.getItem('pd_view') as ViewMode) || 'list',
    );

    const [mkdirOpen, setMkdirOpen] = useState(false);
    const [mkdirName, setMkdirName] = useState('');
    const [noteOpen, setNoteOpen] = useState(false);
    const [noteName, setNoteName] = useState('');
    const [renameTarget, setRenameTarget] = useState<FileEntry | null>(null);
    const [renameName, setRenameName] = useState('');
    const [moveTarget, setMoveTarget] = useState<FileEntry | null>(null);
    const [deleteTarget, setDeleteTarget] = useState<FileEntry | null>(null);
    const [previewIdx, setPreviewIdx] = useState<number | null>(null);

    const [shareTarget, setShareTarget] = useState<FileEntry | null>(null);
    const [sharePass, setSharePass] = useState('');
    const [shareType, setShareType] = useState('page');
    const [shareExpire, setShareExpire] = useState('0');
    const [shareResult, setShareResult] = useState<Share | null>(null);

    const [dragOver, setDragOver] = useState(false);
    const [dropTarget, setDropTarget] = useState<string | null>(null);

    const load = useCallback(() => {
        setLoading(true);
        api.listFiles(path)
            .then((r) => setEntries(r.entries))
            .catch((e) => {
                toast.error(e instanceof Error ? e.message : '加载失败');
                setEntries([]);
            })
            .finally(() => setLoading(false));
    }, [path]);

    useEffect(load, [load]);

    const join = (name: string) => (path === '' ? name : `${path}/${name}`);

    const switchView = (v: ViewMode) => {
        setView(v);
        localStorage.setItem('pd_view', v);
    };

    const onUpload = async (files: FileList | null) => {
        if (!files || files.length === 0) return;
        setUploading(true);
        try {
            const r = await api.upload(path, files);
            toast.success(`已上传 ${r.saved.length} 个文件`);
            load();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '上传失败');
        } finally {
            setUploading(false);
            if (fileInput.current) fileInput.current.value = '';
        }
    };

    const run = async (fn: () => Promise<unknown>, close?: () => void) => {
        try {
            await fn();
            close?.();
            load();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '操作失败');
        }
    };

    const doMoveTo = (src: string, destDir: string) =>
        run(async () => {
            await api.move(src, destDir);
            toast.success('已移动');
        });

    const doShare = async () => {
        if (!shareTarget) return;
        try {
            const r = await api.createShare(
                join(shareTarget.name),
                sharePass,
                shareType,
                parseInt(shareExpire, 10),
            );
            setShareResult(r.share);
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '创建分享失败');
        }
    };

    const shareLink = (s: Share) =>
        `${window.location.origin}${s.type === 'direct' ? '/d/' : '/s/'}${s.token}`;

    const copyText = async (text: string) => {
        try {
            await navigator.clipboard.writeText(text);
            toast.success('已复制到剪贴板');
        } catch {
            toast.warning('复制失败,请手动复制');
        }
    };

    const openEntry = (e: FileEntry, idx: number) => {
        if (e.dir) navigate(`/files/${join(e.name)}`);
        else setPreviewIdx(idx);
    };

    const closeShare = () => {
        setShareTarget(null);
        setSharePass('');
        setShareType('page');
        setShareExpire('0');
        setShareResult(null);
    };

    const newNote = () => {
        const name = noteName.trim();
        if (!name) return;
        const full = join(name.endsWith('.md') ? name : `${name}.md`);
        run(
            async () => {
                await api.writeFile(full, `# ${name.replace(/\.md$/, '')}\n\n`);
                navigate(`/note/${full}`);
            },
            () => setNoteOpen(false),
        );
    };

    // ---- 行内拖拽移动 ----
    const onRowDragStart = (e: React.DragEvent, entry: FileEntry) => {
        e.dataTransfer.setData(DND_TYPE, join(entry.name));
        e.dataTransfer.effectAllowed = 'move';
    };
    const onFolderDrop = (e: React.DragEvent, folder: FileEntry) => {
        e.preventDefault();
        e.stopPropagation();
        setDropTarget(null);
        setDragOver(false);
        const src = e.dataTransfer.getData(DND_TYPE);
        if (!src) return;
        const dest = join(folder.name);
        if (src === dest) return;
        doMoveTo(src, dest);
    };

    const actions = (e: FileEntry) => (
        <span className="flex gap-0.5 shrink-0 flex-wrap justify-end">
            {!e.dir && (
                <>
                    <a href={api.downloadUrl(join(e.name), true)} download={e.name}>
                        <Button variant="ghost" size="sm">
                            下载
                        </Button>
                    </a>
                    <Button variant="ghost" size="sm" onClick={() => setShareTarget(e)}>
                        分享
                    </Button>
                </>
            )}
            <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                    setRenameTarget(e);
                    setRenameName(e.name);
                }}
            >
                重命名
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setMoveTarget(e)}>
                移动
            </Button>
            <Button variant="ghost-danger" size="sm" onClick={() => setDeleteTarget(e)}>
                删除
            </Button>
        </span>
    );

    return (
        <div
            onDragOver={(e) => {
                e.preventDefault();
                if (e.dataTransfer.types.includes('Files')) setDragOver(true);
            }}
            onDragLeave={() => setDragOver(false)}
            onDrop={(e) => {
                e.preventDefault();
                setDragOver(false);
                if (e.dataTransfer.types.includes('Files')) onUpload(e.dataTransfer.files);
            }}
        >
            <div className="flex items-center gap-3 flex-wrap mb-3">
                <h2 className="text-xl font-extrabold">📁 我的文件</h2>
                <div className="ml-auto flex gap-2 flex-wrap">
                    <input
                        ref={fileInput}
                        type="file"
                        multiple
                        hidden
                        onChange={(e) => onUpload(e.target.files)}
                    />
                    <Button
                        variant="primary"
                        size="sm"
                        disabled={uploading}
                        onClick={() => fileInput.current?.click()}
                    >
                        ⬆ {uploading ? '上传中…' : '上传'}
                    </Button>
                    <Button size="sm" onClick={() => setMkdirOpen(true)}>
                        新建文件夹
                    </Button>
                    <Button size="sm" onClick={() => setNoteOpen(true)}>
                        📔 新建笔记
                    </Button>
                    <Button
                        size="sm"
                        onClick={() => switchView(view === 'list' ? 'grid' : 'list')}
                    >
                        {view === 'list' ? '🖼️ 缩略图' : '📄 列表'}
                    </Button>
                    <Button size="sm" onClick={load}>
                        刷新
                    </Button>
                </div>
            </div>

            <Breadcrumb path={path} />

            <Card
                className={cn('p-0 overflow-hidden', dragOver && 'border-leaf border-dashed')}
            >
                {loading ? (
                    <div className="text-center text-ink-soft py-10 text-sm">加载中…</div>
                ) : entries.length === 0 ? (
                    <div className="text-center text-ink-soft py-10 text-sm">
                        这里空空如也,拖拽文件到此处上传 🍃
                    </div>
                ) : view === 'list' ? (
                    <div>
                        {entries.map((e, idx) => (
                            <div
                                key={e.name}
                                draggable
                                onDragStart={(ev) => onRowDragStart(ev, e)}
                                onDragOver={(ev) => {
                                    if (
                                        e.dir &&
                                        ev.dataTransfer.types.includes(DND_TYPE)
                                    ) {
                                        ev.preventDefault();
                                        ev.stopPropagation();
                                        setDropTarget(e.name);
                                    }
                                }}
                                onDragLeave={() =>
                                    dropTarget === e.name && setDropTarget(null)
                                }
                                onDrop={(ev) => e.dir && onFolderDrop(ev, e)}
                                className={cn(
                                    'flex items-center gap-2.5 px-4 py-2 border-b border-dashed border-line last:border-b-0 hover:bg-paper-2/60 flex-wrap',
                                    dropTarget === e.name &&
                                        'bg-leaf-soft outline-2 outline-dashed outline-leaf -outline-offset-2',
                                )}
                            >
                                <button
                                    className="flex items-center gap-2 flex-1 min-w-0 basis-full sm:basis-auto font-bold text-left cursor-pointer truncate"
                                    onClick={() => openEntry(e, idx)}
                                    title={e.dir ? `${e.name}(可把文件拖进来)` : e.name}
                                >
                                    <span className="text-lg shrink-0">
                                        {KIND_ICON[fileKind(e.name, e.dir)]}
                                    </span>
                                    <span className="truncate">{e.name}</span>
                                </button>
                                <span className="text-xs text-ink-soft w-20 text-right hidden sm:block shrink-0">
                                    {e.dir ? '-' : formatBytes(e.size)}
                                </span>
                                <span className="text-xs text-ink-soft w-28 text-right hidden lg:block shrink-0">
                                    {formatTime(e.mtime)}
                                </span>
                                {actions(e)}
                            </div>
                        ))}
                    </div>
                ) : (
                    <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3 p-4">
                        {entries.map((e, idx) => (
                            <div
                                key={e.name}
                                draggable
                                onDragStart={(ev) => onRowDragStart(ev, e)}
                                onDragOver={(ev) => {
                                    if (
                                        e.dir &&
                                        ev.dataTransfer.types.includes(DND_TYPE)
                                    ) {
                                        ev.preventDefault();
                                        ev.stopPropagation();
                                        setDropTarget(e.name);
                                    }
                                }}
                                onDragLeave={() =>
                                    dropTarget === e.name && setDropTarget(null)
                                }
                                onDrop={(ev) => e.dir && onFolderDrop(ev, e)}
                                className={cn(
                                    'rounded-2xl border-2 border-line overflow-hidden bg-paper flex flex-col',
                                    dropTarget === e.name && 'border-leaf bg-leaf-soft',
                                )}
                            >
                                <button
                                    className="cursor-pointer text-left"
                                    onClick={() => openEntry(e, idx)}
                                    title={e.name}
                                >
                                    <Thumb path={join(e.name)} name={e.name} dir={e.dir} />
                                    <div className="px-2.5 pt-1.5 font-bold text-xs truncate">
                                        {e.name}
                                    </div>
                                    <div className="px-2.5 pb-1 text-[11px] text-ink-soft">
                                        {e.dir ? '文件夹' : formatBytes(e.size)}
                                    </div>
                                </button>
                                <div className="px-1 pb-1.5 flex justify-center">
                                    {actions(e)}
                                </div>
                            </div>
                        ))}
                    </div>
                )}
            </Card>
            <p className="text-xs text-ink-soft mt-2">
                💡 拖拽文件行到文件夹上可直接移动;拖拽本地文件到列表上传
            </p>

            {/* 新建文件夹 */}
            <Dialog open={mkdirOpen} onOpenChange={(o) => !o && setMkdirOpen(false)}>
                <DialogContent title="新建文件夹">
                    <Input
                        placeholder="文件夹名称"
                        value={mkdirName}
                        onChange={(e) => setMkdirName(e.target.value)}
                    />
                    <DialogFooter
                        onOk={() =>
                            mkdirName.trim() &&
                            run(
                                () => api.mkdir(join(mkdirName.trim())),
                                () => {
                                    setMkdirOpen(false);
                                    setMkdirName('');
                                },
                            )
                        }
                    />
                </DialogContent>
            </Dialog>

            {/* 新建笔记 */}
            <Dialog open={noteOpen} onOpenChange={(o) => !o && setNoteOpen(false)}>
                <DialogContent title="新建笔记">
                    <Input
                        placeholder="笔记名称(自动加 .md)"
                        value={noteName}
                        onChange={(e) => setNoteName(e.target.value)}
                        onKeyDown={(e) => e.key === 'Enter' && newNote()}
                    />
                    <DialogFooter onOk={newNote} okText="创建并编辑" />
                </DialogContent>
            </Dialog>

            {/* 重命名 */}
            <Dialog
                open={renameTarget !== null}
                onOpenChange={(o) => !o && setRenameTarget(null)}
            >
                <DialogContent title={`重命名「${renameTarget?.name ?? ''}」`}>
                    <Input
                        value={renameName}
                        onChange={(e) => setRenameName(e.target.value)}
                    />
                    <DialogFooter
                        onOk={() =>
                            renameTarget &&
                            run(
                                () =>
                                    api.rename(join(renameTarget.name), renameName.trim()),
                                () => setRenameTarget(null),
                            )
                        }
                    />
                </DialogContent>
            </Dialog>

            {/* 移动:目录选择器 */}
            <FolderPicker
                open={moveTarget !== null}
                title={`移动「${moveTarget?.name ?? ''}」到…`}
                onClose={() => setMoveTarget(null)}
                onSelect={(dir) => moveTarget && doMoveTo(join(moveTarget.name), dir)}
            />

            {/* 删除(进回收站) */}
            <Dialog
                open={deleteTarget !== null}
                onOpenChange={(o) => !o && setDeleteTarget(null)}
            >
                <DialogContent title="放进垃圾桶">
                    <p className="text-sm">
                        把「{deleteTarget?.name}」放进垃圾桶?30 天内可以在垃圾桶里找回,
                        30 天后自动清理。
                    </p>
                    <DialogFooter
                        okText="放进垃圾桶"
                        okDanger
                        onOk={() =>
                            deleteTarget &&
                            run(
                                () => api.remove([join(deleteTarget.name)]),
                                () => setDeleteTarget(null),
                            )
                        }
                    />
                </DialogContent>
            </Dialog>

            {/* 分享 */}
            <Dialog open={shareTarget !== null} onOpenChange={(o) => !o && closeShare()}>
                <DialogContent title={`分享「${shareTarget?.name ?? ''}」`}>
                    {shareResult ? (
                        <div>
                            <p className="text-sm mb-2">
                                {shareResult.type === 'direct'
                                    ? '直链已生成,可直接粘贴到播放器/下载工具 🎉'
                                    : '分享链接已生成 🎉'}
                            </p>
                            <code className="block bg-paper-2 rounded-xl px-3 py-2 text-xs break-all">
                                {shareLink(shareResult)}
                            </code>
                            {sharePass && shareResult.type === 'page' && (
                                <p className="text-sm mt-2">
                                    提取密码:<code className="bg-paper-2 rounded px-2">{sharePass}</code>
                                </p>
                            )}
                            <div className="flex gap-2 mt-4">
                                <Button
                                    variant="primary"
                                    onClick={() =>
                                        copyText(
                                            shareLink(shareResult) +
                                                (sharePass && shareResult.type === 'page'
                                                    ? ` 密码: ${sharePass}`
                                                    : ''),
                                        )
                                    }
                                >
                                    复制链接
                                </Button>
                                <Button onClick={closeShare}>完成</Button>
                            </div>
                        </div>
                    ) : (
                        <div className="flex flex-col gap-3">
                            <NativeSelect
                                value={shareType}
                                onChange={(e) => setShareType(e.target.value)}
                            >
                                <option value="page">分享页(可设密码,给人看)</option>
                                <option value="direct">直链(给播放器/下载工具用)</option>
                            </NativeSelect>
                            {shareType === 'page' && (
                                <Input
                                    placeholder="提取密码(留空则无需密码)"
                                    value={sharePass}
                                    onChange={(e) => setSharePass(e.target.value)}
                                />
                            )}
                            <NativeSelect
                                value={shareExpire}
                                onChange={(e) => setShareExpire(e.target.value)}
                            >
                                <option value="0">永久有效</option>
                                <option value="24">1 天</option>
                                <option value="168">7 天</option>
                                <option value="720">30 天</option>
                            </NativeSelect>
                            <p className="text-xs text-ink-soft">
                                生成后可在「设置 → 分享管理」统一查看和删除
                            </p>
                            <DialogFooter onOk={doShare} okText="生成链接" />
                        </div>
                    )}
                </DialogContent>
            </Dialog>

            {previewIdx !== null && entries[previewIdx] && (
                <Preview
                    entries={entries}
                    index={previewIdx}
                    dirPath={path}
                    onNavigate={setPreviewIdx}
                    onClose={() => setPreviewIdx(null)}
                />
            )}
        </div>
    );
}
