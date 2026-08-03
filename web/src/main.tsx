import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { Toaster } from 'sonner';
import '@fontsource/nunito/400.css';
import '@fontsource/nunito/700.css';
import '@fontsource/nunito/800.css';
import '@fontsource/noto-sans-sc/400.css';
import '@fontsource/noto-sans-sc/500.css';
import '@fontsource/noto-sans-sc/700.css';
import './index.css';
import App from './App';
import { initTheme } from './theme';

initTheme();

createRoot(document.getElementById('root')!).render(
    <StrictMode>
        <App />
        <Toaster position="top-center" richColors />
    </StrictMode>,
);
