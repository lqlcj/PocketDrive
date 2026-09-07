import assert from 'node:assert/strict';
import { chromium } from 'playwright-core';

// Uses mocked responses so no account or runtime files are changed.
const browser = await chromium.launch({
    executablePath: process.env.CHROME_PATH || 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
    headless: true,
});
try {
    const page = await browser.newPage();
    const errors = [];
    page.on('pageerror', (error) => errors.push(error.message));
    await page.route('**/api/**', async (route) => {
        const url = new URL(route.request().url());
        if (url.pathname.endsWith('/content')) {
            return route.fulfill({ contentType: 'text/plain', body: 'Preview history fixture' });
        }
        if (url.pathname.endsWith('/download') || url.pathname.endsWith('/thumb')) {
            return route.fulfill({ contentType: 'image/png', body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=', 'base64') });
        }
        const body = url.pathname.endsWith('/auth/me') ? { user: 'test', avatar: '' }
            : url.pathname.endsWith('/files') ? { entries: ['sample.txt', 'image.png', 'other.bin'].map((name) => ({ name, dir: false, size: 20, mtime: 0 })) }
            : { icons: {}, stats: {}, tasks: [], policies: [], items: [] };
        await route.fulfill({ json: body });
    });
    const base = process.env.PREVIEW_TEST_URL || 'http://127.0.0.1:5173';
    await page.goto(`${base}/files`);
    await page.getByRole('button', { name: 'sample.txt', exact: true }).waitFor();
    // A previous menu must remain behind the file list in browser history.
    await page.evaluate(() => {
        history.replaceState(history.state, '', '/settings');
        history.pushState({ ...history.state, idx: history.state.idx + 1 }, '', '/files');
        dispatchEvent(new PopStateEvent('popstate', { state: history.state }));
    });
    for (const name of ['sample.txt', 'image.png', 'other.bin']) {
        await page.getByRole('button', { name, exact: true }).click();
        await page.waitForFunction(() => !!history.state.usr?.preview);
        await page.goBack();
        await page.waitForFunction(() => !history.state.usr?.preview);
        assert.equal(new URL(page.url()).pathname, '/files');
        await page.locator('[role="dialog"], .pswp').waitFor({ state: 'hidden' });
    }
    await page.getByRole('button', { name: 'sample.txt', exact: true }).click();
    await page.getByRole('button', { name: '编辑', exact: true }).click();
    await page.getByRole('textbox').fill('Unsaved draft');
    page.once('dialog', (dialog) => dialog.dismiss());
    await page.evaluate(() => history.back());
    await page.waitForTimeout(500);
    await page.waitForFunction(() => history.state.usr?.preview === 'sample.txt');
    assert.equal(await page.getByRole('textbox').inputValue(), 'Unsaved draft');
    page.once('dialog', (dialog) => dialog.accept());
    await page.goBack();
    await page.locator('[role="dialog"]').waitFor({ state: 'hidden' });
    await page.getByRole('button', { name: 'sample.txt', exact: true }).click();
    await page.getByRole('button', { name: '编辑', exact: true }).click();
    await page.getByRole('textbox').fill('Another draft');
    let confirmations = 0;
    const accept = async (dialog) => { confirmations++; await dialog.accept(); };
    page.on('dialog', accept);
    await page.getByRole('button', { name: '关闭', exact: true }).click();
    await page.locator('[role="dialog"]').waitFor({ state: 'hidden' });
    page.off('dialog', accept);
    assert.equal(confirmations, 1);
    await page.getByRole('button', { name: 'other.bin', exact: true }).click();
    await page.getByRole('dialog').waitFor();
    await page.keyboard.press('Escape');
    await page.waitForFunction(() => !history.state.usr?.preview);
    await page.goBack();
    assert.equal(new URL(page.url()).pathname, '/settings');
    assert.deepEqual(errors, []);
    console.log('Preview history checks passed.');
} finally {
    await browser.close();
}
