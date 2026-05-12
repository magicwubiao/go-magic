'use client';
import { jsx as _jsx } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import spinners from 'unicode-animations';
import { cn } from '../../utils';
/**
 * Braille unicode spinner. Renders the active frame of a `unicode-animations`
 * sequence inside an inline `<span>`, advancing on the spinner's own interval.
 *
 * Inherits font color and font size from its parent — apply Tailwind utilities
 * (e.g. `text-warning`, `text-base`) via `className` to style.
 *
 * Decorative by default. Pass `aria-label` (and optionally `role="status"`) when
 * the spinner has no surrounding loading text and screen readers need to know
 * something is loading.
 */
export function Spinner({ className, name = 'braille', style, ...props }) {
    const [frame, setFrame] = useState(0);
    const animation = spinners[name];
    useEffect(() => {
        const id = setInterval(() => setFrame(f => (f + 1) % animation.frames.length), animation.interval);
        return () => clearInterval(id);
    }, [animation.frames.length, animation.interval]);
    return (_jsx("span", { "aria-hidden": props['aria-label'] ? undefined : true, className: cn('font-mono inline-block leading-none tabular-nums', className), style: style, ...props, children: animation.frames[frame] }));
}
//# sourceMappingURL=spinner.js.map