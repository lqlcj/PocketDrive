import {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useMemo,
    useRef,
    useState,
} from 'react';
import type { ReactNode } from 'react';
import { api } from '../api';

// 全局音乐播放器。
//
// 和上传队列一样挂在路由之上:换目录、开对话框、切页面都不会重挂,音乐
// 一直响着。关键是那个 Audio 对象**不进 React 树**——写成 <audio> 元素的话,
// 任何一次父组件重挂(甚至 key 变了)都会打断播放;放在 ref 里才真"全局"。
//
// 面板只是它的遥控器:面板收起、隐藏都不影响播放,只有 close() 才是真停。

export interface Track {
    /** 完整路径(含 @挂载前缀),既用来取流,也是队列里的唯一键 */
    path: string;
    name: string;
}

/** off=放完就停;all=整个列表循环;one=单曲循环 */
export type RepeatMode = 'off' | 'all' | 'one';

const VOL_KEY = 'pd.player.volume';

interface PlayerAPI {
    queue: Track[];
    /** 当前在队列里的下标,-1 表示没在放 */
    index: number;
    current: Track | null;
    playing: boolean;
    time: number;
    duration: number;
    repeat: RepeatMode;
    volume: number;
    /** 取流失败/格式不支持时的提示,换歌时清掉 */
    error: string | null;
    /** 放一整个列表并从 start 首开始(点同一首会从头重放) */
    playList: (tracks: Track[], start?: number) => void;
    toggle: () => void;
    next: () => void;
    prev: () => void;
    seek: (t: number) => void;
    setVolume: (v: number) => void;
    cycleRepeat: () => void;
    /** 关掉播放器:停止播放并清空队列 */
    close: () => void;
}

const PlayerCtx = createContext<PlayerAPI | null>(null);

export function usePlayer(): PlayerAPI {
    const ctx = useContext(PlayerCtx);
    if (!ctx) throw new Error('usePlayer 必须在 PlayerProvider 内使用');
    return ctx;
}

function initialVolume(): number {
    const v = Number(localStorage.getItem(VOL_KEY));
    return Number.isFinite(v) && v >= 0 && v <= 1 ? v : 1;
}

export function PlayerProvider({ children }: { children: ReactNode }) {
    // 整个应用生命周期里只有这一个 Audio
    const ref = useRef<HTMLAudioElement | null>(null);
    if (!ref.current) ref.current = new Audio();
    const audio = ref.current;

    const [queue, setQueue] = useState<Track[]>([]);
    const [index, setIndex] = useState(-1);
    // 重放同一首(单曲列表循环、重复点同一个文件)时 path 不变,
    // 靠这个计数器把加载 effect 顶一次
    const [nonce, setNonce] = useState(0);
    const [playing, setPlaying] = useState(false);
    const [time, setTime] = useState(0);
    const [duration, setDuration] = useState(0);
    const [repeat, setRepeat] = useState<RepeatMode>('off');
    const [volume, setVol] = useState(initialVolume);
    const [error, setError] = useState<string | null>(null);

    const current = index >= 0 && index < queue.length ? (queue[index] ?? null) : null;
    const src = current?.path ?? null;

    // 'ended' 要看最新的 queue/index/repeat,而监听只挂一次,
    // 所以处理逻辑放 ref 里、每次渲染刷新,避免闭包冻住旧值
    const onEndRef = useRef<() => void>(() => {});
    onEndRef.current = () => {
        if (index + 1 < queue.length) {
            setIndex(index + 1);
            setNonce((n) => n + 1);
        } else if (repeat === 'all' && queue.length > 0) {
            setIndex(0);
            setNonce((n) => n + 1);
        } else {
            setPlaying(false); // 放完最后一首,停在这儿
        }
    };

    useEffect(() => {
        const onTime = () => setTime(audio.currentTime);
        const onMeta = () => setDuration(Number.isFinite(audio.duration) ? audio.duration : 0);
        const onPlay = () => setPlaying(true);
        const onPause = () => setPlaying(false);
        const onEnded = () => onEndRef.current();
        // flac/wma 这类容器 Chrome 未必认,给个明话而不是干瞪眼
        const onError = () => {
            setPlaying(false);
            setError('这个格式浏览器放不了');
        };
        audio.addEventListener('timeupdate', onTime);
        audio.addEventListener('durationchange', onMeta);
        audio.addEventListener('loadedmetadata', onMeta);
        audio.addEventListener('play', onPlay);
        audio.addEventListener('pause', onPause);
        audio.addEventListener('ended', onEnded);
        audio.addEventListener('error', onError);
        return () => {
            audio.removeEventListener('timeupdate', onTime);
            audio.removeEventListener('durationchange', onMeta);
            audio.removeEventListener('loadedmetadata', onMeta);
            audio.removeEventListener('play', onPlay);
            audio.removeEventListener('pause', onPause);
            audio.removeEventListener('ended', onEnded);
            audio.removeEventListener('error', onError);
        };
    }, [audio]);

    // 换歌:src 变了(或被 nonce 顶了)就重新加载并播放
    useEffect(() => {
        setTime(0);
        setDuration(0);
        if (src === null) {
            audio.pause();
            audio.removeAttribute('src');
            audio.load(); // 断掉正在下载的流,不然关掉了还在占带宽
            return;
        }
        setError(null);
        audio.src = api.downloadUrl(src);
        // 走到这儿必定是用户点出来的,不会被自动播放策略拦;
        // 真被拦或换歌打断了,catch 掉即可——面板会停在暂停态,再点一下就行
        void audio.play().catch(() => {});
    }, [audio, src, nonce]);

    // 单曲循环交给原生 loop:比在 ended 里重播更顺,中间不会断一下
    useEffect(() => {
        audio.loop = repeat === 'one';
    }, [audio, repeat]);

    useEffect(() => {
        audio.volume = volume;
        localStorage.setItem(VOL_KEY, String(volume));
    }, [audio, volume]);

    const playList = useCallback((tracks: Track[], start = 0) => {
        if (tracks.length === 0) return;
        const i = start >= 0 && start < tracks.length ? start : 0;
        setQueue(tracks);
        setIndex(i);
        setNonce((n) => n + 1);
    }, []);

    const toggle = useCallback(() => {
        if (audio.paused) void audio.play().catch(() => {});
        else audio.pause();
    }, [audio]);

    // 手点上一首/下一首一律绕圈,不看 repeat——按钮按下去就该有反应
    const next = useCallback(() => {
        setIndex((i) => {
            if (queue.length === 0) return i;
            return i + 1 >= queue.length ? 0 : i + 1;
        });
        setNonce((n) => n + 1);
    }, [queue.length]);

    const prev = useCallback(() => {
        // 已经放过几秒了,「上一首」先理解成回到本曲开头(和常见播放器一致)
        if (audio.currentTime > 3) {
            audio.currentTime = 0;
            setTime(0);
            return;
        }
        setIndex((i) => {
            if (queue.length === 0) return i;
            return i - 1 < 0 ? queue.length - 1 : i - 1;
        });
        setNonce((n) => n + 1);
    }, [audio, queue.length]);

    const seek = useCallback(
        (t: number) => {
            if (!Number.isFinite(audio.duration) || audio.duration <= 0) return;
            const v = Math.max(0, Math.min(audio.duration, t));
            audio.currentTime = v;
            setTime(v);
        },
        [audio],
    );

    const setVolume = useCallback((v: number) => {
        setVol(Math.max(0, Math.min(1, v)));
    }, []);

    const cycleRepeat = useCallback(() => {
        setRepeat((r) => (r === 'off' ? 'all' : r === 'all' ? 'one' : 'off'));
    }, []);

    const close = useCallback(() => {
        audio.pause();
        setQueue([]);
        setIndex(-1);
        setPlaying(false);
        setError(null);
    }, [audio]);

    // 系统媒体控制:锁屏、耳机线控、键盘媒体键都能操作。
    // 手机上锁屏后还能切歌,这才算"全局"。
    useEffect(() => {
        if (!('mediaSession' in navigator)) return;
        const ms = navigator.mediaSession;
        const set = (action: MediaSessionAction, handler: (() => void) | null) => {
            try {
                ms.setActionHandler(action, handler);
            } catch {
                // 老浏览器不认某些动作,忽略即可
            }
        };
        if (!current) {
            ms.metadata = null;
            ms.playbackState = 'none';
            set('play', null);
            set('pause', null);
            set('previoustrack', null);
            set('nexttrack', null);
            return;
        }
        ms.metadata = new MediaMetadata({ title: current.name, artist: 'PocketDrive' });
        ms.playbackState = playing ? 'playing' : 'paused';
        set('play', () => void audio.play().catch(() => {}));
        set('pause', () => audio.pause());
        set('previoustrack', prev);
        set('nexttrack', next);
    }, [audio, current, playing, prev, next]);

    const value = useMemo(
        () => ({
            queue,
            index,
            current,
            playing,
            time,
            duration,
            repeat,
            volume,
            error,
            playList,
            toggle,
            next,
            prev,
            seek,
            setVolume,
            cycleRepeat,
            close,
        }),
        [
            queue,
            index,
            current,
            playing,
            time,
            duration,
            repeat,
            volume,
            error,
            playList,
            toggle,
            next,
            prev,
            seek,
            setVolume,
            cycleRepeat,
            close,
        ],
    );
    return <PlayerCtx.Provider value={value}>{children}</PlayerCtx.Provider>;
}
