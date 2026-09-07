import assert from 'node:assert/strict';
import { chromium } from 'playwright-core';

// Run against Vite. All fixtures stay in the browser; no server or user data is needed.
const base = process.env.PD_TEST_URL ?? 'http://127.0.0.1:5174';
const browser = await chromium.launch({
    channel: process.env.PD_TEST_BROWSER ?? 'msedge',
    headless: true,
});
try {
    for (const dark of [false, true]) {
        for (const viewport of [{ width: 1280, height: 800 }, { width: 390, height: 844 }]) {
            const page = await browser.newPage({ viewport });
            const errors = [];
            page.on('pageerror', (error) => { errors.push(error.message); console.error(error.message); });
            await page.route('**/image-preview-test', (route) => route.fulfill({
                contentType: 'text/html',
                body: `<html class="${dark ? 'dark' : ''}"><body><div id="root"></div>
                    <script type="module">
                    import RefreshRuntime from '/@react-refresh';
                    RefreshRuntime.injectIntoGlobalHook(window);
                    window.$RefreshReg$ = () => {};
                    window.$RefreshSig$ = () => (type) => type;
                    window.__vite_plugin_react_preamble_installed__ = true;
                    const { default: React } = await import('/node_modules/.vite/deps/react.js');
                    const { default: { createRoot } } = await import('/node_modules/.vite/deps/react-dom_client.js');
                    const { default: ImagePreview } = await import('/src/components/ImagePreview.tsx');
                    await import('/src/index.css');
                    const canvas = document.createElement('canvas');
                    canvas.width = 240; canvas.height = 800;
                    const ctx = canvas.getContext('2d');
                    ctx.fillStyle = '#27a77f'; ctx.fillRect(0, 0, 240, 800);
                    window.fixture = canvas.toDataURL();
                    const entries = ['portrait.png', 'second.png', 'broken.png'].map(name => ({ name, dir: false, size: 100, mtime: 0 }));
                    function App() {
                        const [index, setIndex] = React.useState(-1);
                        return index < 0
                            ? React.createElement('button', { onClick: () => setIndex(0) }, 'Open')
                            : React.createElement(ImagePreview, { entries, index, dirPath: '', onNavigate: setIndex, onClose: () => setIndex(-1) });
                    }
                    createRoot(document.getElementById('root')).render(React.createElement(React.StrictMode, null, React.createElement(App)));
                    </script></body></html>`,
            }));
            await page.route('**/api/**', async (route) => {
                if (route.request().url().includes('broken.png')) return route.fulfill({ status: 404 });
                await new Promise((resolve) => setTimeout(resolve, 250));
                const data = await page.evaluate(() => window.fixture.split(',')[1]);
                await route.fulfill({ contentType: 'image/png', body: Buffer.from(data, 'base64') });
            });
            await page.goto(`${base}/image-preview-test`);
            await page.getByRole('button', { name: 'Open' }).waitFor();
            for (let repeat = 0; repeat < 2; repeat++) {
                await page.evaluate(() => {
                    window.samples = [];
                    window.firstImage = null;
                    window.replaced = false;
                    window.sampling = true;
                    const sample = () => {
                        const overlay = document.querySelector('[aria-label="图片预览"]');
                        const viewer = document.querySelector('.pswp');
                        const bg = document.querySelector('.pswp__bg');
                        if (overlay || viewer) {
                            window.samples.push(overlay ? 0.95 : Number(getComputedStyle(viewer).opacity) * Number(getComputedStyle(bg).opacity));
                        }
                        const image = document.querySelector('.pswp__item[aria-hidden="false"] img.pswp__img');
                        if (image) {
                            if (window.firstImage && window.firstImage !== image) window.replaced = true;
                            window.firstImage ??= image;
                        }
                        if (window.sampling) requestAnimationFrame(sample);
                    };
                    requestAnimationFrame(sample);
                });
                await page.getByRole('button', { name: 'Open' }).click();
                await page.waitForFunction(() => {
                    const img = document.querySelector('.pswp__item[aria-hidden="false"] img.pswp__img');
                    return img?.naturalWidth === 240 && img.getBoundingClientRect().height > 0;
                });
                await page.waitForTimeout(400);
                const result = await page.evaluate(() => {
                    window.sampling = false;
                    const rect = window.firstImage.getBoundingClientRect();
                    return { samples: window.samples, replaced: window.replaced, ratio: rect.width / rect.height };
                });
                assert(result.samples.length > 0);
                assert(result.samples.every((opacity) => opacity >= 0.949), `Backdrop flashed: ${result.samples}`);
                assert.equal(result.replaced, false, 'Loaded image was replaced');
                assert(Math.abs(result.ratio - 0.3) < 0.005, `Incorrect portrait ratio: ${result.ratio}; ${await page.locator('.pswp__img').evaluateAll(nodes => nodes.map(node => node.outerHTML))}`);
                await page.screenshot({ path: `${process.env.TEMP ?? '/tmp'}/pd-image-${dark ? 'dark' : 'light'}-${viewport.width}.png` });
                await page.keyboard.press('ArrowRight');
                await page.waitForFunction(() => document.querySelector('.pswp__filename')?.textContent === 'second.png');
                await page.keyboard.press('ArrowRight');
                await page.locator('.pswp__error-msg').waitFor();
                await page.keyboard.press('Escape');
                await page.getByRole('button', { name: 'Open' }).waitFor();
                assert.equal(await page.locator('.pswp').count(), 0);
                assert.equal(await page.evaluate(() => document.body.style.overflow), '');
            }
            assert.deepEqual(errors, []);
            console.log(`PASS ${dark ? 'dark' : 'light'} ${viewport.width}: backdrop, image identity, dimensions, navigation, errors, reopen, cleanup`);
            await page.close();
        }
    }
} finally {
    await browser.close();
}
