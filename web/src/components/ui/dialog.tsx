import * as React from 'react';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { X } from 'lucide-react';
import { cn } from '../../lib/utils';
import { Button } from './button';

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;

export function DialogContent({
    className,
    children,
    title,
    wide,
    ...props
}: React.ComponentProps<typeof DialogPrimitive.Content> & {
    title: React.ReactNode;
    wide?: boolean;
}) {
    return (
        <DialogPrimitive.Portal>
            <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/45 backdrop-blur-[2px]" />
            <DialogPrimitive.Content
                className={cn(
                    'fixed left-1/2 top-1/2 z-50 -translate-x-1/2 -translate-y-1/2 w-[calc(100vw-24px)] bg-paper border border-line/70 rounded-[var(--radius-card)] shadow-2xl p-5 outline-none max-h-[min(88vh,560px)] overflow-auto overscroll-behavior-contain',
                    wide ? 'max-w-4xl' : 'max-w-md',
                    className,
                )}
                {...props}
            >
                <div className="flex items-center gap-2 mb-3">
                    <DialogPrimitive.Title className="font-extrabold text-base flex-1 min-w-0 truncate">
                        {title}
                    </DialogPrimitive.Title>
                    <DialogPrimitive.Close asChild>
                        <Button variant="ghost" size="icon" aria-label="关闭">
                            <X className="size-4" />
                        </Button>
                    </DialogPrimitive.Close>
                </div>
                {children}
            </DialogPrimitive.Content>
        </DialogPrimitive.Portal>
    );
}

/** 简易确认/表单弹窗的底部按钮排 */
export function DialogFooter({
    onOk,
    okText = '确定',
    okDanger,
    okLoading,
}: {
    onOk: () => void;
    okText?: string;
    okDanger?: boolean;
    okLoading?: boolean;
}) {
    return (
        <div className="flex justify-end gap-2 mt-4">
            <DialogPrimitive.Close asChild>
                <Button>取消</Button>
            </DialogPrimitive.Close>
            <Button
                variant={okDanger ? 'danger' : 'primary'}
                disabled={okLoading}
                onClick={onOk}
            >
                {okText}
            </Button>
        </div>
    );
}
