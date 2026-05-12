'use client';
import { jsx as _jsx, Fragment as _Fragment, jsxs as _jsxs } from "react/jsx-runtime";
import { createElement } from 'react';
import { cn, polyRef } from '../../../utils';
export const FitText = polyRef(({ as, children, className, max, min = '1em', style: baseStyle, ...rest }, ref) => {
    if (typeof children !== 'string') {
        return null;
    }
    const style = {
        '--fit-max': max ?? 'infinity * 1px',
        '--fit-min': min,
        ...baseStyle
    };
    return createElement((as ?? 'span'), { ...rest, className: cn('fit-text', className), ref, style }, _jsxs(_Fragment, { children: [_jsx("span", { children: _jsx("span", { children: children }) }), _jsx("span", { "aria-hidden": "true", children: children })] }));
});
//# sourceMappingURL=index.js.map