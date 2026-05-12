'use client';
import { jsx as _jsx, Fragment as _Fragment, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useRef, useState } from 'react';
const ease = (t) => (t < 0.5 ? 4 * t ** 3 : 1 - (-2 * t + 2) ** 3 / 2);
const easeD = (t) => (t < 0.5 ? 12 * t ** 2 : 6 * (-2 * t + 2) ** 2);
export function useAnimatedCount(from, rate, ts = new Date(), pausedAt) {
    const [value, setValue] = useState(from);
    const current = useRef(from);
    useEffect(() => {
        if (!rate) {
            return;
        }
        let raf;
        let prev = Date.now();
        let last = current.current;
        const target = () => from +
            Math.floor(((pausedAt
                ? Math.max(0, pausedAt.getTime() - ts.getTime())
                : Date.now() - ts.getTime()) /
                1e3) *
                rate *
                0.9);
        const tick = () => {
            const now = Date.now();
            current.current +=
                (target() - current.current) * Math.min(1, ((now - prev) / 1e3) * 3);
            prev = now;
            const rounded = Math.round(current.current);
            if (rounded !== last) {
                last = rounded;
                setValue(rounded);
            }
            if (!pausedAt || Math.abs(current.current - target()) > 0.5) {
                raf = requestAnimationFrame(tick);
            }
        };
        raf = requestAnimationFrame(tick);
        return () => cancelAnimationFrame(raf);
    }, [from, ts, rate, pausedAt]);
    return value;
}
export function AnimatedCount({ damping = 1, duration, pausedAt, rate = 0, value }) {
    const id = useRef(Math.random().toString(36).slice(2, 9));
    const prev = useRef(value);
    const [display, setDisplay] = useState(value);
    const [velocity, setVelocity] = useState(rate);
    useEffect(() => {
        if (!duration) {
            prev.current = value;
            return;
        }
        const start = prev.current;
        const delta = value - start;
        const dur = duration * damping;
        prev.current = value;
        if (!delta) {
            setVelocity(0);
            return;
        }
        const t0 = performance.now();
        const tick = (now) => {
            const t = Math.min((now - t0) / dur, 1);
            setDisplay(Math.round(start + delta * ease(t)));
            setVelocity(Math.abs((delta * easeD(t)) / dur) * 1000);
            t < 1 ? requestAnimationFrame(tick) : setVelocity(0);
        };
        const raf = requestAnimationFrame(tick);
        return () => cancelAnimationFrame(raf);
    }, [value, duration, rate, damping]);
    const digits = Math.round(duration ? display : value).toLocaleString().split('');
    const v = duration ? velocity : rate;
    const paused = !duration && pausedAt;
    const blurred = new Set(digits
        .map((c, i) => {
        if (!/\d/.test(c) || v <= 0 || paused) {
            return -1;
        }
        const pos = digits.filter(x => /\d/.test(x)).length -
            digits.slice(0, i + 1).filter(x => /\d/.test(x)).length;
        return (10 ** Math.max(0, pos) / v) * 1e3 < 500 ? i : -1;
    })
        .filter(i => i >= 0));
    return (_jsxs(_Fragment, { children: [_jsx("svg", { className: "pointer-events-none absolute size-0", children: _jsx("defs", { children: digits.map((_, i) => (_jsx("filter", { id: `blur-${id.current}-${i}`, suppressHydrationWarning: true, children: _jsx("feGaussianBlur", { stdDeviation: `0 ${blurred.has(i) ? 1 + [...blurred].indexOf(i) * 0.6 : 0}` }) }, i))) }) }), _jsx("span", { className: "inline-flex tabular-nums", children: digits.map((c, i) => (_jsx("span", { className: "inline-block text-center", style: {
                        filter: blurred.has(i) ? `url(#blur-${id.current}-${i})` : 'none',
                        width: c === ',' ? '0.4ch' : '1ch'
                    }, suppressHydrationWarning: true, children: c }, i))) })] }));
}
//# sourceMappingURL=animated-count.js.map