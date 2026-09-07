import { useEffect } from 'react';

let guard: ((event: PopStateEvent) => void) | undefined;

// Imported by App before BrowserRouter mounts: window events at the target
// run in registration order, so the guard must precede the router listener.
window.addEventListener('popstate', (event) => guard?.(event), true);

export function useHistoryGuard(block: boolean, saving: boolean) {
    useEffect(() => {
        if (!block) return;
        const currentState = window.history.state;
        const currentUrl = window.location.href;
        let restoring = false;
        const handler = (event: PopStateEvent) => {
            if (restoring) {
                event.stopImmediatePropagation();
                restoring = false;
                return;
            }
            if (!saving && window.confirm('修改尚未保存，确定放弃吗？')) return;
            event.stopImmediatePropagation();
            const delta = currentState?.idx - event.state?.idx;
            if (Number.isFinite(delta) && delta !== 0) {
                restoring = true;
                window.history.go(delta);
            } else {
                window.history.pushState(currentState, '', currentUrl);
            }
        };
        guard = handler;
        return () => { if (guard === handler) guard = undefined; };
    }, [block, saving]);
}
