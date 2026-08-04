import { useState } from 'react';
import type { Profile } from '../api';
import { api } from '../api';
import { cn } from '../lib/utils';

// 用户头像。上传过就显示图片,没有则用用户名首字母——纯 CSS 渲染,
// 不占任何存储,也不会出现在网盘目录里。

/** 取用户名的首个字符:中文取首字,英文取首字母大写 */
function initial(name: string): string {
    const trimmed = name.trim();
    if (trimmed === '') return '?';
    const first = Array.from(trimmed)[0];
    return first.toUpperCase();
}

/** 由用户名派生一个稳定的底色,不同用户名颜色不同但自己每次都一样 */
function hueOf(name: string): number {
    let h = 0;
    for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) % 360;
    return h;
}

export default function Avatar({
    profile,
    className,
    size = 'md',
}: {
    profile: Profile;
    className?: string;
    size?: 'sm' | 'md' | 'lg';
}) {
    // 图片加载失败(比如刚删掉)时回退到首字母,不留一个破图标
    const [failed, setFailed] = useState(false);

    const sizeCls =
        size === 'lg' ? 'size-16 text-2xl' : size === 'sm' ? 'size-6 text-[11px]' : 'size-8 text-sm';

    if (profile.hasAvatar && !failed) {
        return (
            <img
                src={api.avatarUrl(profile.avatarVersion ?? '')}
                alt={profile.user}
                onError={() => setFailed(true)}
                className={cn(sizeCls, 'rounded-full object-cover shrink-0', className)}
            />
        );
    }
    return (
        <span
            className={cn(
                sizeCls,
                'rounded-full shrink-0 grid place-items-center font-bold text-white select-none',
                className,
            )}
            style={{ backgroundColor: `hsl(${hueOf(profile.user)} 45% 45%)` }}
            title={profile.user}
        >
            {initial(profile.user)}
        </span>
    );
}
