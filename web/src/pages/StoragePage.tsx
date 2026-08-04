import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { ArrowLeft, Cloud, HardDrive, Plug, Plus } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../api';
import type { StoragePolicy, StoragePolicyInput } from '../api';
import { Card } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Badge } from '../components/ui/progress';
import { Dialog, DialogContent } from '../components/ui/dialog';

const EMPTY: StoragePolicyInput = {
    name: '',
    endpoint: '',
    region: '',
    bucket: '',
    accessKey: '',
    secretKey: '',
    basePath: '',
};

/**
 * 存储策略:把 S3 兼容对象存储(Cloudflare R2 / AWS S3 / MinIO…)
 * 挂载为网盘根目录下的 @名称 文件夹。上传由服务端中转、下载 302 到
 * 预签名地址,浏览器全程只和本站通信——桶上不需要配置任何 CORS。
 */
export default function StoragePage() {
    const [policies, setPolicies] = useState<StoragePolicy[]>([]);
    const [loading, setLoading] = useState(true);
    const [editOpen, setEditOpen] = useState(false);
    const [form, setForm] = useState<StoragePolicyInput>(EMPTY);
    const [editingId, setEditingId] = useState<number | undefined>();
    const [testing, setTesting] = useState(false);
    const [saving, setSaving] = useState(false);
    const [deleteTarget, setDeleteTarget] = useState<StoragePolicy | null>(null);

    const load = useCallback(() => {
        api.storages()
            .then((r) => setPolicies(r.policies))
            .catch((e) => toast.error(e instanceof Error ? e.message : '加载失败'))
            .finally(() => setLoading(false));
    }, []);

    useEffect(load, [load]);

    const openAdd = () => {
        setForm(EMPTY);
        setEditingId(undefined);
        setEditOpen(true);
    };

    const openEdit = (p: StoragePolicy) => {
        setForm({
            name: p.name,
            endpoint: p.endpoint,
            region: p.region,
            bucket: p.bucket,
            accessKey: p.accessKey,
            secretKey: '',
            basePath: p.basePath,
        });
        setEditingId(p.id);
        setEditOpen(true);
    };

    const test = async () => {
        setTesting(true);
        try {
            await api.testStorage({ ...form, id: editingId });
            toast.success('连接成功,桶可正常访问');
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '连接失败');
        } finally {
            setTesting(false);
        }
    };

    const save = async () => {
        setSaving(true);
        try {
            await api.saveStorage({ ...form, id: editingId });
            toast.success(editingId ? '已更新,挂载已刷新' : `已挂载为 @${form.name}`);
            setEditOpen(false);
            load();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '保存失败');
        } finally {
            setSaving(false);
        }
    };

    const doDelete = async () => {
        if (!deleteTarget) return;
        try {
            await api.deleteStorage(deleteTarget.id);
            toast.success('已删除挂载(桶里的文件不受影响)');
            setDeleteTarget(null);
            load();
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '删除失败');
        }
    };

    const field = (
        label: string,
        key: keyof StoragePolicyInput,
        placeholder: string,
        type = 'text',
    ) => (
        <div>
            <label className="block text-xs font-bold text-ink-soft mb-1">{label}</label>
            <Input
                type={type}
                value={String(form[key] ?? '')}
                placeholder={placeholder}
                autoComplete="off"
                onChange={(e) => setForm({ ...form, [key]: e.target.value })}
            />
        </div>
    );

    return (
        <div>
            <div className="flex items-center gap-3 mb-4 flex-wrap">
                <h2 className="text-xl font-extrabold flex items-center gap-2">
                    <Cloud className="size-5 text-leaf-dark" /> 存储策略
                </h2>
                <div className="ml-auto flex gap-2">
                    <Link to="/settings">
                        <Button size="sm">
                            <ArrowLeft className="size-3.5" /> 返回设置
                        </Button>
                    </Link>
                    <Button variant="primary" size="sm" onClick={openAdd}>
                        <Plus className="size-3.5" /> 添加 S3/R2 存储
                    </Button>
                </div>
            </div>

            <Card className="mb-4">
                <div className="flex items-center gap-3 flex-wrap">
                    <HardDrive className="size-5 text-leaf-dark shrink-0" />
                    <div className="flex-1 min-w-0">
                        <div className="font-bold text-sm">本机存储(默认)</div>
                        <div className="text-xs text-ink-soft">
                            VPS 磁盘,支持全部功能:回收站 / 缩略图 / 搜索 / 离线下载落盘
                        </div>
                    </div>
                    <Badge tone="green">始终启用</Badge>
                </div>
            </Card>

            {loading ? (
                <Card className="text-center text-ink-soft py-10 text-sm">加载中…</Card>
            ) : policies.length === 0 ? (
                <Card className="text-center text-ink-soft py-10 text-sm">
                    还没有外部存储。点右上角「添加 S3/R2 存储」,把你的 Cloudflare R2
                    免费 10G 挂进来吧
                </Card>
            ) : (
                <div className="flex flex-col gap-3">
                    {policies.map((p) => (
                        <Card key={p.id}>
                            <div className="flex items-center gap-3 flex-wrap">
                                <Cloud className="size-5 text-sky-600 dark:text-sky-400 shrink-0" />
                                <div className="flex-1 min-w-0">
                                    <div className="font-bold text-sm">
                                        @{p.name}
                                        <span className="text-ink-soft font-normal">
                                            {' '}
                                            · {p.bucket}
                                            {p.basePath && `/${p.basePath}`}
                                        </span>
                                    </div>
                                    <div className="text-xs text-ink-soft truncate">
                                        {p.endpoint}
                                    </div>
                                </div>
                                {p.connected ? (
                                    <Badge tone="green">已挂载</Badge>
                                ) : (
                                    <Badge tone="red">配置异常</Badge>
                                )}
                                <div className="flex gap-1.5">
                                    <Link to={`/files/@${p.name}`}>
                                        <Button size="sm">打开</Button>
                                    </Link>
                                    <Button size="sm" onClick={() => openEdit(p)}>
                                        编辑
                                    </Button>
                                    <Button
                                        variant="ghost-danger"
                                        size="sm"
                                        onClick={() => setDeleteTarget(p)}
                                    >
                                        删除
                                    </Button>
                                </div>
                            </div>
                        </Card>
                    ))}
                </div>
            )}

            <Card className="mt-4 text-xs text-ink-soft leading-relaxed">
                <div className="font-bold text-sm text-ink mb-1.5">说明</div>
                <p>
                    · 上传由服务器中转、下载 302 到预签名地址,浏览器只和本站通信,
                    <b>桶上无需配置 CORS</b>,私有桶即可,不用开公开访问
                </p>
                <p>
                    · Cloudflare R2 填法:Endpoint 是
                    <code className="bg-paper-2 rounded px-1 mx-1">
                        https://&lt;账户ID&gt;.r2.cloudflarestorage.com
                    </code>
                    (R2 概览页可复制),Region 留空即可;密钥在「R2 → 管理 R2 API
                    令牌」创建(对象读和写权限)
                </p>
                <p>
                    · 外部存储与本机的差异:删除不经回收站、不生成缩略图、不参与全局搜索、
                    离线下载/yt下载仍落本机(可下载完再移动上去)
                </p>
                <p>· WebDAV 里同样能看到 @挂载名 文件夹,手机播放器可直接播放桶里的文件</p>
            </Card>

            {/* 添加/编辑 */}
            <Dialog open={editOpen} onOpenChange={(o) => !o && setEditOpen(false)}>
                <DialogContent title={editingId ? `编辑 @${form.name}` : '添加 S3/R2 存储'}>
                    <div className="flex flex-col gap-3">
                        {field('挂载名称(将显示为 @名称 文件夹)', 'name', '例如 R2')}
                        {field(
                            'Endpoint',
                            'endpoint',
                            'https://xxxx.r2.cloudflarestorage.com',
                        )}
                        <div className="grid grid-cols-2 gap-3">
                            {field('Bucket 桶名', 'bucket', 'my-bucket')}
                            {field('Region(R2 留空)', 'region', 'auto')}
                        </div>
                        {field('Access Key ID', 'accessKey', '')}
                        {field(
                            editingId ? 'Secret Key(留空 = 不修改)' : 'Secret Access Key',
                            'secretKey',
                            '',
                            'password',
                        )}
                        {field('桶内路径前缀(可选)', 'basePath', '例如 pocketdrive')}
                        <div className="flex gap-2 justify-end mt-1">
                            <Button disabled={testing} onClick={test}>
                                <Plug className="size-3.5" />
                                {testing ? '测试中…' : '测试连接'}
                            </Button>
                            <Button variant="primary" disabled={saving} onClick={save}>
                                {saving ? '保存中…' : '保存并挂载'}
                            </Button>
                        </div>
                    </div>
                </DialogContent>
            </Dialog>

            {/* 删除确认 */}
            <Dialog open={deleteTarget !== null} onOpenChange={(o) => !o && setDeleteTarget(null)}>
                <DialogContent title={`删除挂载 @${deleteTarget?.name ?? ''}`}>
                    <p className="text-sm">
                        只是取消挂载,<b>桶里的文件不会被删除</b>
                        。之前生成的该存储文件分享链接会失效。确定删除?
                    </p>
                    <div className="flex justify-end gap-2 mt-4">
                        <Button onClick={() => setDeleteTarget(null)}>取消</Button>
                        <Button variant="danger" onClick={doDelete}>
                            删除挂载
                        </Button>
                    </div>
                </DialogContent>
            </Dialog>
        </div>
    );
}
