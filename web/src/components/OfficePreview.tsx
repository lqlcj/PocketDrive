import { useEffect, useRef, useState } from 'react';
import { Loader2 } from 'lucide-react';

/**
 * DOCX 文件预览:在浏览器端渲染,服务器只负责出文件流。
 * 电子表格和演示文稿不再交给存在已知漏洞的前端解析器处理。
 */
export default function OfficePreview({ url, name }: { url: string; name: string }) {
    const ext = name.split('.').pop()?.toLowerCase() ?? '';
    const containerRef = useRef<HTMLDivElement>(null);
    const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading');
    const [errMsg, setErrMsg] = useState('');

    useEffect(() => {
        let cancelled = false;
        setState('loading');

        (async () => {
            if (ext !== 'docx') throw new Error('此格式已停用在线预览');
            const resp = await fetch(url);
            if (!resp.ok) throw new Error(`文件读取失败 (${resp.status})`);
            const buf = await resp.arrayBuffer();
            if (cancelled) return;

            const { renderAsync } = await import('docx-preview');
            if (cancelled || !containerRef.current) return;
            containerRef.current.replaceChildren();
            await renderAsync(buf, containerRef.current, undefined, {
                ignoreLastRenderedPageBreak: true,
            });
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
            <div ref={containerRef} className="office-docx" />
        </div>
    );
}
