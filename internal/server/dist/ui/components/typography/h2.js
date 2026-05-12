import { jsx as _jsx } from "react/jsx-runtime";
import { forwardRef } from 'react';
import { cn } from '../../../utils';
import { Typography } from '.';
export const H2 = forwardRef(({ className, ...props }, ref) => {
    return (_jsx(Typography, { as: "h2", className: cn('font-bold', className), variant: "lg", ref, ...props }));
});
//# sourceMappingURL=h2.js.map