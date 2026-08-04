import * as React from 'react';
import { cn } from '../../lib/utils';

export function Card({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
    return (
        <div
            className={cn(
                'bg-paper border border-line/70 rounded-[var(--radius-card)] shadow-[var(--shadow-card)] p-4',
                className,
            )}
            {...props}
        />
    );
}

export function CardTitle({
    className,
    children,
    ...props
}: React.HTMLAttributes<HTMLHeadingElement>) {
    return (
        <h3
            className={cn('font-extrabold text-base mb-3 flex items-center gap-2', className)}
            {...props}
        >
            {children}
        </h3>
    );
}
