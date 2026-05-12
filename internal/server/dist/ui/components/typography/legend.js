import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { cn } from '../../../utils';
import { Small } from './small';
export function Legend({ children, className, label, sub, ...props }) {
    return (_jsxs("hgroup", { className: cn('flex flex-col gap-2', className), ...props, children: [_jsx(Small, { children: label }), sub && _jsxs(Small, { className: "opacity-50", children: ["- ", sub] }), children] }));
}
//# sourceMappingURL=legend.js.map