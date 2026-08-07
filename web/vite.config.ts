import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// host 必须显式 127.0.0.1:Windows 下 Vite 默认绑 IPv6 ::1,原生 curl 连不上
export default defineConfig({
    plugins: [react(), tailwindcss()],
    server: {
        host: '127.0.0.1',
        port: 5173,
        proxy: {
            '/api': 'http://127.0.0.1:16688',
            '/dav': 'http://127.0.0.1:16688',
            '/d': 'http://127.0.0.1:16688',
        },
    },
    build: {
        outDir: 'dist',
        emptyOutDir: true,
        rollupOptions: {
            output: {
                manualChunks(id) {
                    if (id.includes('node_modules/react-router')) return 'vendor-router';
                    if (id.includes('node_modules/lucide-react')) return 'vendor-icons';
                    if (id.includes('node_modules/react-markdown') || id.includes('node_modules/remark-gfm')) return 'vendor-markdown';
                    if (id.includes('node_modules/@radix-ui')) return 'vendor-radix';
                    if (id.includes('node_modules/react-dom') || id.includes('node_modules/react')) return 'vendor-react';
                    if (id.includes('node_modules/@fontsource')) return 'vendor-fonts';
                },
            },
        },
    },
});
