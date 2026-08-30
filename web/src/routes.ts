/**
 * 路由的懒加载入口,单独摆一份出来给侧栏做预取用。
 *
 * 点导航之所以会顿一下,一大半是首次访问那个路由时才开始下载它的 chunk:
 * 主区先塌成一个转圈,chunk 到了再画出来。所以让鼠标悬停(或聚焦)就把
 * chunk 拉下来——从悬停到真的点下去通常有两三百毫秒,足够到货了,点下去
 * 就是同步命中,一次转圈都不闪。
 *
 * import() 自带模块缓存:预取过之后 lazy() 再要同一个模块不会走第二次网络,
 * 重复悬停也不会重复下载,所以这里不需要自己记状态。
 *
 * 和 lib/listcache.ts 里"悬停文件夹就提前拉目录"是同一个路子。
 */

export const importFiles = () => import('./pages/Files');
export const importNoteEditor = () => import('./pages/NoteEditor');
export const importDownloads = () => import('./pages/Downloads');
export const importDownloadSettings = () => import('./pages/DownloadSettings');
export const importShares = () => import('./pages/SharesPage');
export const importTrash = () => import('./pages/Trash');
export const importSettings = () => import('./pages/Settings');
export const importStorage = () => import('./pages/StoragePage');
export const importLogin = () => import('./pages/Login');
export const importSharePage = () => import('./pages/SharePage');

/**
 * 包一层给事件处理器直接用:预取失败(离线、chunk 404)只是没预取成功,
 * 真正点进去时 lazy() 会自己再试一次并弹出正常的错误界面,所以这里必须
 * 把 rejection 吃掉,不然控制台里全是 unhandled rejection。
 */
export function prefetch(load: () => Promise<unknown>): () => void {
    return () => {
        void load().catch(() => undefined);
    };
}
