'use client';
import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { cn } from '../../utils';
import { ImageDistortion } from './image-distortion';
import { Typography } from './typography';
import { Small } from './typography/small';
/**
 * Selectable tier / pricing card. Full-bleed distorted image background,
 * readable overlay text, and an animated `.arc-border` shimmer on the
 * selected state. Fully presentational — the consumer owns the data
 * (tier schema, price formatting, tier imagery / tints).
 *
 * Visual states:
 * - `selected`: brightens the distortion, activates `.arc-border`, and
 *   composites the headline / price with `mix-blend-mode: plus-lighter`
 *   so the text lifts off the image regardless of tint.
 * - `isCurrent`: subtle midground-tinted border hint (suppressed when
 *   `selected` wins).
 * - `overlay`: optional top-layer color blended with `mix-blend-mode:
 *   color` — used for the "highest tier" red treatment on top of any
 *   base tint.
 */
export function TierCard({ badge, bullets, className, image, isCurrent = false, onSelect, overlay, price, selected = false, tint, tintStrength, title }) {
    return (_jsxs("button", { className: cn('group relative flex w-full cursor-pointer flex-col border border-current/20', 'text-left transition-colors duration-300', selected && 'border-midground/60', isCurrent && !selected && 'border-midground/30', className), onClick: onSelect, type: "button", children: [_jsx("span", { "aria-hidden": true, className: cn('arc-border transition-opacity duration-200', selected ? 'opacity-100' : 'opacity-0 group-hover:opacity-100') }), _jsxs("div", { className: "relative aspect-[3/4] min-h-0 w-full flex-1 overflow-hidden", style: { backgroundColor: 'var(--background)' }, children: [_jsx(ImageDistortion, { active: selected, src: image, tint: tint, tintStrength: tintStrength }), overlay && (_jsx("div", { className: "pointer-events-none absolute inset-0", style: { backgroundColor: overlay, mixBlendMode: 'color' } })), _jsxs("div", { className: "pointer-events-none absolute inset-0 z-[1] flex flex-col justify-between p-3", children: [_jsxs("div", { className: "flex flex-col gap-0.5", children: [_jsxs(Small, { className: cn('block drop-shadow-[0_1px_2px_rgba(0,0,0,0.5)]', 'transition-colors', selected && 'text-midground'), style: selected ? { mixBlendMode: 'plus-lighter' } : undefined, children: [title, badge && _jsx("span", { className: "ml-1 opacity-50", children: badge })] }), price.secondary ? (_jsxs(_Fragment, { children: [_jsxs(Typography, { className: "block text-sm line-through opacity-50 drop-shadow-[0_1px_3px_rgba(0,0,0,0.6)]", expanded: true, style: { mixBlendMode: 'plus-lighter' }, children: [price.secondary, price.secondarySuffix && (_jsx("span", { className: "text-[0.625rem]", children: price.secondarySuffix }))] }), _jsxs(Typography, { className: "block text-lg font-bold drop-shadow-[0_1px_3px_rgba(0,0,0,0.6)]", expanded: true, style: { mixBlendMode: 'plus-lighter' }, children: [price.primary, price.primarySuffix && (_jsxs("span", { className: "text-[0.625rem] opacity-60", children: [' ', price.primarySuffix] }))] })] })) : (_jsxs(Typography, { className: "block text-lg font-bold drop-shadow-[0_1px_3px_rgba(0,0,0,0.6)]", expanded: true, style: { mixBlendMode: 'plus-lighter' }, children: [price.primary, price.primarySuffix && (_jsx("span", { className: "text-[0.625rem] opacity-60", children: price.primarySuffix }))] }))] }), bullets.length > 0 && (_jsx("ul", { className: "flex flex-col gap-1", children: bullets.map((bullet, i) => (_jsxs("li", { className: cn('font-courier text-[0.6875rem] leading-tight tracking-wider uppercase', 'drop-shadow-[0_1px_2px_rgba(0,0,0,0.5)]', 'opacity-70'), children: ["\u00B7 ", bullet] }, typeof bullet === 'string' ? bullet : i))) }))] })] })] }));
}
//# sourceMappingURL=tier-card.js.map