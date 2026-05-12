import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { cn } from '../../../utils';
export function HamburgerIcon({ className, open = false, ...props }) {
    return (_jsxs("svg", { className: cn('size-5', className), fill: "none", stroke: "currentColor", strokeLinecap: "round", strokeWidth: 1.5, viewBox: "0 0 24 24", ...props, children: [_jsx("line", { className: "origin-center transition-transform duration-200 ease-out", style: { transform: open ? 'rotate(45deg)' : 'translateY(-4px)' }, x1: 4, x2: 20, y1: 12, y2: 12 }), _jsx("line", { className: "transition-opacity duration-200 ease-out", style: { opacity: open ? 0 : 1 }, x1: 4, x2: 20, y1: 12, y2: 12 }), _jsx("line", { className: "origin-center transition-transform duration-200 ease-out", style: { transform: open ? 'rotate(-45deg)' : 'translateY(4px)' }, x1: 4, x2: 20, y1: 12, y2: 12 })] }));
}
//# sourceMappingURL=hamburger.js.map