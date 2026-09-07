import { read, utils, type WorkBook } from 'xlsx';

let workbook: WorkBook;

self.onmessage = ({ data }) => {
    try {
        if (data.buffer) {
            workbook = data.csv
                ? read(new TextDecoder('utf-8', { fatal: true }).decode(data.buffer), { type: 'string', raw: true, cellHTML: false, cellFormula: false })
                : read(data.buffer, { type: 'array', cellHTML: false, cellFormula: false });
        }
        const names = workbook.SheetNames.filter((_, index) => !workbook.Workbook?.Sheets?.[index]?.Hidden);
        const name = names[data.sheet ?? 0];
        const sheet = workbook.Sheets[name];
        const range = sheet?.['!ref'] ? utils.decode_range(sheet['!ref']) : null;
        const rowCount = range ? range.e.r + 1 : 0;
        const columnCount = range ? range.e.c + 1 : 0;
        const rowStart = data.row ?? 0;
        const columnStart = data.column ?? 0;
        const columns = Array.from({ length: Math.min(30, Math.max(0, columnCount - columnStart)) }, (_, index) => utils.encode_col(columnStart + index));
        const rows = Array.from({ length: Math.min(100, Math.max(0, rowCount - rowStart)) }, (_, index) =>
            columns.map((column) => {
                const cell = sheet[`${column}${rowStart + index + 1}`];
                return cell ? utils.format_cell(cell) : '';
            }));
        self.postMessage({ names, rows, columns, rowCount, columnCount });
    } catch (error) {
        self.postMessage({ error: error instanceof Error ? error.message : '表格解析失败' });
    }
};
