import { useEffect, useRef, useState } from 'react';
import type PhotoSwipe from 'photoswipe';
import { Loader2, X } from 'lucide-react';
import { api, type FileEntry } from '../api';
import { fileKind } from '../util';
import { Button } from './ui/button';

interface Props {
    entries: FileEntry[];
    index: number;
    dirPath: string;
    onNavigate: (index: number) => void;
    onClose: () => void;
    source?: { url: string; downloadUrl: string };
}

export default function ImagePreview({ entries, index, dirPath, onNavigate, onClose, source }: Props) {
    const callbacks = useRef({ onNavigate, onClose });
    callbacks.current = { onNavigate, onClose };
    const [gallery] = useState(() => {
        const images = entries.flatMap((entry, originalIndex) => {
            if (entry.dir || fileKind(entry.name) !== 'image') return [];
            const path = dirPath ? `${dirPath}/${entry.name}` : entry.name;
            return [{ src: source?.url ?? api.downloadUrl(path), download: source?.downloadUrl ?? api.downloadUrl(path, true), alt: entry.name, width: 1600, height: 1000, originalIndex }];
        });
        return { images, index: Math.max(0, images.findIndex((image) => image.originalIndex === index)) };
    });
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(false);

    useEffect(() => {
        let disposed = false;
        let viewer: PhotoSwipe | undefined;
        const originalOverflow = document.body.style.overflow;
        document.body.style.overflow = 'hidden';

        (async () => {
            const [{ default: PhotoSwipeClass }] = await Promise.all([
                import('photoswipe'),
                import('photoswipe/style.css'),
            ]);
            if (disposed) return;
            const instance = new PhotoSwipeClass({
                dataSource: gallery.images,
                index: gallery.index,
                bgOpacity: 0.94,
                loop: false,
                wheelToZoom: true,
                showHideAnimationType: 'fade',
                closeTitle: '关闭',
                zoomTitle: '缩放',
                arrowPrevTitle: '上一张',
                arrowNextTitle: '下一张',
                errorMsg: '图片加载失败',
                padding: { top: 60, bottom: 24, left: 16, right: 16 },
            });
            viewer = instance;
            const measured = new Set<number>();
            const measure = (content: { element?: HTMLElement; index: number }) => {
                const image = content.element;
                if (!(image instanceof HTMLImageElement) || !image.naturalWidth || measured.has(content.index)) return;
                measured.add(content.index);
                // 文件列表没有像素尺寸，图片加载后用实际尺寸更新缩放边界。
                Object.assign(gallery.images[content.index], { width: image.naturalWidth, height: image.naturalHeight });
                queueMicrotask(() => {
                    if (!disposed && instance.isOpen) instance.refreshSlideContent(content.index);
                });
            };
            instance.on('loadComplete', ({ content, isError }) => { if (!isError) measure(content); });
            instance.on('contentActivate', ({ content }) => measure(content));
            instance.on('uiRegister', () => {
                instance.ui?.registerElement({
                    name: 'filename',
                    order: 5,
                    isButton: false,
                    appendTo: 'bar',
                    onInit: (element) => {
                        Object.assign(element.style, { alignSelf: 'center', flex: '1', minWidth: '0', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: '#fff', fontSize: '14px' });
                        const update = () => { element.textContent = gallery.images[instance.currIndex]?.alt ?? ''; };
                        instance.on('change', update);
                        update();
                    },
                });
            });
            instance.on('change', () => {
                const image = gallery.images[instance.currIndex];
                if (image) callbacks.current.onNavigate(image.originalIndex);
            });
            instance.on('destroy', () => {
                if (!disposed) callbacks.current.onClose();
            });
            instance.init();
            setLoading(false);
        })().catch(() => {
            if (!disposed) { setError(true); setLoading(false); }
        });

        return () => {
            disposed = true;
            viewer?.destroy();
            document.body.style.overflow = originalOverflow;
        };
    }, [gallery]);

    if (!loading && !error) return null;
    return (
        <div role="dialog" aria-modal="true" aria-label="图片预览" className="fixed inset-0 z-[100000] flex items-center justify-center bg-black/95 text-white" onKeyDown={(event) => { if (event.key === 'Escape') onClose(); }}>
            <Button autoFocus size="icon" variant="ghost" className="absolute right-3 top-3" aria-label="关闭" onClick={onClose}><X className="size-5" /></Button>
            {error ? <p>查看器加载失败，<a className="underline" href={gallery.images[gallery.index]?.download} download>下载图片</a></p> : <Loader2 className="size-6 animate-spin" aria-label="加载中" />}
        </div>
    );
}
