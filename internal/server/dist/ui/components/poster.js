'use client';
import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import fillerBg from '../../assets/filler-bg0.jpg';
import { cn } from '../../utils';
import { Blink } from './blink';
import { ImageDistortion } from './image-distortion';
import { Typography } from './typography';
import { Small } from './typography/small';
const ASPECT_CONFIG = {
    landscape: { defaultLayout: 'split', height: 1080, width: 1920 },
    portrait: { defaultLayout: 'split', height: 1350, width: 1080 },
    square: { defaultLayout: 'split', height: 1080, width: 1080 },
    story: { defaultLayout: 'stacked', height: 1920, width: 1080 },
    wide: { defaultLayout: 'split', height: 900, width: 1600 }
};
const DEFAULT_SRC = fillerBg.src ?? fillerBg;
function useUtcClock() {
    const [now, setNow] = useState(null);
    useEffect(() => {
        setNow(new Date());
        const id = setInterval(() => setNow(new Date()), 1000);
        return () => clearInterval(id);
    }, []);
    return now ? now.toISOString().slice(11, 19) : '--:--:--';
}
function CornerMark({ className }) {
    return (_jsxs("span", { "aria-hidden": true, className: cn('pointer-events-none absolute block size-4 opacity-50', className), children: [_jsx("span", { className: "absolute top-1/2 left-0 h-px w-full -translate-y-1/2 bg-current" }), _jsx("span", { className: "absolute top-0 left-1/2 h-full w-px -translate-x-1/2 bg-current" })] }));
}
function ChannelDot() {
    return (_jsxs("span", { className: "flex items-center gap-1.5", children: [_jsx("span", { className: "bg-midground size-1.5 animate-pulse rounded-full" }), _jsx(Small, { className: "opacity-70", children: "REC" })] }));
}
function ScanlineOverlay() {
    return (_jsx("div", { "aria-hidden": true, className: "pointer-events-none absolute inset-0 opacity-20 mix-blend-overlay", style: {
            backgroundImage: 'repeating-linear-gradient(0deg, transparent 0, transparent 2px, rgba(255,255,255,0.08) 2px, rgba(255,255,255,0.08) 3px)'
        } }));
}
/**
 * Social-ready glitchy card built around the haptic-distortion image
 * component. The poster runs the sword-guy distortion on an auto-animated
 * slash pattern so it can be screen-recorded as a GIF without a human
 * moving a cursor.
 *
 * Two variants, matching actual use cases:
 * - `'vibe'` (default): full-bleed distorted image with just registration
 *   marks and a tiny "Hermes Agent" mark in the corner — mirrors the
 *   overlay on the Hermes agent website.
 * - `'dispatch'`: broadcast-card layout with sidebar copy, numbered tags,
 *   and chrome — for when the poster needs to carry information.
 */
export function Poster({ aspect = 'square', autoPlay = 'slash', body, border = true, channel, children, className, cornerMarks = true, eyebrow, headline = ['An Agent', 'That Grows', 'With You.'], layout, scale = 1, seal = 'MIT · 2026', signature, src = DEFAULT_SRC, tags, tint, tintStrength, variant = 'vibe', ...rest }) {
    const config = ASPECT_CONFIG[aspect];
    const resolvedLayout = layout ?? config.defaultLayout;
    // Use aspect-ratio + max-width/height so the poster fluidly fits any parent
    // (storybook iframe, a tweet preview, an embed) without getting clipped,
    // but caps at the intended export width for screen-recording. `maxHeight`
    // uses an absolute `dvh`-based value rather than `%` because `%` inside a
    // flex container can cause the browser to clamp height without re-running
    // aspect-ratio on width, producing a subtly wrong shape. An absolute cap
    // leaves aspect-ratio fully in charge: once the height binds, width is
    // re-derived correctly. `calc(100dvh - 8rem)` = viewport minus a typical
    // host's vertical padding (e.g. Storybook's `p-8` = 4rem on each side),
    // so the poster + padding fit within the viewport without ever producing
    // scrollbars. Container queries tie all internal typography to the
    // actual rendered width so headline/metadata scales along with the canvas.
    const outerProps = {
        // `text-midground` (not `text-foreground`) is the readable on-canvas
        // color across every lens. `--foreground` is really the lens's inversion
        // layer color: on dark lenses it has `fgOpacity: 0` and resolves to
        // fully-transparent via `color-mix`, which would make text invisible.
        // `--midground` always has opacity 1 and picks up each lens's accent.
        className: cn('text-midground relative overflow-hidden font-sans', border && 'border border-current/25', className),
        style: {
            aspectRatio: `${config.width} / ${config.height}`,
            background: 'var(--background)',
            containerType: 'inline-size',
            fontSize: `${(16 / config.width) * 100}cqi`,
            maxHeight: 'calc(100dvh - 8rem)',
            maxWidth: '100%',
            width: `${config.width * scale}px`
        },
        ...rest
    };
    if (variant === 'vibe') {
        return (_jsx("div", { ...outerProps, children: _jsx(VibeContent, { autoPlay: autoPlay, channel: channel, cornerMarks: cornerMarks, signature: signature, src: src, tint: tint, tintStrength: tintStrength }) }));
    }
    const headlineLines = Array.isArray(headline) ? headline : [headline];
    return (_jsxs("div", { ...outerProps, className: cn('flex flex-col', outerProps.className), children: [_jsx(DispatchHeader, { channel: channel }), _jsxs("div", { className: cn('relative min-h-0 min-w-0 flex-1', resolvedLayout === 'split'
                    ? 'grid grid-cols-[3fr_2fr]'
                    : 'grid grid-rows-[3fr_2fr]'), children: [_jsxs("div", { className: cn('relative overflow-hidden border-current/20', resolvedLayout === 'split' ? 'border-r' : 'border-b'), style: { backgroundColor: 'var(--background)' }, children: [_jsx(ImageDistortion, { autoPlay: autoPlay, src: src, tint: tint, tintStrength: tintStrength }), cornerMarks && (_jsxs(_Fragment, { children: [_jsx(CornerMark, { className: "top-3 left-3" }), _jsx(CornerMark, { className: "top-3 right-3" }), _jsx(CornerMark, { className: "bottom-3 left-3" }), _jsx(CornerMark, { className: "right-3 bottom-3" })] })), _jsx(ScanlineOverlay, {}), _jsx(Small, { className: "absolute bottom-4 left-4 z-1 opacity-80", children: "Hermes Agent" })] }), _jsxs("aside", { className: "relative flex min-w-0 flex-col justify-between gap-8 p-8", children: [_jsxs("div", { className: "flex flex-col gap-5", children: [eyebrow && (_jsxs("div", { className: "flex items-center gap-2", children: [_jsx("span", { className: "bg-midground/80 h-px flex-1" }), _jsx(Small, { className: "opacity-80", children: eyebrow })] })), children ?? (_jsxs(_Fragment, { children: [_jsx(Typography, { as: "h1", className: "text-[2.75em] leading-[0.95] font-bold tracking-[-0.01em]", expanded: true, children: headlineLines.map((line, i) => (_jsx("span", { className: "block", children: line }, `${line}-${i}`))) }), body && (_jsx("p", { className: "text-[1.0625em] leading-[1.5] tracking-normal normal-case opacity-60", children: body }))] }))] }), tags && tags.length > 0 && (_jsx("ul", { className: "flex flex-col gap-2 border-t border-current/15 pt-4", children: tags.map((tag, i) => (_jsxs("li", { className: "flex items-baseline justify-between gap-3", children: [_jsx(Small, { className: "font-courier opacity-40", children: String(i + 1).padStart(3, '0') }), _jsx(Small, { className: "opacity-80", children: tag }), _jsx("span", { className: "mx-1 h-px flex-1 translate-y-[-3px] border-b border-dotted border-current/25" }), _jsxs(Small, { className: "font-courier opacity-40", children: [String(i + 1).padStart(2, '0'), "/", String(tags.length).padStart(2, '0')] })] }, `${tag}-${i}`))) }))] })] }), _jsxs("footer", { className: "flex items-center justify-between gap-4 border-t border-current/20 px-6 py-3", children: [_jsxs(Small, { className: "opacity-70", children: [signature, _jsx(Blink, {})] }), _jsx(Small, { className: "font-courier opacity-40", children: seal })] })] }));
}
function DispatchHeader({ channel }) {
    const clock = useUtcClock();
    return (_jsxs("header", { className: "flex items-center justify-between gap-4 border-b border-current/20 px-6 py-3", children: [_jsxs("div", { className: "flex items-center gap-3", children: [_jsx("span", { className: "bg-midground size-2 rounded-sm opacity-70" }), _jsx(Small, { className: "opacity-70", children: channel })] }), _jsxs("div", { className: "flex items-center gap-4", children: [_jsx(ChannelDot, {}), _jsxs(Small, { className: "font-courier opacity-50", children: [clock, " UTC"] })] })] }));
}
function VibeContent({ autoPlay, channel, cornerMarks, signature, src, tint, tintStrength }) {
    // Absolute-inset-0 guarantees this fills the poster even when the outer
    // container uses aspect-ratio-derived height in a browser that doesn't
    // propagate that as a definite height for percentage-based children.
    return (_jsxs("div", { className: "absolute inset-0", children: [_jsx(ImageDistortion, { autoPlay: autoPlay, src: src, tint: tint, tintStrength: tintStrength }), cornerMarks && (_jsxs(_Fragment, { children: [_jsx(CornerMark, { className: "top-5 left-5" }), _jsx(CornerMark, { className: "top-5 right-5" }), _jsx(CornerMark, { className: "bottom-5 left-5" }), _jsx(CornerMark, { className: "right-5 bottom-5" })] })), _jsx(ScanlineOverlay, {}), channel && (_jsx(Small, { className: "absolute top-5 left-10 z-1 text-[0.75em] opacity-70", children: channel })), _jsx(Small, { className: "absolute right-10 bottom-5 z-1 text-[0.75em] opacity-80", children: signature })] }));
}
//# sourceMappingURL=poster.js.map