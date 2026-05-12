import { createElement } from 'react';
import { cn, polyRef } from '../../../utils';
export const Grid = polyRef(({ as, className, ...rest }, ref) => createElement((as ?? 'div'), {
    ...rest,
    className: cn('g', className),
    ref
}));
export const Cell = polyRef(({ as, className, ...rest }, ref) => createElement((as ?? 'div'), {
    ...rest,
    className: cn('gc', className),
    ref
}));
//# sourceMappingURL=index.js.map