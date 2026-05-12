'use client';
import { jsx as _jsx, Fragment as _Fragment, jsxs as _jsxs } from "react/jsx-runtime";
import { cn } from '../../utils';
import { Scramble } from './ascii';
import { LinkIcon } from './icons';
import { Typography } from './typography';
const ETH_RE = /^0x[a-fA-F0-9]{40}$/;
const truncate = (a) => `${a.slice(0, 6)}${'·'.repeat(8)}${a.slice(-4)}`;
export function Watchlist({ className, counter = false, items, scramble = false, ...props }) {
    return (_jsx("div", { className: cn('flex flex-col gap-3', className), ...props, children: items.map(({ label, right, url }, i) => {
            const isStr = typeof label === 'string';
            const eth = isStr && ETH_RE.test(label);
            const text = eth ? truncate(label) : label;
            return (_jsxs("a", { className: cn('grid items-center gap-2.5 px-2.5 py-1.5', 'leading-[1.4] uppercase', 'hover:bg-midground/10! hover:ring-2 hover:ring-current/20', 'transition-all duration-500 hover:duration-0', 'opacity-(--midground-alpha)'), href: url, rel: "noopener noreferrer", style: {
                    background: `color-mix(in oklch, var(--color-midground) ${10 * Math.max(0, 1 - i / 9)}%, transparent)`,
                    gridTemplateColumns: [
                        counter && 'auto auto',
                        '1fr',
                        right && 'auto',
                        url && 'auto auto'
                    ]
                        .filter(Boolean)
                        .join(' ')
                }, target: "_blank", children: [counter && (_jsxs(_Fragment, { children: [_jsx(Typography, { className: "text-lg tracking-[0.35em] opacity-40", compressed: true, children: String(i + 1).padStart(2, '0') }), _jsx("span", { className: "text-[0.8125rem] font-bold tracking-[0.4em] opacity-20", children: ":" })] })), isStr ? (_jsx(Typography, { className: "min-w-0 overflow-hidden text-lg font-bold tracking-[0.35em]", ...(eth ? { mono: true } : { compressed: true }), children: scramble ? _jsx(Scramble, { delay: i * 80, text: text }) : text })) : (label), right && (_jsx(Typography, { className: "text-right text-sm tracking-widest opacity-40", mono: true, children: right })), url && (_jsxs(_Fragment, { children: [_jsx("span", { className: "text-[0.8125rem] tracking-[0.4em] opacity-20", children: ":" }), _jsx(LinkIcon, { className: "text-midground size-3.5" })] }))] }, i));
        }) }));
}
//# sourceMappingURL=watchlist.js.map