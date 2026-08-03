// 冒烟 v3(shadcn 皮肤版):登录→小岛主页→文件→md预览→笔记编辑→直链分享→
// 拖拽提示/回收站→展览馆→搜索→黑夜模式→截图/控制台错误
import { chromium } from 'playwright-core';
import fs from 'fs';

const BASE = 'http://127.0.0.1:8080';
const OUT = 'shots';
fs.mkdirSync(OUT, { recursive: true });

const browser = await chromium.launch({
    executablePath: 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
    headless: true,
});
const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
const page = await ctx.newPage();

const errors = [];
page.on('console', (m) => {
    if (m.type() === 'error') errors.push(m.text());
});
page.on('pageerror', (e) => errors.push(String(e)));

// 登录 → 小岛主页
await page.goto(BASE, { waitUntil: 'networkidle' });
await page.fill('input[placeholder="用户名"]', 'admin');
await page.fill('input[placeholder="密码"]', 'test123456');
await page.click('button:has-text("登录")');
await page.waitForSelector('text=PocketDrive 小岛', { timeout: 10000 });
await page.waitForTimeout(600);
await page.screenshot({ path: `${OUT}/20-home.png` });

// 文件页 + md 预览
await page.click('a:has-text("我的文件")');
await page.waitForSelector('text=note.md', { timeout: 10000 });
await page.screenshot({ path: `${OUT}/21-files.png` });
await page.click('button:has-text("note.md")');
await page.waitForSelector('text=item one', { timeout: 10000 });
await page.waitForTimeout(400);
await page.screenshot({ path: `${OUT}/22-md-preview.png` });

// 进入笔记编辑器
await page.click('button:has-text("✏️ 编辑")');
await page.waitForSelector('textarea', { timeout: 10000 });
await page.fill(
    'textarea',
    `# Hello Island\n\n改了一行 **加粗** ${Date.now()}\n\n- item one\n`,
);
await page.waitForSelector('text=未保存', { timeout: 5000 });
await page.click('button:has-text("保存")');
await page.waitForSelector('text=已保存', { timeout: 5000 });
await page.screenshot({ path: `${OUT}/23-editor.png` });

// 返回文件 → 直链分享
await page.click('button:has-text("返回文件")');
await page.waitForSelector('text=note.md', { timeout: 10000 });
await page.click('button:has-text("分享")');
await page.waitForSelector('select', { timeout: 5000 });
await page.selectOption('select >> nth=0', 'direct');
await page.click('button:has-text("生成链接")');
await page.waitForSelector('text=直链已生成', { timeout: 5000 });
const link = (await page.textContent('code')).trim();
await page.screenshot({ path: `${OUT}/24-direct-share.png` });
await page.click('button:has-text("完成")');

// 删除进回收站 → 还原(精确定位 note.md 所在行,别误删文件夹)
await page.click('xpath=//div[button[contains(., "note.md")]]//button[normalize-space()="删除"]');
await page.waitForSelector('text=放进垃圾桶?30 天内', { timeout: 5000 });
await page.click('button:has-text("放进垃圾桶")');
await page.waitForTimeout(600);
await page.click('a:has-text("垃圾桶")');
await page.waitForSelector('text=note.md', { timeout: 10000 });
await page.screenshot({ path: `${OUT}/25-trash.png` });
await page.click('button:has-text("还原")');
await page.waitForSelector('text=已还原', { timeout: 5000 });

// 展览馆
await page.click('a:has-text("照片")');
await page.waitForSelector('text=展览馆', { timeout: 10000 });
await page.waitForTimeout(600);
await page.screenshot({ path: `${OUT}/26-gallery.png` });

// 顶栏全局搜索
await page.fill('input[placeholder="搜索全岛文件…"]', 'note');
await page.waitForSelector('text=note.md', { timeout: 5000 });
await page.screenshot({ path: `${OUT}/27-search.png` });
await page.keyboard.press('Escape');

// 黑夜模式
await page.click('button[aria-label="切换主题"]');
await page.click('a:has-text("小岛")');
await page.waitForSelector('text=PocketDrive 小岛', { timeout: 5000 });
await page.waitForTimeout(500);
await page.screenshot({ path: `${OUT}/28-dark-home.png` });

// 直链免登录验证
const resp = await fetch(link);
const body = await resp.text();
if (!body.includes('Hello Island')) {
    errors.push('DIRECT LINK CONTENT MISMATCH: ' + body.slice(0, 80));
}

console.log('LINK:', link);
console.log('CONSOLE_ERRORS:', JSON.stringify(errors, null, 2));
await browser.close();
console.log('SMOKE-DONE');
