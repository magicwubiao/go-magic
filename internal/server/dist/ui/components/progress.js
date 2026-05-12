import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { cn } from '../../utils';
import { Typography } from './typography';
export const Progress = ({ animate = true, barProps, children, className, speed = 0.4, value, ...props }) => (_jsxs("div", { className: cn('relative flex min-h-[2.3rem] min-w-0 flex-1 items-stretch overflow-hidden', className), ...props, children: [_jsx(Typography, { ...barProps, className: cn('shrink-0 translate-y-0.5 truncate py-2', 'bg-midground/20', children ? 'px-2' : 'px-0', barProps?.className), mono: true, style: {
                ...(animate && { transition: `width ${speed}s steps(10, end)` }),
                width: `${value}%`,
                ...barProps?.style
            }, children: children }), _jsx("div", { className: "flex-1", style: {
                '--x': '.5rem',
                backgroundImage: `repeating-linear-gradient(to right, transparent 0 var(--x), color-mix(in srgb, var(--color-midground) 17%, transparent) var(--x) calc(var(--x) + 1px))`
            } })] }));
//# sourceMappingURL=progress.js.map