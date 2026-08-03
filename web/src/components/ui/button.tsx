import * as React from 'react';
import { cva } from 'class-variance-authority';
import type { VariantProps } from 'class-variance-authority';
import { cn } from '../../lib/utils';

const buttonVariants = cva(
    'inline-flex items-center justify-center gap-1.5 whitespace-nowrap font-bold transition-all cursor-pointer select-none disabled:pointer-events-none disabled:opacity-50 active:translate-y-px outline-none focus-visible:ring-2 focus-visible:ring-leaf/50',
    {
        variants: {
            variant: {
                default:
                    'bg-paper text-ink border-2 border-line rounded-full shadow-sm hover:border-leaf hover:text-leaf-dark',
                primary:
                    'bg-leaf text-white rounded-full shadow-sm hover:brightness-105 border-2 border-leaf',
                ghost: 'rounded-full text-ink hover:bg-paper-2',
                danger: 'bg-paper text-danger border-2 border-danger/40 rounded-full hover:bg-danger-soft',
                'ghost-danger': 'rounded-full text-danger hover:bg-danger-soft',
            },
            size: {
                default: 'h-9 px-4 text-sm',
                sm: 'h-7 px-2.5 text-xs',
                lg: 'h-11 px-6 text-base',
                icon: 'h-9 w-9 text-base',
            },
        },
        defaultVariants: { variant: 'default', size: 'default' },
    },
);

export interface ButtonProps
    extends React.ButtonHTMLAttributes<HTMLButtonElement>,
        VariantProps<typeof buttonVariants> {}

export function Button({ className, variant, size, type, ...props }: ButtonProps) {
    return (
        <button
            type={type ?? 'button'}
            className={cn(buttonVariants({ variant, size }), className)}
            {...props}
        />
    );
}
