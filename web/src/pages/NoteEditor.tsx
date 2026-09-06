import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import {
    MDXEditor,
    BlockTypeSelect,
    BoldItalicUnderlineToggles,
    ChangeCodeMirrorLanguage,
    CodeToggle,
    ConditionalContents,
    CreateLink,
    InsertCodeBlock,
    InsertImage,
    InsertTable,
    InsertThematicBreak,
    ListsToggle,
    Separator,
    UndoRedo,
    codeBlockPlugin,
    codeMirrorPlugin,
    headingsPlugin,
    imagePlugin,
    linkDialogPlugin,
    linkPlugin,
    listsPlugin,
    markdownShortcutPlugin,
    quotePlugin,
    tablePlugin,
    thematicBreakPlugin,
    toolbarPlugin,
    type Translation,
} from '@mdxeditor/editor';
import '@mdxeditor/editor/style.css';
import './NoteEditor.css';
import { NotebookPen } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import { Button } from '../components/ui/button';

// MDXEditor 界面中文化;未收录的 key 回落英文默认文案
const zh: Record<string, string> = {
    'toolbar.undo': '撤销 {{shortcut}}',
    'toolbar.redo': '重做 {{shortcut}}',
    'toolbar.bold': '加粗',
    'toolbar.removeBold': '取消加粗',
    'toolbar.italic': '斜体',
    'toolbar.removeItalic': '取消斜体',
    'toolbar.underline': '下划线',
    'toolbar.removeUnderline': '取消下划线',
    'toolbar.strikethrough': '删除线',
    'toolbar.removeStrikethrough': '取消删除线',
    'toolbar.superscript': '上标',
    'toolbar.removeSuperscript': '取消上标',
    'toolbar.subscript': '下标',
    'toolbar.removeSubscript': '取消下标',
    'toolbar.inlineCode': '行内代码',
    'toolbar.removeInlineCode': '取消行内代码',
    'toolbar.bulletedList': '无序列表',
    'toolbar.numberedList': '有序列表',
    'toolbar.checkList': '任务列表',
    'toolbar.link': '插入链接',
    'toolbar.image': '插入图片',
    'toolbar.codeBlock': '插入代码块',
    'toolbar.table': '插入表格',
    'toolbar.thematicBreak': '插入分割线',
    'toolbar.blockTypes.paragraph': '正文',
    'toolbar.blockTypes.quote': '引用',
    'toolbar.blockTypes.heading': '标题 {{level}}',
    'toolbar.blockTypeSelect.placeholder': '段落样式',
    'toolbar.blockTypeSelect.selectBlockTypeTooltip': '选择段落样式',
    'createLink.url': '链接地址',
    'createLink.urlPlaceholder': '选择或粘贴链接',
    'createLink.text': '链接文字',
    'createLink.textTooltip': '链接上显示的文字',
    'createLink.title': '链接标题',
    'createLink.titleTooltip': '鼠标悬停时显示的标题',
    'createLink.saveTooltip': '确定',
    'createLink.cancelTooltip': '取消',
    'dialogControls.save': '保存',
    'dialogControls.cancel': '取消',
    'dialog.close': '关闭对话框',
    'uploadImage.dialogTitle': '插入图片',
    'uploadImage.uploadInstructions': '从本机上传图片:',
    'uploadImage.addViaUrlInstructions': '或输入图片链接:',
    'uploadImage.addViaUrlInstructionsNoUpload': '输入图片链接:',
    'uploadImage.autoCompletePlaceholder': '选择或粘贴图片地址',
    'uploadImage.alt': '说明文字:',
    'uploadImage.title': '标题:',
    'uploadImage.width': '宽度:',
    'uploadImage.height': '高度:',
    'codeBlock.selectLanguage': '选择代码语言',
    'codeBlock.language': '代码块语言',
    'codeBlock.inlineLanguage': '语言',
    'codeblock.delete': '删除代码块',
};

const zhTranslation: Translation = (key, defaultValue, interpolations) => {
    let value = zh[key] ?? defaultValue;
    if (interpolations) {
        for (const [k, v] of Object.entries(interpolations)) {
            value = value.replaceAll(`{{${k}}}`, String(v));
        }
    }
    return value;
};

/** 在线 Markdown 笔记:MDXEditor 所见即所得编辑 */
export default function NoteEditor() {
    const params = useParams();
    const path = params['*'] ?? '';
    const name = path.split('/').pop() ?? path;

    const [text, setText] = useState<string | null>(null);
    const [saving, setSaving] = useState(false);
    const [dirty, setDirty] = useState(false);
    // MDXEditor 暗色通过 dark-theme 类开关;这里响应式跟随 html.dark,
    // 否则其样式里靠后的 :root 浅色调色板会盖住 html.dark,文字变黑
    const [isDark, setIsDark] = useState(() => document.documentElement.classList.contains('dark'));
    // 编辑器首次 onChange 是装载时的归一化触发,不是用户输入,跳过避免误标「未保存」
    const skipFirstChange = useRef(true);
    const savedText = useRef('');

    useEffect(() => {
        const root = document.documentElement;
        const observer = new MutationObserver(() =>
            setIsDark(root.classList.contains('dark')),
        );
        observer.observe(root, { attributes: true, attributeFilter: ['class'] });
        return () => observer.disconnect();
    }, []);

    useEffect(() => {
        let cancelled = false;
        setText(null);
        api.content(path)
            .then((t) => {
                if (cancelled) return;
                setText(t);
                savedText.current = t;
                skipFirstChange.current = true;
            })
            .catch((e) => {
                if (cancelled) return;
                toast.error(e instanceof Error ? e.message : '读取失败');
                setText('');
                savedText.current = '';
            });
        return () => {
            cancelled = true;
        };
    }, [path]);

    const handleChange = useCallback((md: string) => {
        if (skipFirstChange.current) {
            skipFirstChange.current = false;
            return;
        }
        setText(md);
        setDirty(md !== savedText.current);
    }, []);

    const parentDir = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '';

    // 图片传到笔记同目录;文件名加时间戳避免覆盖已有图片
    const uploadImage = useCallback(async (file: File): Promise<string> => {
        const name = `${Date.now()}-${file.name.replace(/[\\/:*?"<>|]/g, '_')}`;
        await api.upload(parentDir, [new File([file], name, { type: file.type })]);
        return `./${name}`;
    }, [parentDir]);

    // WYSIWYG 预览时把相对路径/下载路径解析成浏览器可加载的 URL
    const previewImage = useCallback(
        async (src: string): Promise<string> => {
            if (/^(?:https?:)?\/\//.test(src) || src.startsWith('data:')) return src;
            const rel = src.replace(/^\.\//, '').replace(/\/+$/, '');
            return api.downloadUrl(parentDir ? `${parentDir}/${rel}` : rel);
        },
        [parentDir],
    );

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
        return <div className="text-center text-ink-soft py-16">加载中…</div>;
    }

    return (
        <div className="flex flex-col h-[calc(var(--vh)*100-120px)]">
            <div className="flex items-center gap-2 mb-3 flex-wrap">
                <h2 className="text-lg font-extrabold truncate flex items-center gap-2">
                    <NotebookPen className="size-4.5 text-leaf-dark shrink-0" />
                    <span className="truncate">{name}</span>
                </h2>
                {dirty && <span className="text-xs text-amber-600 font-bold">未保存</span>}
                <div className="ml-auto flex gap-2">
                    <Link to={`/files/${parentDir}`}>
                        <Button size="sm">返回文件</Button>
                    </Link>
                    <Button variant="primary" size="sm" disabled={saving || !dirty} onClick={save}>
                        {saving ? '保存中…' : '保存 (Ctrl+S)'}
                    </Button>
                </div>
            </div>

            <div className="flex-1 min-h-0">
                <MDXEditor
                    key={path}
                    markdown={text}
                    onChange={handleChange}
                    spellCheck={false}
                    placeholder="用 Markdown 写点什么…"
                    translation={zhTranslation}
                    className={`pd-note-editor mdxeditor-full-height h-full${isDark ? ' dark-theme' : ''}`}
                    contentEditableClassName="pd-note-content"
                    plugins={[
                        headingsPlugin(),
                        listsPlugin(),
                        quotePlugin(),
                        thematicBreakPlugin(),
                        linkPlugin(),
                        linkDialogPlugin(),
                        tablePlugin(),
                        imagePlugin({
                            imageUploadHandler: uploadImage,
                            imagePreviewHandler: previewImage,
                            allowSetImageDimensions: true,
                        }),
                        codeBlockPlugin(),
                        codeMirrorPlugin(),
                        markdownShortcutPlugin(),
                        toolbarPlugin({
                            toolbarClassName: 'border-b border-line',
                            toolbarContents: () => (
                                <ConditionalContents
                                    options={[
                                        {
                                            when: (editor) => editor?.editorType === 'codeblock',
                                            contents: () => <ChangeCodeMirrorLanguage />,
                                        },
                                        {
                                            fallback: () => (
                                                <>
                                                    <UndoRedo />
                                                    <Separator />
                                                    <BlockTypeSelect />
                                                    <BoldItalicUnderlineToggles />
                                                    <ListsToggle />
                                                    <CodeToggle />
                                                    <CreateLink />
                                                    <InsertImage />
                                                    <Separator />
                                                    <InsertTable />
                                                    <InsertCodeBlock />
                                                    <InsertThematicBreak />
                                                </>
                                            ),
                                        },
                                    ]}
                                />
                            ),
                        }),
                    ]}
                />
            </div>
        </div>
    );
}
