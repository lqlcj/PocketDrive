// 冒烟 v6(v0.5):动画登录页→文件页(主页)→目录树/图标(⋯ 菜单)→md 预览→
// xlsx 预览→压缩/解压→直链→分享管理→离线下载(种子上传入口)→下载设置→
// yt(播放列表开关)→储存策略(最近修改/WebDAV)→设置(资料/容量/组件状态/备份迁移)→搜索→
// 黑夜模式→直链免登录验证
//
// 注意:会往 ../data 写测试文件(note.md / test.xlsx / music/rock),
// 跑之前确认那不是你正在用的数据目录。
import { chromium } from 'playwright-core';
import fs from 'fs';
import path from 'path';
import * as XLSX from 'xlsx';

const BASE = 'http://127.0.0.1:16688';
const OUT = 'shots';
const DATA = path.resolve('..', 'data');
fs.mkdirSync(OUT, { recursive: true });

// ---- 测试数据:note.md + music/rock 目录 + test.xlsx ----
fs.mkdirSync(path.join(DATA, 'music', 'rock'), { recursive: true });
fs.writeFileSync(
    path.join(DATA, 'note.md'),
    '# Hello Island\n\n- item one\n- item two\n',
);
const wb = XLSX.utils.book_new();
XLSX.utils.book_append_sheet(
    wb,
    XLSX.utils.aoa_to_sheet([
        ['名称', '数量'],
        ['椰子', 12],
        ['浆果', 34],
    ]),
    '库存',
);
XLSX.writeFile(wb, path.join(DATA, 'test.xlsx'));

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

// 动画登录页
await page.goto(BASE, { waitUntil: 'networkidle' });
await page.waitForSelector('#pd-user', { timeout: 10000 });
await page.waitForTimeout(600);
await page.screenshot({ path: `${OUT}/40-login.png` });
await page.fill('#pd-user', 'admin');
await page.fill('#pd-pass', 'test123456');
await page.click('button:has-text("登录")');

// 登录后直达文件页(主页)
await page.waitForSelector('text=note.md', { timeout: 10000 });
await page.waitForTimeout(500);
await page.screenshot({ path: `${OUT}/41-files-home.png` });

// 目录树点击 music 联动
await page.click('button:has-text("music") >> nth=0');
await page.waitForSelector('text=rock', { timeout: 5000 });

// 给 rock 设置 emoji 图标(行操作已收进「⋯」菜单)
await page.click('xpath=//div[button[contains(., "rock")]]//button[@aria-label="更多操作"]');
await page.click('text=设置图标');
await page.waitForSelector('text=「rock」的图标', { timeout: 5000 });
await page.click('button:has-text("🎮")');
await page.waitForTimeout(500);
await page.screenshot({ path: `${OUT}/42-folder-icon.png` });

// 回根目录,md 预览
await page.click('a:has-text("根目录") >> nth=0');
await page.waitForSelector('text=note.md', { timeout: 5000 });
await page.click('button:has-text("note.md")');
await page.waitForSelector('text=item one', { timeout: 10000 });
await page.keyboard.press('Escape');
await page.waitForTimeout(300);

// xlsx 在线预览(懒加载 SheetJS 分包)
await page.click('button:has-text("test.xlsx")');
await page.waitForSelector('.office-sheet table', { timeout: 20000 });
await page.waitForTimeout(400);
await page.screenshot({ path: `${OUT}/43-xlsx-preview.png` });
await page.keyboard.press('Escape');
await page.waitForTimeout(300);

// 压缩 music 目录 → 等异步任务完成 → 再解压回来
await page.click('xpath=//div[button[contains(., "music")]]//button[@aria-label="更多操作"]');
await page.click('text=压缩为…');
await page.waitForSelector('text=压缩包名称', { timeout: 5000 });
await page.click('button:has-text("开始压缩")');
// 任务是后台跑的,完成后列表里会出现压缩包
await page.waitForSelector('text=music.zip', { timeout: 30000 });
await page.screenshot({ path: `${OUT}/44-compressed.png` });

await page.click('xpath=//div[button[contains(., "music.zip")]]//button[@aria-label="更多操作"]');
await page.click('text=解压到此处');
await page.waitForSelector('button:has-text("开始解压")', { timeout: 5000 });
await page.click('button:has-text("开始解压")');
await page.waitForTimeout(4000); // 解压完会刷新列表,内容覆盖回原处
await page.waitForTimeout(300);

// 直链分享(行操作已收进「⋯」菜单)
await page.click('xpath=//div[button[contains(., "note.md")]]//button[@aria-label="更多操作"]');
await page.click('text=分享');
await page.waitForSelector('select', { timeout: 5000 });
await page.selectOption('select >> nth=0', 'direct');
await page.click('button:has-text("生成链接")');
await page.waitForSelector('text=直链已生成', { timeout: 5000 });
const link = (await page.textContent('code')).trim();
await page.click('button:has-text("完成")');

// 分享管理页
await page.click('a:has-text("分享管理")');
await page.waitForSelector('text=直链', { timeout: 10000 });

// 离线下载页:种子上传入口
await page.click('a:has-text("离线下载")');
await page.waitForSelector('button:has-text("上传种子")', { timeout: 10000 });
await page.waitForTimeout(400);
await page.screenshot({ path: `${OUT}/44-downloads.png` });

// 下载设置页
await page.click('a:has-text("下载设置")');
await page.waitForSelector('text=最大同时下载数', { timeout: 10000 });

// yt下载页:播放列表开关
await page.click('a:has-text("yt下载")');
await page.waitForSelector('text=整个播放列表批量下载', { timeout: 10000 });
await page.waitForTimeout(400);
await page.screenshot({ path: `${OUT}/45-ytdl.png` });

// 储存策略页:最近修改 + 存储策略 + WebDAV
await page.click('a:has-text("储存策略") >> nth=0');
await page.waitForSelector('text=最近修改', { timeout: 10000 });
await page.waitForSelector('text=WebDAV', { timeout: 5000 });

// 设置页:紧凑资料卡 + 容量 + 组件状态 + 备份迁移
await page.click('a:has-text("设置") >> nth=0');
await page.waitForSelector('text=个人资料', { timeout: 10000 });
await page.waitForSelector('text=修改密码', { timeout: 5000 });
await page.waitForSelector('text=仓库容量', { timeout: 5000 });
await page.waitForSelector('text=组件状态', { timeout: 5000 });
await page.waitForSelector('text=备份与迁移', { timeout: 5000 });
await page.waitForTimeout(500);
await page.screenshot({ path: `${OUT}/46-settings.png` });

// 全局搜索
await page.fill('input[placeholder="搜索全盘文件…"]', 'note');
await page.waitForSelector('text=note.md', { timeout: 5000 });
await page.keyboard.press('Escape');

// 黑夜模式
await page.click('button[aria-label="切换主题"]');
await page.waitForTimeout(600);
await page.click('a:has-text("我的文件")');
await page.waitForSelector('text=note.md', { timeout: 5000 });
await page.waitForTimeout(500);
await page.screenshot({ path: `${OUT}/47-dark-files.png` });

// 退出后检查动画登录页在暗色下也正常
await page.click('button:has-text("退出")');
await page.waitForSelector('#pd-user', { timeout: 10000 });
await page.waitForTimeout(600);
await page.screenshot({ path: `${OUT}/48-dark-login.png` });

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
