import { api } from '../api';
import type { FileEntry } from '../api';

/**
 * 目录列表的内存缓存。
 *
 * 之前每次点目录都是「清空 → 转圈 → 等一个往返」,而左边的目录树还会
 * 跟着把根目录和所有已展开节点重新拉一遍,进三层目录就是 5 个往返。
 * 服务端那边只是一次 ReadDir,慢的全是网络。
 *
 * 所以这里做三件事:
 *   1. 同一个路径的结果缓存下来,再进去先拿旧数据直接画,后台再刷新
 *      (stale-while-revalidate),看不到转圈;
 *   2. 同一路径的并发请求合并成一个;
 *   3. 鼠标悬停在文件夹上时提前拉——等真的点下去,数据常常已经到了。
 *
 * 缓存只活在内存里,刷新页面就没了;任何写操作(新建/改名/删除/移动/
 * 上传完成)调 invalidate() 全清,宁可多拉一次也不能让用户看到脏数据。
 */

interface Entry {
    entries: FileEntry[];
    at: number;
}

const cache = new Map<string, Entry>();
const inflight = new Map<string, Promise<FileEntry[]>>();
const subs = new Set<() => void>();

/** 超过这个岁数的缓存只用于「先画上」,一定会再发一次请求 */
const FRESH_MS = 5000;

function emit() {
    for (const fn of subs) fn();
}

export function subscribe(fn: () => void): () => void {
    subs.add(fn);
    return () => subs.delete(fn);
}

/** 取缓存,没有就返回 undefined。不发请求。 */
export function peekList(path: string): FileEntry[] | undefined {
    return cache.get(path)?.entries;
}

/**
 * 拉目录列表。
 * @param force 无视缓存年龄,一定发请求(写操作之后用)
 */
export function fetchList(path: string, force = false): Promise<FileEntry[]> {
    const hit = cache.get(path);
    if (!force && hit && Date.now() - hit.at < FRESH_MS) {
        return Promise.resolve(hit.entries);
    }
    const running = inflight.get(path);
    if (running) return running;

    const p = api
        .listFiles(path)
        .then((r) => {
            cache.set(path, { entries: r.entries, at: Date.now() });
            emit();
            return r.entries;
        })
        .finally(() => {
            inflight.delete(path);
        });
    inflight.set(path, p);
    return p;
}

/**
 * 预取:鼠标悬停时调。已经有比较新的缓存就什么也不做,失败也不吭声——
 * 这只是个优化,不能因为预取失败弹错误提示。
 */
export function prefetchList(path: string): void {
    const hit = cache.get(path);
    if (hit && Date.now() - hit.at < FRESH_MS) return;
    if (inflight.has(path)) return;
    void fetchList(path).catch(() => undefined);
}

/** 清缓存。不传参数 = 全清(任何写操作之后都这么用)。 */
export function invalidateList(path?: string): void {
    if (path === undefined) cache.clear();
    else cache.delete(path);
}
