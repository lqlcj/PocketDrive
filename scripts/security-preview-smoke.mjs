// Run after npm --prefix web ci. Uses installed Edge on Windows, or BROWSER_PATH.
// Requests are mocked; fixtures and screenshots never touch the drive data.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { mkdir } from 'node:fs/promises';
import { fileURLToPath, pathToFileURL } from 'node:url';
import path from 'node:path';

const root = fileURLToPath(new URL('../', import.meta.url));
const require = createRequire(new URL('../web/package.json', import.meta.url));
const { chromium } = require('playwright-core');
const JSZip = require('jszip');
const { createServer } = await import(pathToFileURL(require.resolve('vite')).href);
const zip = new JSZip();
zip.file('[Content_Types].xml', `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="html" ContentType="text/html"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`);
zip.file('_rels/.rels', `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="doc" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`);
zip.file('word/document.xml', `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body><w:p><w:r><w:t>Security preview fixture</w:t></w:r></w:p><w:p><w:hyperlink r:id="link"><w:r><w:t>Unsafe document link</w:t></w:r></w:hyperlink></w:p><w:altChunk r:id="chunk"/><w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="720" w:bottom="720" w:left="720" w:right="720"/></w:sectPr></w:body></w:document>`);
zip.file('word/_rels/document.xml.rels', `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="chunk" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/aFChunk" Target="chunk.html"/><Relationship Id="link" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" TargetMode="External" Target="javascript:parent.__docxExecuted=true"/></Relationships>`);
zip.file('word/chunk.html', `<html><body><script>parent.__docxExecuted=true;fetch('/__security_probe__')</script></body></html>`);
const docx = await zip.generateAsync({ type: 'nodebuffer' });
const shots = path.join(root, 'web/shots/security');
await mkdir(shots, { recursive: true });
const server = await createServer({ root: path.join(root, 'web'), configFile: path.join(root, 'web/vite.config.ts'), server: { host: '127.0.0.1', port: 0 }, logLevel: 'error' });
let browser;
try {
    await server.listen();
    const base = `http://127.0.0.1:${server.httpServer.address().port}`;
    browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_PATH ? { executablePath: process.env.BROWSER_PATH } : { channel: 'msedge' }) });
    for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
        const context = await browser.newContext({ viewport });
        const page = await context.newPage();
        const errors = [];
        page.on('pageerror', (e) => errors.push(e.message));
        let probes = 0;
        await page.route('**/__security_probe__', (route) => { probes++; return route.fulfill({ body: 'probe' }); });
        await page.route('**/api/v1/**', (route) => {
            const p = new URL(route.request().url()).pathname;
            if (p.endsWith('/download')) return route.fulfill({ contentType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', body: docx });
            if (p.endsWith('/public/share/security-docx')) return route.fulfill({ json: { type: 'file', name: 'security.docx', size: docx.length, mtime: 0, needPassword: false } });
            if (p.endsWith('/auth/me')) return route.fulfill({ status: 401, json: { error: 'mock anonymous session' } });
            return route.fulfill({ json: {} });
        });
        await page.goto(`${base}/s/security-docx`);
        await page.getByRole('button', { name: /\u9884\u89c8\u6587\u4ef6/ }).click();
        const frameElement = page.locator('iframe[title="security.docx"]');
        const frame = page.frameLocator('iframe[title="security.docx"]');
        try {
            await frame.getByText('Security preview fixture').waitFor({ timeout: 10000 });
        } catch (error) {
            console.error(await page.locator('body').innerText());
            console.error('Page errors:', errors);
            console.error(await frameElement.evaluateAll((frames) => frames.map((f) => ({ html: f.contentDocument?.documentElement.outerHTML, rect: f.getBoundingClientRect().toJSON() }))));
            await page.screenshot({ path: path.join(shots, 'failure.png'), fullPage: true });
            throw error;
        }
        assert.equal(await frameElement.getAttribute('sandbox'), 'allow-same-origin');
        assert.equal(await frame.locator('iframe').count(), 0, 'altChunk must be disabled');
        assert.equal(await frame.locator('a[href]').count(), 0, 'document hyperlinks must be inert');
        // Inject an active element to verify the sandbox itself, independently
        // of the altChunk option and link sanitization.
        await frameElement.evaluate((element) => {
            const script = element.contentDocument.createElement('script');
            script.textContent = "parent.__docxExecuted=true;fetch('/__security_probe__')";
            element.contentDocument.body.appendChild(script);
        });
        await page.waitForTimeout(300);
        assert.equal(await page.evaluate(() => Boolean(window.__docxExecuted)), false);
        assert.equal(probes, 0, 'preview must not execute network requests');
        const bounds = await frameElement.boundingBox();
        assert.ok(bounds.width > 100 && bounds.x >= 0 && bounds.x + bounds.width <= viewport.width + 1, JSON.stringify(bounds));
        assert.equal(errors.length, 0, errors.join('\n'));
        await page.screenshot({ path: path.join(shots, `docx-${viewport.width}.png`), fullPage: true });
        // Positive control: the same fixture must execute under the dependency's
        // old defaults, otherwise the negative test could pass on an inert DOCX.
        await page.evaluate(async (base64) => {
            const { renderAsync } = await import('/node_modules/.vite/deps/docx-preview.js');
            const container = document.createElement('div');
            document.body.appendChild(container);
            const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
            await renderAsync(bytes, container);
        }, docx.toString('base64'));
        await page.waitForFunction(() => window.__docxExecuted === true);
        console.log(`PASS ${viewport.width}x${viewport.height}: DOCX visible; scripts, altChunk and unsafe links blocked; vulnerable control reproduced`);
        await context.close();
    }
} finally {
    await browser?.close();
    await server.close();
}
