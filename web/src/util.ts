export function formatBytes(n: number): string {
    if (!Number.isFinite(n) || n < 0) return '-';
    if (n < 1024) return `${n} B`;
    const units = ['KB', 'MB', 'GB', 'TB'];
    let v = n;
    let i = -1;
    do {
        v /= 1024;
        i++;
    } while (v >= 1024 && i < units.length - 1);
    return `${v.toFixed(v >= 100 ? 0 : 1)} ${units[i]}`;
}

export function formatSpeed(n: number): string {
    return n > 0 ? `${formatBytes(n)}/s` : '';
}

export function formatTime(ms: number | string): string {
    const d = new Date(ms);
    const pad = (x: number) => String(x).padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export type FileKind =
    | 'folder'
    | 'image'
    | 'video'
    | 'audio'
    | 'markdown'
    | 'text'
    | 'archive'
    | 'doc'
    | 'sheet'
    | 'slide'
    | 'pdf'
    | 'other';

const EXT_KIND: Record<string, FileKind> = {
    png: 'image', jpg: 'image', jpeg: 'image', gif: 'image', webp: 'image',
    svg: 'image', bmp: 'image', avif: 'image', ico: 'image',
    mp4: 'video', webm: 'video', mkv: 'video', mov: 'video', avi: 'video',
    m4v: 'video', ts: 'video', flv: 'video',
    mp3: 'audio', m4a: 'audio', flac: 'audio', wav: 'audio', ogg: 'audio',
    aac: 'audio', opus: 'audio', wma: 'audio',
    md: 'markdown', markdown: 'markdown',
    txt: 'text', log: 'text', json: 'text', yaml: 'text', yml: 'text',
    toml: 'text', ini: 'text', conf: 'text', sh: 'text', ps1: 'text',
    go: 'text', js: 'text', jsx: 'text', tsx: 'text', py: 'text', css: 'text',
    html: 'text', xml: 'text', csv: 'text', sql: 'text', c: 'text', h: 'text',
    cpp: 'text', rs: 'text', java: 'text',
    zip: 'archive', rar: 'archive', '7z': 'archive', gz: 'archive',
    tar: 'archive', xz: 'archive', bz2: 'archive', iso: 'archive',
    doc: 'doc', docx: 'doc',
    xls: 'sheet', xlsx: 'sheet',
    ppt: 'slide', pptx: 'slide',
    pdf: 'pdf',
};

export function fileKind(name: string, dir = false): FileKind {
    if (dir) return 'folder';
    const ext = name.includes('.') ? name.split('.').pop()!.toLowerCase() : '';
    // .ts 既是 TypeScript 又是视频流分片,按代码文本处理更常见
    if (ext === 'ts') return 'text';
    return EXT_KIND[ext] ?? 'other';
}

// 浏览器可直接播放的容器格式;其余视频只提供下载
export function browserPlayable(name: string): boolean {
    const ext = name.split('.').pop()?.toLowerCase() ?? '';
    return ['mp4', 'webm', 'm4v', 'mov', 'ogg'].includes(ext);
}

/**
 * 直链里文件名那一段的转义。
 *
 * encodeURIComponent 会把「我的壁纸.png」变成一长串 %E6%88%91…,难看
 * 也难认。name 段对后端毫无意义(token 才是凭证,name 只是让播放器/
 * 下载工具按后缀识别类型),所以这里只转义真正会破坏 URL 结构的字符,
 * 中文原样保留——和浏览器地址栏显示 URL 的做法一致。
 *
 *   % 必须先转,否则原本就带 % 的文件名会被当成转义序列
 *   # ? 会截断路径;空格在很多地方会被当成分隔符
 *   / 已经不可能出现(name 是最后一段),仍然转一道保险
 */
function encodeNameSegment(name: string): string {
    return name.replace(/[%#?/\s]/g, (ch) => encodeURIComponent(ch));
}

// 分享链接:直链在 token 后带上真实文件名段(/d/xxx/歌.mp3),
// 播放器/下载工具靠 URL 后缀识别类型;token 本身仍是唯一凭证
export function shareLink(s: { token: string; type: string; path: string }): string {
    if (s.type === 'direct') {
        const name = s.path.includes('/')
            ? s.path.slice(s.path.lastIndexOf('/') + 1)
            : s.path;
        return `${window.location.origin}/d/${s.token}/${encodeNameSegment(name)}`;
    }
    return `${window.location.origin}/s/${s.token}`;
}

// Office 在线预览支持的格式(纯浏览器端渲染,服务器只出文件流)
export function officePreviewable(name: string): boolean {
    const ext = name.split('.').pop()?.toLowerCase() ?? '';
    // 旧二进制格式里只有 .xls 能被 SheetJS 解析;.doc/.ppt 不支持
    return ['docx', 'xlsx', 'xls', 'pptx'].includes(ext);
}

/** 能不能在线解压。rar/7z 需要额外的二进制,没有做 */
export function extractable(name: string): boolean {
    const l = name.toLowerCase();
    return ['.zip', '.tar.gz', '.tgz', '.tar.xz', '.txz', '.tar'].some((s) =>
        l.endsWith(s),
    );
}

/**
 * 复制文本到剪贴板。
 *
 * navigator.clipboard 只在 secure context 里存在——用 http://IP:16688
 * 直接访问自己 VPS 时它是 undefined,直接用会抛 TypeError。这里退回到
 * 老的 execCommand('copy'),两条路都不通才报失败。
 *
 * 兜底那条路有个坑:临时 textarea 如果挂在 document.body 上,而当前
 * 正开着 Radix 的 Dialog,Dialog 的焦点陷阱会立刻把焦点抢回去、选区
 * 随之作废,execCommand 复制到的是空串。所以要把它挂进最上层那个
 * dialog 里。
 */
export async function copyText(text: string): Promise<boolean> {
    try {
        if (window.isSecureContext && navigator.clipboard) {
            await navigator.clipboard.writeText(text);
            return true;
        }
    } catch {
        // 落到下面的兜底
    }
    try {
        const dialogs = document.querySelectorAll<HTMLElement>('[role="dialog"]');
        const host = dialogs.length > 0 ? dialogs[dialogs.length - 1]! : document.body;
        const ta = document.createElement('textarea');
        ta.value = text;
        // 不能用 display:none,否则选不中
        ta.style.position = 'fixed';
        ta.style.top = '-9999px';
        ta.style.opacity = '0';
        ta.setAttribute('readonly', '');
        host.appendChild(ta);
        ta.focus();
        ta.select();
        // iOS Safari 不认 select(),要显式给范围
        ta.setSelectionRange(0, text.length);
        const ok = document.execCommand('copy');
        host.removeChild(ta);
        return ok;
    } catch {
        return false;
    }
}
