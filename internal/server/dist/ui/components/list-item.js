'use client';
import { jsx as _jsx } from "react/jsx-runtime";
import { forwardRef } from 'react';
import { cn } from '../../utils';
export const ListItem = forwardRef(function ListItem({ active = false, children, className, type = 'button', ...props }, ref) {
    return (_jsx("button", { className: cn('group relative flex w-full items-center gap-2 px-3 py-2 text-left', 'font-courier text-sm transition-colors cursor-pointer', 'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-midground/30', 'disabled:cursor-not-allowed disabled:opacity-50', active
            ? 'bg-midground/10 text-midground'
            : 'text-midground/70 hover:text-midground hover:bg-midground/5', className), "data-active": active || undefined, ref: ref, type: type, ...props, children: children }));
});
//# sourceMappingURL=list-item.js.map