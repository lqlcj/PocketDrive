import * as React from 'react';
import { cn } from '../../lib/utils';

export function Card({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
    return (
        <div
            className={cn(
                'bg-paper border-2 border-line rounded-[var(--radius-card)] shadow-sm p-4',
                className,
            )}
            {...props}
        />
    );
}

export function CardTitle({ className, ...props }: React.HTMLAttributes<HTMLHeadingElement>) {
    return <h3 className={cn('font-extrabold text-base mb-3', className)} {...props} />;
}
