export type Theme = 'light' | 'dark';

const KEY = 'pd_theme';

export function getTheme(): Theme {
    return (localStorage.getItem(KEY) as Theme) || 'light';
}

export function applyTheme(t: Theme) {
    document.documentElement.classList.toggle('dark', t === 'dark');
    localStorage.setItem(KEY, t);
}

export function initTheme() {
    applyTheme(getTheme());
}
