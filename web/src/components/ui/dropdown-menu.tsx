import * as React from 'react';
import * as DropdownPrimitive from '@radix-ui/react-dropdown-menu';
import * as ContextPrimitive from '@radix-ui/react-context-menu';
import { cn } from '../../lib/utils';

// 下拉菜单与右键菜单共用一套外观:同一批操作既能从「⋯」按钮点开,
// 也能在列表行上右键唤出,两处视觉完全一致。

const contentCls =
    'z-50 min-w-36 bg-paper border border-line/70 rounded-lg shadow-lg p-1 outline-none';

const itemCls =
    'flex items-center gap-2 px-2 py-1.5 text-sm rounded-md cursor-pointer outline-none select-none data-[disabled]:opacity-40 data-[disabled]:pointer-events-none';

const itemTone = (danger?: boolean) =>
    danger
        ? 'text-danger data-[highlighted]:bg-danger-soft'
        : 'text-ink data-[highlighted]:bg-paper-2';

const separatorCls = 'h-px bg-line/70 my-1 -mx-1';

// ---- 下拉菜单(点触发) ----

export const DropdownMenu = DropdownPrimitive.Root;
export const DropdownMenuTrigger = DropdownPrimitive.Trigger;

export function DropdownMenuContent({
    className,
    align = 'end',
    ...props
}: React.ComponentProps<typeof DropdownPrimitive.Content>) {
    return (
        <DropdownPrimitive.Portal>
            <DropdownPrimitive.Content
                align={align}
                sideOffset={4}
                className={cn(contentCls, className)}
                {...props}
            />
        </DropdownPrimitive.Portal>
    );
}

export function DropdownMenuItem({
    className,
    danger,
    ...props
}: React.ComponentProps<typeof DropdownPrimitive.Item> & { danger?: boolean }) {
    return (
        <DropdownPrimitive.Item
            className={cn(itemCls, itemTone(danger), className)}
            {...props}
        />
    );
}

export function DropdownMenuSeparator({
    className,
    ...props
}: React.ComponentProps<typeof DropdownPrimitive.Separator>) {
    return (
        <DropdownPrimitive.Separator className={cn(separatorCls, className)} {...props} />
    );
}

// ---- 右键菜单 ----

export const ContextMenu = ContextPrimitive.Root;
export const ContextMenuTrigger = ContextPrimitive.Trigger;

export function ContextMenuContent({
    className,
    ...props
}: React.ComponentProps<typeof ContextPrimitive.Content>) {
    return (
        <ContextPrimitive.Portal>
            <ContextPrimitive.Content className={cn(contentCls, className)} {...props} />
        </ContextPrimitive.Portal>
    );
}

export function ContextMenuItem({
    className,
    danger,
    ...props
}: React.ComponentProps<typeof ContextPrimitive.Item> & { danger?: boolean }) {
    return (
        <ContextPrimitive.Item
            className={cn(itemCls, itemTone(danger), className)}
            {...props}
        />
    );
}

export function ContextMenuSeparator({
    className,
    ...props
}: React.ComponentProps<typeof ContextPrimitive.Separator>) {
    return (
        <ContextPrimitive.Separator className={cn(separatorCls, className)} {...props} />
    );
}
