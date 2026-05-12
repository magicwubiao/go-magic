'use client';
import { jsx as _jsx } from "react/jsx-runtime";
import { useSmoothControls } from '../../../hooks/use-smooth-controls';
import { cn } from '../../../utils';
import { hexToRgb } from '../../../utils/color';
import { BLEND_MODES } from '.';
export function Vignette({ className, style }) {
    const c = useSmoothControls('Effects/Vignette', {
        blend: { options: BLEND_MODES, value: 'lighten' },
        bottomLeft: { max: 1, min: 0, step: 0.01, value: 0 },
        bottomRight: { max: 1, min: 0, step: 0.01, value: 0 },
        color: { value: '#ffbd38' },
        enabled: { value: true },
        opacity: { max: 1, min: 0, step: 0.01, value: 0.22 },
        size: { max: 1, min: 0, step: 0.01, value: 1 },
        topLeft: { max: 1, min: 0, step: 0.01, value: 0.35 },
        topRight: { max: 1, min: 0, step: 0.01, value: 0 }
    }, { collapsed: true });
    if (!c.enabled)
        return null;
    const rgb = hexToRgb(c.color);
    const s = c.size * 60;
    const grad = (x, y, i) => i > 0 &&
        `radial-gradient(ellipse at ${x}% ${y}%, rgba(${rgb},0) ${s}%, rgba(${rgb},${i}) 100%)`;
    const bg = [
        grad(0, 0, c.topLeft),
        grad(100, 0, c.topRight),
        grad(0, 100, c.bottomLeft),
        grad(100, 100, c.bottomRight)
    ].filter(Boolean);
    if (!bg.length)
        return null;
    return (_jsx("div", { className: cn('h-full w-full', className), style: {
            background: bg.join(', '),
            mixBlendMode: c.blend,
            opacity: c.opacity,
            ...style
        } }));
}
//# sourceMappingURL=vignette.js.map