'use client';
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useRef } from 'react';
const INTERACTIVE = 'a, button, [role="button"], input, textarea, select, [data-cursor]';
const HAND = 'M6.84 21.83c-.47-.6-1.05-1.82-2.07-3.34-.58-.83-2.01-2.41-2.45-3.23a2.1 2.1 0 0 1-.25-1.67 2.2 2.2 0 0 1 2.39-1.67c.85.18 1.63.6 2.25 1.2.43.41.82.85 1.18 1.32.27.34.33.47.63.85.3.39.5.77.35.2-.11-.83-.31-2.23-.6-3.48-.21-.95-.26-1.1-.46-1.82s-.32-1.32-.54-2.13c-.2-.8-.35-1.62-.46-2.44a4.7 4.7 0 0 1 .43-3.08c.58-.55 1.44-.7 2.17-.37a4.4 4.4 0 0 1 1.57 2.17c.43 1.07.72 2.19.86 3.33.27 1.67.79 4.1.8 4.6 0-.61-.11-1.91 0-2.5.12-.6.54-1.1 1.12-1.33.5-.15 1.02-.19 1.53-.1.52.1.98.4 1.29.83.38.98.6 2 .63 3.05.04-.91.2-1.82.47-2.7.28-.39.68-.67 1.15-.8.55-.1 1.11-.1 1.66 0 .46.15.85.44 1.14.82.35.88.56 1.82.63 2.77 0 .23.12-.65.48-1.24a1.67 1.67 0 1 1 3.17 1.07v3.77c-.06.97-.2 1.94-.4 2.9-.29.85-.7 1.65-1.2 2.38-.8.9-1.48 1.92-1.98 3.02a6.67 6.67 0 0 0 .03 3.2c-.68.07-1.37.07-2.05 0-.65-.1-1.45-1.4-1.67-1.8a.63.63 0 0 0-1.13 0c-.37.64-1.18 1.79-1.75 1.85-1.12.14-3.42 0-5.23 0 0 0 .3-1.66-.39-2.27-.68-.6-1.38-1.3-1.9-1.76l-1.4-1.6Z';
export function Cursor({ scale = 0.8 }) {
    const $root = useRef(null);
    const $arrow = useRef(null);
    const $ptr = useRef(null);
    useEffect(() => {
        const [root, arrow, ptr] = [$root.current, $arrow.current, $ptr.current];
        if (!root || !arrow || !ptr) {
            return;
        }
        const on = (el, ev, fn, opts) => {
            el.addEventListener(ev, fn, opts);
            return () => el.removeEventListener(ev, fn);
        };
        return [
            on(document, 'mousemove', (e) => {
                const { clientX: x, clientY: y } = e;
                root.style.translate = `${x}px ${y}px`;
                root.style.opacity = '1';
            }, { passive: true }),
            on(document, 'mouseover', (e) => {
                const isPtr = !!e.target.closest?.(INTERACTIVE);
                arrow.style.opacity = isPtr ? '0' : '1';
                ptr.style.opacity = isPtr ? '1' : '0';
            }, { passive: true }),
            on(document, 'mousedown', () => {
                root.style.transform = 'translate(1px, 1px)';
            }),
            on(document, 'mouseup', () => {
                root.style.transform = '';
            }),
            on(document.documentElement, 'mouseleave', () => {
                root.style.opacity = '0';
            }),
            on(document.documentElement, 'mouseenter', () => {
                root.style.opacity = '1';
            })
        ].reduce((_, fn) => fn, undefined);
    }, []);
    return (_jsxs("div", { "aria-hidden": true, ref: $root, style: {
            filter: 'drop-shadow(1px 2px 0 #000)',
            height: 32 * scale,
            left: 0,
            opacity: 0,
            pointerEvents: 'none',
            position: 'fixed',
            top: 0,
            width: 32 * scale,
            willChange: 'translate',
            zIndex: 9999
        }, children: [_jsx("div", { ref: $arrow, style: { inset: 0, position: 'absolute' }, children: _jsx("svg", { viewBox: "0 0 16 16", children: _jsx("path", { d: "M1 1L1 14L5 10L8 15L10 14L7 9L12 9L1 1Z", fill: "#fff", stroke: "#000", strokeLinejoin: "round", strokeWidth: 1 }) }) }), _jsx("div", { ref: $ptr, style: { inset: 0, opacity: 0, position: 'absolute' }, children: _jsx("svg", { viewBox: "0 0 28 29", children: _jsx("path", { d: HAND, fill: "#fff", stroke: "#000", strokeLinejoin: "round", strokeWidth: 2, style: { paintOrder: 'stroke fill' } }) }) })] }));
}
//# sourceMappingURL=cursor.js.map