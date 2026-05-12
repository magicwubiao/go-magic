import { cva } from 'class-variance-authority';
import { createElement } from 'react';
import { cn, polyRef } from '../../../utils';
const typographyVariants = cva('font-sans', {
    variants: {
        compressed: { true: 'font-compressed' },
        courier: { true: 'font-courier' },
        expanded: { true: 'font-expanded' },
        mondwest: { true: 'font-mondwest tracking-[0.1875rem]' },
        mono: { true: 'font-mono' },
        sans: { true: 'font-sans' },
        variant: {
            lg: 'text-[2.625rem] leading-[1] tracking-[0.0525rem]',
            md: 'text-[2.625rem] leading-[1] tracking-[0.0525rem]',
            sm: 'leading-1.4 text-[.9375rem] tracking-[0.1875rem]',
            xl: 'text-[4.5rem] leading-[1] tracking-[0.135rem]'
        }
    }
});
export const Typography = polyRef(({ as, className, compressed, courier, expanded, mondwest, mono, variant, ...rest }, ref) => {
    const fonts = { compressed, courier, expanded, mondwest, mono };
    const fontVariant = { ...fonts, sans: !Object.values(fonts).some(Boolean) };
    return createElement((as ?? 'span'), {
        ...rest,
        className: cn(typographyVariants({ ...fontVariant, variant }), className),
        ref
    });
});
//# sourceMappingURL=index.js.map