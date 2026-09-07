import { useEffect, useRef, useState } from 'react';
import { Loader2 } from 'lucide-react';

/**
 * DOCX 文件预览:在浏览器端渲染,服务器只负责出文件流。
 * 电子表格由 SheetPreview 单独处理。
 */
export default function OfficePreview({ url, name }: { url: string; name: string }) {
    const ext = name.split('.').pop()?.toLowerCase() ?? '';
    const frameRef = useRef<HTMLIFrameElement>(null);
    const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading');
    const [errMsg, setErrMsg] = useState('');

    useEffect(() => {
        let cancelled = false;
        const abort = new AbortController();
        const observer = new ResizeObserver(() => fitPages());
        const fitPages = () => {
            const frame = frameRef.current;
            const container = frame?.contentDocument?.body;
            if (!frame || !container) return;
            container.querySelectorAll<HTMLElement>('section.docx').forEach((page) => {
                page.style.zoom = String(Math.min(1, Math.max(0.1, (frame.clientWidth - 24) / page.offsetWidth)));
            });
            frame.style.height = `${Math.max(300, container.scrollHeight + 24)}px`;
        };
        setState('loading');

        (async () => {
            if (ext !== 'docx') throw new Error('此格式已停用在线预览');
            const resp = await fetch(url, { signal: abort.signal });
            if (!resp.ok) throw new Error(`文件读取失败 (${resp.status})`);
            const buf = await resp.arrayBuffer();
            if (cancelled) return;

            const { renderAsync } = await import('docx-preview');
            const frame = frameRef.current;
            if (cancelled || !frame) return;
            await new Promise<void>((resolve) => {
                frame.onload = () => resolve();
                frame.srcdoc = `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data: blob:; font-src data: blob:; frame-src 'none'; base-uri 'none'; form-action 'none'"><style>body{margin:0;overflow-wrap:anywhere}.docx-wrapper{padding:12px!important}section.docx{max-width:none}</style></head><body></body></html>`;
            });
            if (cancelled || !frame.contentDocument) return;
            // Same-origin access is only for sizing/rendering; the sandbox never permits scripts.
            const container = frame.contentDocument.body;
            await renderAsync(buf, container, undefined, {
                ignoreLastRenderedPageBreak: true,
                renderAltChunks: false,
                useBase64URL: true,
            });
            if (!cancelled) {
                container.querySelectorAll('a').forEach((link) => link.removeAttribute('href'));
                fitPages();
                observer.observe(frame.parentElement!);
                setState('ready');
            }
        })().catch((e) => {
            if (!cancelled) {
                setErrMsg(e instanceof Error ? e.message : '渲染失败');
                setState('error');
            }
        });

        return () => {
            cancelled = true;
            abort.abort();
            observer.disconnect();
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
            <iframe ref={frameRef} title={name} sandbox="allow-same-origin" referrerPolicy="no-referrer"
                className="w-full border-0" style={{ minHeight: 300 }} />
        </div>
    );
}
