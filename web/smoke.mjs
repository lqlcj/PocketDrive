// 冒烟 v4(Claude 配色版):登录→主页→文件(目录树/图标)→md预览→直链→
// 分享管理→下载设置→回收站→搜索→黑夜模式
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

// 登录 → 主页
await page.goto(BASE, { waitUntil: 'networkidle' });
await page.fill('input[placeholder="用户名"]', 'admin');
await page.fill('input[placeholder="密码"]', 'test123456');
await page.click('button:has-text("登录")');
await page.waitForSelector('text=仓库容量', { timeout: 10000 });
await page.waitForTimeout(600);
await page.screenshot({ path: `${OUT}/30-home.png` });

// 文件页:目录树 + 列表
await page.click('a:has-text("我的文件")');
await page.waitForSelector('text=note.md', { timeout: 10000 });
await page.waitForTimeout(500);
await page.screenshot({ path: `${OUT}/31-files-tree.png` });

// 目录树点击 music 联动
await page.click('button:has-text("music") >> nth=0');
await page.waitForSelector('text=rock', { timeout: 5000 });
await page.screenshot({ path: `${OUT}/32-tree-nav.png` });

// 给 rock 设置图标
await page.click('xpath=//div[button[contains(., "rock")]]//button[normalize-space()="图标"]');
await page.waitForSelector('text=「rock」的图标', { timeout: 5000 });
await page.click('button:has-text("🎮")');
await page.waitForTimeout(500);
await page.screenshot({ path: `${OUT}/33-folder-icon.png` });

// 回根目录 md 预览
await page.click('a:has-text("根目录") >> nth=0');
await page.waitForSelector('text=note.md', { timeout: 5000 });
await page.click('button:has-text("note.md")');
await page.waitForSelector('text=item one', { timeout: 10000 });
await page.keyboard.press('Escape');
await page.waitForTimeout(300);

// 直链分享
await page.click('xpath=//div[button[contains(., "note.md")]]//button[normalize-space()="分享"]');
await page.waitForSelector('select', { timeout: 5000 });
await page.selectOption('select >> nth=0', 'direct');
await page.click('button:has-text("生成链接")');
await page.waitForSelector('text=直链已生成', { timeout: 5000 });
const link = (await page.textContent('code')).trim();
await page.click('button:has-text("完成")');

// 分享管理页
await page.click('a:has-text("分享管理")');
await page.waitForSelector('text=直链', { timeout: 10000 });
await page.screenshot({ path: `${OUT}/34-shares.png` });

// 下载设置页
await page.click('a:has-text("离线下载")');
await page.click('a:has-text("下载设置")');
await page.waitForSelector('text=最大同时下载数', { timeout: 10000 });
await page.waitForTimeout(400);
await page.screenshot({ path: `${OUT}/35-dl-settings.png` });

// 搜索
await page.fill('input[placeholder="搜索全岛文件…"]', 'note');
await page.waitForSelector('text=note.md', { timeout: 5000 });
await page.keyboard.press('Escape');

// 黑夜模式
await page.click('button[aria-label="切换主题"]');
await page.click('a:has-text("主页")');
await page.waitForSelector('text=仓库容量', { timeout: 5000 });
await page.waitForTimeout(500);
await page.screenshot({ path: `${OUT}/36-dark-home.png` });
await page.click('a:has-text("我的文件")');
await page.waitForSelector('text=note.md', { timeout: 5000 });
await page.waitForTimeout(500);
await page.screenshot({ path: `${OUT}/37-dark-files.png` });

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
