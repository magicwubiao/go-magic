import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { cn } from '../../../utils';
export function ArrowIcon({ className, direction = 'down', ...props }) {
    return (_jsxs("svg", { className: cn(direction === 'up' && 'rotate-180', direction === 'left' && 'rotate-90', direction === 'right' && '-rotate-90', 'origin-center', className), fill: "none", viewBox: "0 0 13 15", ...props, children: [_jsx("path", { clipRule: "evenodd", d: "M5 15V0h2.50075v15z", fill: "currentColor", fillRule: "evenodd" }), _jsx("path", { clipRule: "evenodd", d: "M10 12.5007H2.5V9.99998H10zM12.4976 9.99951H9.99805v-2.4996h2.49955zM2.4996 9.99951H0v-2.4996h2.4996z", fill: "currentColor", fillRule: "evenodd" })] }));
}
//# sourceMappingURL=arrow.js.map