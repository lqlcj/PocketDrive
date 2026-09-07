import { useEffect, useRef, useState } from 'react';
import { ChevronLeft, ChevronRight, Loader2 } from 'lucide-react';
import { Button } from './ui/button';

interface SheetPage {
    names: string[];
    columns: string[];
    rows: string[][];
    rowCount: number;
    columnCount: number;
}

export default function SheetPreview({ url, name }: { url: string; name: string }) {
    const worker = useRef<Worker | null>(null);
    const [page, setPage] = useState<SheetPage | null>(null);
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(true);
    const [position, setPosition] = useState({ sheet: 0, row: 0, column: 0 });

    useEffect(() => {
        const parser = new Worker(new URL('./sheet.worker.ts', import.meta.url), { type: 'module' });
        const controller = new AbortController();
        worker.current = parser;
        parser.onmessage = ({ data }) => {
            setBusy(false);
            if (data.error) setError(data.error);
            else setPage(data);
        };
        parser.onerror = () => { setError('表格解析失败'); setBusy(false); };
        (async () => {
            const response = await fetch(url, { signal: controller.signal });
            if (!response.ok) throw new Error(`文件读取失败 (${response.status})`);
            const buffer = await response.arrayBuffer();
            if (!controller.signal.aborted) parser.postMessage({ buffer, csv: name.toLowerCase().endsWith('.csv') }, [buffer]);
        })().catch((reason) => {
            if (!controller.signal.aborted) { setError(String(reason)); setBusy(false); }
        });
        return () => { controller.abort(); parser.terminate(); worker.current = null; };
    }, [url, name]);

    const move = (next: typeof position) => {
        setPosition(next);
        setBusy(true);
        worker.current?.postMessage(next);
    };

    if (error) return <p className="p-6 text-sm text-ink-soft">预览失败：{error}</p>;
    if (!page) return <div className="flex items-center justify-center gap-2 p-10 text-sm text-ink-soft"><Loader2 className="size-4 animate-spin" /> 正在读取表格…</div>;

    return (
        <div className="pd-sheet-preview" aria-busy={busy}>
            <div className="pd-sheet-toolbar">
                <select aria-label="工作表" value={position.sheet} disabled={busy} onChange={(event) => move({ sheet: Number(event.target.value), row: 0, column: 0 })}>
                    {page.names.map((name, index) => <option key={name} value={index}>{name}</option>)}
                </select>
                <span className="text-xs text-ink-soft">{page.rowCount} 行 · {page.columnCount} 列</span>
                {busy && <Loader2 className="size-4 animate-spin" />}
            </div>
            <div className="pd-sheet-grid">
                {page.rows.length ? (
                    <table>
                        <thead><tr><th aria-label="行号" />{page.columns.map((column) => <th key={column}>{column}</th>)}</tr></thead>
                        <tbody>{page.rows.map((row, index) => (
                            <tr key={position.row + index}><th scope="row">{position.row + index + 1}</th>{row.map((value, column) => <td key={column}>{value}</td>)}</tr>
                        ))}</tbody>
                    </table>
                ) : <p className="p-6 text-sm text-ink-soft">空工作表</p>}
            </div>
            <div className="pd-sheet-toolbar">
                <Button size="icon" title="上一页行" aria-label="上一页行" disabled={busy || position.row === 0} onClick={() => move({ ...position, row: Math.max(0, position.row - 100) })}><ChevronLeft className="size-4" /></Button>
                <span className="text-xs">行 {page.rowCount ? position.row + 1 : 0}–{Math.min(position.row + 100, page.rowCount)}</span>
                <Button size="icon" title="下一页行" aria-label="下一页行" disabled={busy || position.row + 100 >= page.rowCount} onClick={() => move({ ...position, row: position.row + 100 })}><ChevronRight className="size-4" /></Button>
                <Button size="icon" title="上一组列" aria-label="上一组列" disabled={busy || position.column === 0} onClick={() => move({ ...position, column: Math.max(0, position.column - 30) })}><ChevronLeft className="size-4" /></Button>
                <span className="text-xs">列 {page.columns[0] ?? '0'}–{page.columns.at(-1) ?? '0'}</span>
                <Button size="icon" title="下一组列" aria-label="下一组列" disabled={busy || position.column + 30 >= page.columnCount} onClick={() => move({ ...position, column: position.column + 30 })}><ChevronRight className="size-4" /></Button>
            </div>
        </div>
    );
}
