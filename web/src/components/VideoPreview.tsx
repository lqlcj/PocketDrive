import { useEffect, useRef, useState } from 'react';
import type Plyr from 'plyr';
import type Mpegts from 'mpegts.js';
import { Loader2 } from 'lucide-react';
import { videoPlaybackMode } from '../util';

export default function VideoPreview({ url, name, downloadUrl }: { url: string; name: string; downloadUrl: string }) {
    const host = useRef<HTMLDivElement>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');

    useEffect(() => {
        const container = host.current!;
        const video = document.createElement('video');
        video.controls = true;
        video.playsInline = true;
        video.preload = 'metadata';
        video.setAttribute('aria-label', name);
        container.appendChild(video);
        let disposed = false;
        let player: Plyr | undefined;
        let engine: Mpegts.Player | undefined;
        setLoading(true);
        setError('');

        const fail = (message: string) => {
            if (disposed) return;
            setLoading(false);
            setError(message);
            video.pause();
            engine?.unload();
        };
        const mediaError = () => fail('视频读取失败，或当前浏览器不支持此视频的编码。');
        const ready = () => { if (!disposed) setLoading(false); };
        video.addEventListener('error', mediaError);
        video.addEventListener('loadedmetadata', ready);

        (async () => {
            const mode = videoPlaybackMode(name);
            if (!mode) { fail('此视频格式暂不支持在线播放。'); return; }
            const [{ default: PlyrClass }, , { default: iconUrl }] = await Promise.all([
                import('plyr'),
                import('plyr/dist/plyr.css'),
                import('../../node_modules/plyr/dist/plyr.svg?url'),
                import('./VideoPreview.css'),
            ]);
            if (disposed) return;
            player = new PlyrClass(video, {
                iconUrl,
                blankVideo: 'data:video/mp4;base64,',
                autoplay: false,
                controls: ['play-large', 'play', 'progress', 'current-time', 'duration', 'mute', 'volume', 'settings', 'pip', 'fullscreen'],
                settings: ['speed'],
                speed: { selected: 1, options: [0.5, 0.75, 1, 1.25, 1.5, 2] },
                keyboard: { focused: true, global: false },
                fullscreen: { enabled: true, fallback: false, iosNative: true },
                tooltips: { controls: true, seek: true },
                i18n: {
                    play: '播放', pause: '暂停', seek: '进度', seekLabel: '{currentTime} / {duration}',
                    played: '已播放', buffered: '已缓冲', currentTime: '当前时间', duration: '总时长',
                    volume: '音量', mute: '静音', unmute: '取消静音', settings: '设置',
                    speed: '倍速', normal: '正常', pip: '画中画', enterFullscreen: '全屏',
                    exitFullscreen: '退出全屏', menuBack: '返回', download: '下载',
                },
            });
            if (mode === 'native') {
                video.src = url;
            } else {
                const { default: mpegts } = await import('mpegts.js');
                if (disposed) return;
                if (!mpegts.isSupported()) { fail('当前浏览器不支持此视频的流式播放。'); return; }
                engine = mpegts.createPlayer({ type: mode, url, isLive: false }, {
                    enableWorker: true,
                    lazyLoadMaxDuration: 60,
                    autoCleanupSourceBuffer: true,
                    autoCleanupMaxBackwardDuration: 60,
                    autoCleanupMinBackwardDuration: 30,
                });
                engine.on(mpegts.Events.ERROR, mediaError);
                engine.attachMediaElement(video);
                engine.load();
            }
            // 浏览器可能阻止有声自动播放，保留播放按钮供用户启动。
            void video.play().catch(() => {});
        })().catch(() => fail('播放器加载失败，请稍后重新打开。'));

        return () => {
            disposed = true;
            video.removeEventListener('error', mediaError);
            video.removeEventListener('loadedmetadata', ready);
            video.pause();
            engine?.destroy();
            player?.destroy();
            video.removeAttribute('src');
            video.load();
            container.replaceChildren();
        };
    }, [url, name]);

    return (
        <div className="pd-video-preview">
            <div ref={host} className="pd-video-host" hidden={Boolean(error)} />
            {loading && <p className="flex items-center justify-center gap-2 py-3 text-sm text-ink-soft" role="status"><Loader2 className="size-4 animate-spin" /> 正在加载视频…</p>}
            {error && <p className="py-6 text-center text-sm" role="alert">{error} <a className="text-leaf-dark underline" href={downloadUrl} download>下载后播放</a></p>}
        </div>
    );
}
