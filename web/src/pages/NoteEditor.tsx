import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { toast } from 'sonner';
import { api } from '../api';
import { Button } from '../components/ui/button';
import { Textarea } from '../components/ui/input';
import { cn } from '../lib/utils';

/** 在线 Markdown 笔记:左边编辑,右边实时预览 */
export default function NoteEditor() {
    const params = useParams();
    const path = params['*'] ?? '';
    const name = path.split('/').pop() ?? path;

    const [text, setText] = useState<string | null>(null);
    const [saving, setSaving] = useState(false);
    const [dirty, setDirty] = useState(false);
    // 手机上没有并排空间:tab 切换
    const [mobileTab, setMobileTab] = useState<'edit' | 'preview'>('edit');
    const savedText = useRef('');

    useEffect(() => {
        api.content(path)
            .then((t) => {
                setText(t);
                savedText.current = t;
            })
            .catch((e) => {
                toast.error(e instanceof Error ? e.message : '读取失败');
                setText('');
            });
    }, [path]);

    const save = useCallback(async () => {
        if (text === null) return;
        setSaving(true);
        try {
            await api.writeFile(path, text);
            savedText.current = text;
            setDirty(false);
            toast.success('已保存');
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '保存失败');
        } finally {
            setSaving(false);
        }
    }, [path, text]);

    // Ctrl/Cmd+S 保存
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if ((e.ctrlKey || e.metaKey) && e.key === 's') {
                e.preventDefault();
                save();
            }
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [save]);

    if (text === null) {
        return <div className="text-center text-ink-soft py-16">📔 加载中…</div>;
    }

    const parentDir = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '';

    return (
        <div className="flex flex-col h-[calc(100vh-120px)]">
            <div className="flex items-center gap-2 mb-3 flex-wrap">
                <h2 className="text-lg font-extrabold truncate">📝 {name}</h2>
                {dirty && <span className="text-xs text-amber-600 font-bold">未保存</span>}
                <div className="ml-auto flex gap-2">
                    {/* 手机端 tab 切换 */}
                    <div className="md:hidden flex rounded-full border-2 border-line overflow-hidden">
                        <button
                            className={cn(
                                'px-3 py-1 text-xs font-bold cursor-pointer',
                                mobileTab === 'edit' ? 'bg-leaf text-white' : 'bg-paper',
                            )}
                            onClick={() => setMobileTab('edit')}
                        >
                            编辑
                        </button>
                        <button
                            className={cn(
                                'px-3 py-1 text-xs font-bold cursor-pointer',
                                mobileTab === 'preview' ? 'bg-leaf text-white' : 'bg-paper',
                            )}
                            onClick={() => setMobileTab('preview')}
                        >
                            预览
                        </button>
                    </div>
                    <Link to={`/files/${parentDir}`}>
                        <Button size="sm">返回文件</Button>
                    </Link>
                    <Button variant="primary" size="sm" disabled={saving || !dirty} onClick={save}>
                        {saving ? '保存中…' : '保存 (Ctrl+S)'}
                    </Button>
                </div>
            </div>

            <div className="flex-1 min-h-0 grid md:grid-cols-2 gap-3">
                <Textarea
                    value={text}
                    onChange={(e) => {
                        setText(e.target.value);
                        setDirty(e.target.value !== savedText.current);
                    }}
                    spellCheck={false}
                    placeholder="用 Markdown 写点什么…"
                    className={cn(
                        'h-full font-mono text-[13px] leading-relaxed rounded-2xl',
                        mobileTab === 'preview' && 'hidden md:block',
                    )}
                />
                <div
                    className={cn(
                        'h-full overflow-auto rounded-2xl border-2 border-line bg-paper p-4 prose-island',
                        mobileTab === 'edit' && 'hidden md:block',
                    )}
                >
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>{text}</ReactMarkdown>
                </div>
            </div>
        </div>
    );
}
