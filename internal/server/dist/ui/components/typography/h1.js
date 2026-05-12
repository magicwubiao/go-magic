import { jsx as _jsx } from "react/jsx-runtime";
import { forwardRef } from 'react';
import { cn } from '../../../utils';
import { Typography } from '.';
export const H1 = forwardRef(({ className, ...props }, ref) => {
    return (_jsx(Typography, { as: "h1", className: cn('font-bold', className), variant: "xl", ref, ...props }));
});
//# sourceMappingURL=h1.js.map