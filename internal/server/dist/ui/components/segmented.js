'use client';
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { cn } from '../../utils';
export function Segmented({ className, onChange, options, size = 'sm', value }) {
    return (_jsx("div", { className: cn('inline-flex border border-midground/15 bg-background/30', className), role: "radiogroup", children: options.map(opt => {
            const active = opt.value === value;
            return (_jsx("button", { "aria-checked": active, className: cn('font-mondwest tracking-[0.1em] uppercase', 'transition-colors cursor-pointer whitespace-nowrap', 'border-r border-midground/15 last:border-r-0', 'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-midground/30', size === 'sm' && 'h-7 px-2.5 text-[0.65rem]', size === 'md' && 'h-8 px-3 text-xs', active
                    ? 'bg-midground text-background'
                    : 'text-midground/60 hover:bg-midground/10 hover:text-midground'), onClick: () => onChange(opt.value), role: "radio", type: "button", children: opt.label }, opt.value));
        }) }));
}
export function FilterGroup({ children, className, label }) {
    return (_jsxs("div", { className: cn('flex items-center gap-2', className), children: [_jsx("span", { className: "font-mondwest text-[0.65rem] tracking-[0.12em] uppercase text-midground/50", children: label }), children] }));
}
//# sourceMappingURL=segmented.js.map