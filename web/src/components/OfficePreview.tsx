import { useEffect, useRef, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { cn } from '../lib/utils';

/**
 * Office 文件预览:全部在浏览器端渲染,服务器只负责出文件流,
 * 渲染库按格式动态 import(不进主包,小内存 VPS 零负担)。
 * - docx → docx-preview
 * - xlsx / xls → SheetJS 转 HTML 表格(多 sheet 可切换)
 * - pptx → pptx-preview
 */
export default function OfficePreview({ url, name }: { url: string; name: string }) {
    const ext = name.split('.').pop()?.toLowerCase() ?? '';
    const containerRef = useRef<HTMLDivElement>(null);
    const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading');
    const [errMsg, setErrMsg] = useState('');
    const [sheets, setSheets] = useState<Array<{ name: string; html: string }>>([]);
    const [activeSheet, setActiveSheet] = useState(0);

    useEffect(() => {
        let cancelled = false;
        setState('loading');
        setSheets([]);
        setActiveSheet(0);

        (async () => {
            const resp = await fetch(url);
            if (!resp.ok) throw new Error(`文件读取失败 (${resp.status})`);
            const buf = await resp.arrayBuffer();
            if (cancelled) return;

            if (ext === 'docx') {
                const { renderAsync } = await import('docx-preview');
                if (cancelled || !containerRef.current) return;
                containerRef.current.innerHTML = '';
                await renderAsync(buf, containerRef.current, undefined, {
                    ignoreLastRenderedPageBreak: true,
                });
            } else if (ext === 'xlsx' || ext === 'xls') {
                const XLSX = await import('xlsx');
                const wb = XLSX.read(new Uint8Array(buf), { type: 'array' });
                if (cancelled) return;
                setSheets(
                    wb.SheetNames.map((n) => ({
                        name: n,
                        html: XLSX.utils.sheet_to_html(wb.Sheets[n]!),
                    })),
                );
            } else if (ext === 'pptx') {
                const { init } = await import('pptx-preview');
                if (cancelled || !containerRef.current) return;
                containerRef.current.innerHTML = '';
                const width = Math.min(containerRef.current.clientWidth || 800, 820);
                const previewer = init(containerRef.current, {
                    width,
                    height: Math.round((width * 9) / 16),
                });
                await previewer.preview(buf);
            } else {
                throw new Error('不支持的格式');
            }
            if (!cancelled) setState('ready');
        })().catch((e) => {
            if (!cancelled) {
                setErrMsg(e instanceof Error ? e.message : '渲染失败');
                setState('error');
            }
        });

        return () => {
            cancelled = true;
        };
    }, [url, ext]);

    if (state === 'error') {
        return (
            <p className="text-sm text-ink-soft py-6 text-center">
                预览失败:{errMsg}。可下载后本地打开。
            </p>
        );
    }

    return (
        <div>
            {state === 'loading' && (
                <div className="flex items-center justify-center gap-2 py-10 text-sm text-ink-soft">
                    <Loader2 className="size-4 animate-spin" /> 正在渲染文档…
                </div>
            )}
            {/* 表格:多 sheet 切换 */}
            {sheets.length > 0 && (
                <div>
                    {sheets.length > 1 && (
                        <div className="flex gap-1 mb-2 flex-wrap">
                            {sheets.map((s, i) => (
                                <button
                                    key={s.name}
                                    className={cn(
                                        'px-3 py-1 rounded-full text-xs font-bold cursor-pointer border transition-colors',
                                        i === activeSheet
                                            ? 'bg-leaf text-white border-leaf'
                                            : 'bg-paper border-line hover:border-leaf/60',
                                    )}
                                    onClick={() => setActiveSheet(i)}
                                >
                                    {s.name}
                                </button>
                            ))}
                        </div>
                    )}
                    <div
                        className="office-sheet"
                        // SheetJS 输出的是自家生成的表格标记,不含用户脚本
                        dangerouslySetInnerHTML={{ __html: sheets[activeSheet]?.html ?? '' }}
                    />
                </div>
            )}
            <div
                ref={containerRef}
                className={cn(
                    ext === 'docx' && 'office-docx',
                    ext === 'pptx' && 'office-pptx',
                    (state !== 'ready' || sheets.length > 0) && ext !== 'docx' && ext !== 'pptx'
                        ? 'hidden'
                        : undefined,
                )}
            />
        </div>
    );
}
