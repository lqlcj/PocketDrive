import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { Toaster } from 'sonner';
import '@fontsource/nunito/400.css';
import '@fontsource/nunito/700.css';
import '@fontsource/nunito/800.css';
import '@fontsource/noto-sans-sc/400.css';
import '@fontsource/noto-sans-sc/700.css';
import './index.css';
import App from './App';
import ErrorBoundary from './components/ErrorBoundary';
import { initTheme } from './theme';

initTheme();

createRoot(document.getElementById('root')!).render(
    <StrictMode>
        {/* 最后一道:路由/鉴权这层挂了也别整页白掉。Toaster 放外面,崩了还能弹提示 */}
        <ErrorBoundary label="应用" fullscreen>
            <App />
        </ErrorBoundary>
        <Toaster position="top-center" richColors />
    </StrictMode>,
);
