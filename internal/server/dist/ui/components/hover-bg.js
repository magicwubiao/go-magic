import { createElement } from 'react';
import { cn, polyRef } from '../../utils';
export const HoverBg = polyRef(({ as, className, ...rest }, ref) => createElement((as ?? 'span'), {
    ...rest,
    className: cn('absolute inset-1 bg-midground pointer-events-none', 'opacity-5 transition-opacity duration-250 group-hover:opacity-5 opacity-0 group-hover:duration-0', className),
    ref
}));
//# sourceMappingURL=hover-bg.js.map