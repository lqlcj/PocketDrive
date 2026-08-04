import * as React from 'react';
import { cn } from '../../lib/utils';

export function Input({ className, ...props }: React.InputHTMLAttributes<HTMLInputElement>) {
    return (
        <input
            className={cn(
                'h-9 w-full rounded-full border border-line bg-paper px-3.5 text-sm text-ink placeholder:text-ink-soft/70 outline-none transition-[border-color,box-shadow] focus:border-leaf focus:ring-2 focus:ring-leaf/25 disabled:opacity-50',
                className,
            )}
            {...props}
        />
    );
}

export function Textarea({
    className,
    ...props
}: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
    return (
        <textarea
            className={cn(
                'w-full rounded-2xl border border-line bg-paper p-3 text-sm text-ink placeholder:text-ink-soft/70 outline-none transition-[border-color,box-shadow] focus:border-leaf focus:ring-2 focus:ring-leaf/25 disabled:opacity-50 resize-none',
                className,
            )}
            {...props}
        />
    );
}

export function NativeSelect({
    className,
    ...props
}: React.SelectHTMLAttributes<HTMLSelectElement>) {
    return (
        <select
            className={cn(
                'h-9 rounded-full border border-line bg-paper px-3 text-sm text-ink outline-none transition-[border-color,box-shadow] focus:border-leaf focus:ring-2 focus:ring-leaf/25 cursor-pointer',
                className,
            )}
            {...props}
        />
    );
}

export function Checkbox({
    label,
    className,
    ...props
}: React.InputHTMLAttributes<HTMLInputElement> & { label: React.ReactNode }) {
    return (
        <label
            className={cn(
                'inline-flex items-center gap-1.5 text-sm cursor-pointer select-none',
                className,
            )}
        >
            <input type="checkbox" className="size-4 accent-[var(--leaf)] cursor-pointer" {...props} />
            {label}
        </label>
    );
}
