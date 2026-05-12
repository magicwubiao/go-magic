import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { cn } from '../../utils';
import { Typography } from './typography';
export function Stats({ className, items, flip, ...props }) {
    return (_jsx("div", { className: cn('flex w-full flex-col gap-5', className), ...props, children: items.map(({ label, value }) => {
            const valueText = (_jsx(Typography, { className: "text-xs leading-[1.4] tracking-widest", expanded: true, children: typeof value === 'string' ? value : value.node }));
            const labelText = (_jsx(Typography, { className: "leading-none tracking-[0.2em] opacity-60", mono: true, children: typeof label === 'string' ? label : label.node }));
            return (_jsxs("div", { className: "text-midground grid grid-cols-[auto_1fr_auto] items-center gap-2.5 uppercase", children: [flip ? labelText : valueText, _jsx(Typography, { className: "min-w-0 overflow-hidden text-[13px] leading-[1.4] tracking-[0.4em] opacity-20", expanded: true, children: '·'.repeat(100) }), flip ? valueText : labelText] }, (typeof label === 'string' ? label : label.key) + '@@@' + (typeof value === 'string' ? value : value.key)));
        }) }));
}
//# sourceMappingURL=stats.js.map