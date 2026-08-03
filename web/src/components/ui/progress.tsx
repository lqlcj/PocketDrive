import type { ReactNode } from 'react';
import { cn } from '../../lib/utils';

export function Progress({
    percent,
    className,
}: {
    percent: number;
    className?: string;
}) {
    const p = Math.max(0, Math.min(100, Number.isFinite(percent) ? percent : 0));
    return (
        <div
            className={cn('h-3.5 w-full rounded-full bg-paper-2 overflow-hidden', className)}
            role="progressbar"
            aria-valuenow={Math.round(p)}
        >
            <div
                className="h-full rounded-full bg-leaf transition-[width] duration-500 flex items-center justify-end"
                style={{ width: `${p}%` }}
            >
                {p >= 18 && (
                    <span className="text-[10px] font-bold text-white px-1.5">
                        {Math.round(p)}%
                    </span>
                )}
            </div>
        </div>
    );
}

export function Badge({
    children,
    tone = 'default',
    className,
}: {
    children: ReactNode;
    tone?: 'default' | 'green' | 'blue' | 'orange' | 'red';
    className?: string;
}) {
    const tones: Record<string, string> = {
        default: 'bg-paper-2 text-ink-soft border-line',
        green: 'bg-leaf-soft text-leaf-dark border-leaf/40',
        blue: 'bg-sky-100 text-sky-700 border-sky-300 dark:bg-sky-900/40 dark:text-sky-300 dark:border-sky-700',
        orange: 'bg-amber-100 text-amber-700 border-amber-300 dark:bg-amber-900/40 dark:text-amber-300 dark:border-amber-700',
        red: 'bg-danger-soft text-danger border-danger/40',
    };
    return (
        <span
            className={cn(
                'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-bold whitespace-nowrap',
                tones[tone],
                className,
            )}
        >
            {children}
        </span>
    );
}
