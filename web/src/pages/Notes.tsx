import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { toast } from 'sonner';
import { api } from '../api';
import type { IndexItem } from '../api';
import { Card } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Dialog, DialogContent, DialogFooter } from '../components/ui/dialog';
import { formatBytes, formatTime } from '../util';

export default function Notes() {
    const navigate = useNavigate();
    const [notes, setNotes] = useState<IndexItem[]>([]);
    const [loading, setLoading] = useState(true);
    const [newOpen, setNewOpen] = useState(false);
    const [newName, setNewName] = useState('');

    useEffect(() => {
        api.category('markdown')
            .then((r) => setNotes(r.items))
            .catch(() => undefined)
            .finally(() => setLoading(false));
    }, []);

    const create = async () => {
        const name = newName.trim();
        if (!name) return;
        const file = (name.endsWith('.md') ? name : `${name}.md`).replace(/[/\\]/g, '_');
        try {
            await api.writeFile(file, `# ${name.replace(/\.md$/, '')}\n\n`);
            navigate(`/note/${file}`);
        } catch (e) {
            toast.error(e instanceof Error ? e.message : '创建失败');
        }
    };

    return (
        <div>
            <div className="flex items-center gap-3 mb-4">
                <h2 className="text-xl font-extrabold">📔 笔记本</h2>
                <Button
                    variant="primary"
                    size="sm"
                    className="ml-auto"
                    onClick={() => setNewOpen(true)}
                >
                    ✏️ 新建笔记
                </Button>
            </div>
            {loading ? (
                <Card className="text-center text-ink-soft py-10 text-sm">加载中…</Card>
            ) : notes.length === 0 ? (
                <Card className="text-center text-ink-soft py-10 text-sm">
                    还没有笔记,写下第一篇吧 ✏️
                </Card>
            ) : (
                <div className="grid sm:grid-cols-2 md:grid-cols-3 gap-3">
                    {notes.map((n) => (
                        <Link key={n.path} to={`/note/${n.path}`}>
                            <Card className="hover:border-leaf transition-colors cursor-pointer h-full">
                                <div className="font-extrabold truncate">
                                    📝 {n.name.replace(/\.md$/, '')}
                                </div>
                                <div className="text-xs text-ink-soft mt-1.5 truncate">
                                    {n.path}
                                </div>
                                <div className="text-xs text-ink-soft mt-1">
                                    {formatBytes(n.size)} · {formatTime(n.mtime)}
                                </div>
                            </Card>
                        </Link>
                    ))}
                </div>
            )}

            <Dialog open={newOpen} onOpenChange={(o) => !o && setNewOpen(false)}>
                <DialogContent title="新建笔记">
                    <p className="text-xs text-ink-soft mb-2">
                        笔记会存放在网盘根目录,想放进文件夹可去「我的文件」里新建
                    </p>
                    <Input
                        placeholder="笔记名称(自动加 .md)"
                        value={newName}
                        onChange={(e) => setNewName(e.target.value)}
                        onKeyDown={(e) => e.key === 'Enter' && create()}
                    />
                    <DialogFooter onOk={create} okText="创建并编辑" />
                </DialogContent>
            </Dialog>
        </div>
    );
}
