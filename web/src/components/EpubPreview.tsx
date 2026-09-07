import { useEffect, useRef, useState } from 'react';
import type { Book, Rendition, Location, NavItem } from 'epubjs';
import { ChevronLeft, ChevronRight, Loader2 } from 'lucide-react';
import { Button } from './ui/button';
import './EpubPreview.css';

function flatten(items: NavItem[], depth = 0): { href: string; label: string }[] {
    return items.flatMap((item) => [
        { href: item.href, label: `${'  '.repeat(depth)}${item.label.trim()}` },
        ...flatten(item.subitems ?? [], depth + 1),
    ]);
}

export default function EpubPreview({ url, downloadUrl, name }: { url: string; downloadUrl: string; name: string }) {
    const host = useRef<HTMLDivElement>(null);
    const rendition = useRef<Rendition | null>(null);
    const [toc, setToc] = useState<{ href: string; label: string }[]>([]);
    const [chapter, setChapter] = useState('');
    const [location, setLocation] = useState<Location | null>(null);
    const [busy, setBusy] = useState(true);
    const [error, setError] = useState('');
    const [size, setSize] = useState(18);

    useEffect(() => {
        let disposed = false;
        let book: Book | undefined;
        let view: Rendition | undefined;
        let resize: ResizeObserver | undefined;
        let theme: MutationObserver | undefined;
        const controller = new AbortController();
        const element = host.current!;
        const storageKey = `pd:epub:${url}`;
        const fail = (reason: unknown) => {
            if (!disposed) { setError(reason instanceof Error ? reason.message : '电子书加载失败'); setBusy(false); }
        };
        (async () => {
            const [{ default: ePub }, response] = await Promise.all([
                import('epubjs'), fetch(url, { signal: controller.signal }),
            ]);
            if (!response.ok) throw new Error(`读取失败 (${response.status})`);
            const data = await response.arrayBuffer();
            if (disposed) return;
            book = ePub({ replacements: 'blobUrl' });
            await book.open(data, 'binary');
            if (disposed) return;
            const chapters = flatten((await book.loaded.navigation).toc);
            if (disposed) return;
            setToc(chapters);
            view = book.renderTo(element, {
                width: element.clientWidth, height: element.clientHeight,
                flow: 'paginated', spread: 'none', allowScriptedContent: false,
            });
            rendition.current = view;
            const applyTheme = () => {
                const style = getComputedStyle(document.documentElement);
                view?.themes.override('color', style.getPropertyValue('--ink').trim(), true);
                view?.themes.override('background-color', style.getPropertyValue('--paper').trim(), true);
            };
            view.themes.fontSize('18px');
            view.themes.default({ 'p': { 'line-height': '1.8' }, 'img': { 'max-width': '100%' } });
            applyTheme();
            theme = new MutationObserver(applyTheme);
            theme.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });
            view.on('relocated', (next: Location) => {
                if (disposed) return;
                setLocation(next);
                const match = chapters.find((item) => item.href.split('#')[0] === next.start.href.split('#')[0]);
                setChapter(match?.href ?? '');
                try { localStorage.setItem(storageKey, next.start.cfi); } catch { /* Storage may be disabled. */ }
            });
            view.on('displayError', fail);
            let saved: string | undefined;
            try { saved = localStorage.getItem(storageKey) ?? undefined; } catch { /* Storage may be disabled. */ }
            try { await view.display(saved); } catch { await view.display(); }
            if (disposed) return;
            resize = new ResizeObserver(() => {
                if (!disposed && element.clientWidth && element.clientHeight) view?.resize(element.clientWidth, element.clientHeight);
            });
            resize.observe(element);
            setBusy(false);
        })().catch(fail);
        return () => {
            disposed = true;
            controller.abort();
            resize?.disconnect();
            theme?.disconnect();
            rendition.current = null;
            book?.destroy();
            element.replaceChildren();
        };
    }, [url]);

    const move = async (target: 'prev' | 'next' | { href: string }) => {
        if (busy || !rendition.current) return;
        setBusy(true);
        try {
            if (typeof target === 'string') await rendition.current[target]();
            else await rendition.current.display(target.href);
        } catch { setError('无法打开此章节，请重新打开电子书'); }
        finally { setBusy(false); }
    };

    return (
        <div className="pd-epub-reader" aria-label={name} aria-busy={busy}>
            <div className="pd-epub-toolbar">
                <select aria-label="章节目录" value={chapter} disabled={busy || !toc.length || Boolean(error)} onChange={(event) => void move({ href: event.target.value })}>
                    <option value="" disabled>目录</option>
                    {toc.map((item, index) => <option key={`${item.href}-${index}`} value={item.href}>{item.label}</option>)}
                </select>
                <select aria-label="字号" value={size} disabled={busy || Boolean(error)} onChange={(event) => { const value = Number(event.target.value); setSize(value); rendition.current?.themes.fontSize(`${value}px`); }}>
                    {[16, 18, 20, 24].map((value) => <option key={value} value={value}>{value} px</option>)}
                </select>
            </div>
            <div className="pd-epub-stage">
                <div ref={host} className="pd-epub-pages" />
                {busy && <div className="pd-epub-status" role="status"><Loader2 className="size-5 animate-spin" /></div>}
                {error && <div className="pd-epub-status" role="alert"><p>{error} <a className="text-leaf-dark underline" href={downloadUrl} download>下载电子书</a></p></div>}
            </div>
            <div className="pd-epub-footer">
                <Button size="icon" aria-label="上一页" title="上一页" disabled={busy || Boolean(error) || location?.atStart} onClick={() => void move('prev')}><ChevronLeft className="size-4" /></Button>
                <span className="text-xs text-ink-soft tabular-nums">{location ? `本章 ${location.start.displayed.page} / ${location.start.displayed.total}` : ''}</span>
                <Button size="icon" aria-label="下一页" title="下一页" disabled={busy || Boolean(error) || location?.atEnd} onClick={() => void move('next')}><ChevronRight className="size-4" /></Button>
            </div>
        </div>
    );
}
