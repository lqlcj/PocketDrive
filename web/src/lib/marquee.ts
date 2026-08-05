import { useCallback, useEffect, useRef, useState } from 'react';

// 拉框选择(marquee / rubber band)。
//
// 在容器里按住左键拖出一个矩形,矩形碰到的项就被选中。容器里凡是带
// data-sel="名字" 的元素都参与命中判断——不想被框中的(比如根目录下的
// @挂载点)不加这个属性就行,不用在这里认识业务规则。
//
// 两个容易踩的地方:
//
// 1) 坐标一律用「文档坐标」(clientX + scrollX)。拖框时页面会跟着滚,
//    起点的视口坐标一直在变,只有文档坐标是稳的。
// 2) 拖到视口边缘要自动滚,而鼠标不动时 pointermove 不再触发,所以滚动
//    得挂在 rAF 循环里,并且每帧重新算一遍命中——页面滚过去以后,底下
//    那些原本没进框的行也该被框住。
//
// 选择语义不在这里:框中了哪些项通过 onSelect 交出去,怎么和已有选择合并
// 由调用方决定(PocketDrive 是纯追加,松手不取消任何原有选择)。

/** 移动够这么多像素才算在拉框,否则当成一次普通点击放行 */
const THRESHOLD = 4;
/** 指针进到视口上下这么近就开始自动滚 */
const EDGE = 80;
/** 自动滚动每帧最多滚多少像素 */
const MAX_SPEED = 18;

export interface MarqueeBox {
    left: number;
    top: number;
    width: number;
    height: number;
}

interface Gesture {
    /** 起点,文档坐标 */
    x0: number;
    y0: number;
    /** 当前点,文档坐标 */
    x: number;
    y: number;
    /** 当前点的视口 Y,判断要不要自动滚 */
    cy: number;
    /** 是否已经越过阈值、真的在拉框了 */
    active: boolean;
    pointerId: number;
}

function hitTest(el: HTMLElement, x0: number, y0: number, x1: number, y1: number): string[] {
    const sx = window.scrollX;
    const sy = window.scrollY;
    const out: string[] = [];
    el.querySelectorAll<HTMLElement>('[data-sel]').forEach((node) => {
        const r = node.getBoundingClientRect();
        const l = r.left + sx;
        const t = r.top + sy;
        // 相交即命中(不要求完全包住),扫过一行的边角也算选上
        if (l + r.width < x0 || l > x1 || t + r.height < y0 || t > y1) return;
        const name = node.dataset.sel;
        if (name) out.push(name);
    });
    return out;
}

export function useMarquee({
    enabled,
    containerRef,
    onStart,
    onSelect,
}: {
    enabled: boolean;
    /** 框选发生的容器:选框相对它定位,命中范围也限在它里面 */
    containerRef: React.RefObject<HTMLElement | null>;
    /** 越过阈值、真的开始拉框时调一次(调用方一般在这里存下基线) */
    onStart: () => void;
    /** 拖动期间反复调用,给出当前框住的所有 data-sel */
    onSelect: (names: string[]) => void;
}) {
    const [box, setBox] = useState<MarqueeBox | null>(null);
    const g = useRef<Gesture | null>(null);
    const raf = useRef(0);
    // 拉完框松手时浏览器还会补一发 click,得把它吃掉,否则等于点了松手位置那一项
    const suppress = useRef(false);
    // 回调塞进 ref:它们每次渲染都是新函数,不该因此重挂 window 监听
    const cb = useRef({ onStart, onSelect });
    cb.current = { onStart, onSelect };

    const apply = useCallback(() => {
        const cur = g.current;
        const el = containerRef.current;
        if (!cur || !cur.active || !el) return;
        const x0 = Math.min(cur.x0, cur.x);
        const x1 = Math.max(cur.x0, cur.x);
        const y0 = Math.min(cur.y0, cur.y);
        const y1 = Math.max(cur.y0, cur.y);
        const r = el.getBoundingClientRect();
        setBox({
            left: x0 - (r.left + window.scrollX),
            top: y0 - (r.top + window.scrollY),
            width: x1 - x0,
            height: y1 - y0,
        });
        cb.current.onSelect(hitTest(el, x0, y0, x1, y1));
    }, [containerRef]);

    const stop = useCallback(() => {
        if (raf.current) {
            cancelAnimationFrame(raf.current);
            raf.current = 0;
        }
        if (g.current?.active) {
            suppress.current = true;
            document.body.style.userSelect = '';
        }
        g.current = null;
        setBox((prev) => (prev === null ? prev : null));
    }, []);

    /** 自动滚边:每帧按离边缘的远近决定速度,滚完重算一次命中 */
    const tick = useCallback(() => {
        const cur = g.current;
        if (!cur || !cur.active) {
            raf.current = 0;
            return;
        }
        const h = window.innerHeight;
        let v = 0;
        if (cur.cy < EDGE) v = -Math.ceil(((EDGE - cur.cy) / EDGE) * MAX_SPEED);
        else if (cur.cy > h - EDGE) v = Math.ceil(((cur.cy - (h - EDGE)) / EDGE) * MAX_SPEED);
        if (v !== 0) {
            window.scrollBy(0, v);
            // 鼠标没动,但页面滚了,所以指针的文档坐标变了
            cur.y = cur.cy + window.scrollY;
            apply();
        }
        raf.current = requestAnimationFrame(tick);
    }, [apply]);

    useEffect(() => {
        const move = (ev: PointerEvent) => {
            const cur = g.current;
            if (!cur || ev.pointerId !== cur.pointerId) return;
            cur.cy = ev.clientY;
            cur.x = ev.clientX + window.scrollX;
            cur.y = ev.clientY + window.scrollY;
            if (!cur.active) {
                if (
                    Math.abs(cur.x - cur.x0) < THRESHOLD &&
                    Math.abs(cur.y - cur.y0) < THRESHOLD
                ) {
                    return;
                }
                cur.active = true;
                // 拖框时别顺手把页面上的文字也选蓝了
                document.body.style.userSelect = 'none';
                cb.current.onStart();
                raf.current = requestAnimationFrame(tick);
            }
            apply();
        };
        const up = (ev: PointerEvent) => {
            if (g.current && ev.pointerId !== g.current.pointerId) return;
            stop();
        };
        window.addEventListener('pointermove', move);
        window.addEventListener('pointerup', up);
        window.addEventListener('pointercancel', up);
        return () => {
            window.removeEventListener('pointermove', move);
            window.removeEventListener('pointerup', up);
            window.removeEventListener('pointercancel', up);
        };
    }, [apply, stop, tick]);

    // 退出选择模式(或组件卸载)时把进行中的手势收干净
    useEffect(() => {
        if (!enabled) stop();
        return stop;
    }, [enabled, stop]);

    const onPointerDown = useCallback(
        (ev: React.PointerEvent) => {
            // 上一次拉框如果松手在空白处没收到 click,标记会留下来,这里清掉
            suppress.current = false;
            if (!enabled) return;
            // 只接鼠标左键:右键要留给上下文菜单,触屏要留给页面滚动
            if (ev.button !== 0 || ev.pointerType !== 'mouse') return;
            const t = ev.target as HTMLElement | null;
            // 复选框、下载、⋯ 菜单自己有事要做,从它们身上起手不算拉框
            if (t?.closest('[data-noselect]')) return;
            const x = ev.clientX + window.scrollX;
            const y = ev.clientY + window.scrollY;
            g.current = {
                x0: x,
                y0: y,
                x,
                y,
                cy: ev.clientY,
                active: false,
                pointerId: ev.pointerId,
            };
        },
        [enabled],
    );

    const onClickCapture = useCallback((ev: React.MouseEvent) => {
        if (!suppress.current) return;
        suppress.current = false;
        ev.preventDefault();
        ev.stopPropagation();
    }, []);

    return { box, marqueeProps: { onPointerDown, onClickCapture } };
}
