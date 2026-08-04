export type Theme = 'light' | 'dark';

const KEY = 'pd_theme';
let fadeTimer: number | undefined;

export function getTheme(): Theme {
    return (localStorage.getItem(KEY) as Theme) || 'light';
}

export function applyTheme(t: Theme, animate = false) {
    const root = document.documentElement;
    if (animate) {
        // 切换瞬间加过渡类,收尾后移除,避免常驻 transition 拖慢交互
        root.classList.add('theme-fade');
        window.clearTimeout(fadeTimer);
        fadeTimer = window.setTimeout(() => root.classList.remove('theme-fade'), 420);
    }
    root.classList.toggle('dark', t === 'dark');
    localStorage.setItem(KEY, t);
}

export function initTheme() {
    applyTheme(getTheme());
}
