'use client';
import { jsx as _jsx, Fragment as _Fragment, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { useSmoothControls } from '../../../hooks/use-smooth-controls';
export const accessor = (key) => typeof key === 'function' ? key : (d) => d[key];
export const CHART_MARGINS = {
    marginBottom: 24,
    marginLeft: 36,
    marginRight: 12,
    marginTop: 8
};
export const CHART_STYLE = {
    background: 'transparent',
    color: 'var(--midground)',
    fontFamily: 'var(--font-mono), monospace',
    fontSize: '11px',
    overflow: 'hidden'
};
export const stylePlot = (plot) => {
    plot.querySelectorAll('[aria-label*="grid"] line').forEach(el => Object.assign(el.style, {
        stroke: 'currentColor',
        strokeDasharray: '2,4',
        strokeOpacity: '0.3'
    }));
    plot.querySelectorAll('text').forEach(el => Object.assign(el.style, {
        fontSize: '11px',
        fontWeight: '600'
    }));
    plot
        .querySelectorAll('[aria-label*="label"] text')
        .forEach(el => (el.style.opacity = '0.4'));
    const svg = plot.querySelector('svg');
    svg && (svg.style.display = 'block');
};
export const useDims = (ref) => {
    const [dims, setDims] = useState({ h: 0, w: 0 });
    useEffect(() => {
        if (!ref.current) {
            return;
        }
        const update = () => {
            const { height: h, width: w } = ref.current.getBoundingClientRect();
            const [rh, rw] = [Math.round(h), Math.round(w)];
            rh &&
                rw &&
                setDims(st => (st.h === rh && st.w === rw ? st : { h: rh, w: rw }));
        };
        update();
        const ro = new ResizeObserver(update);
        ro.observe(ref.current);
        return () => ro.disconnect();
    }, [ref]);
    return dims;
};
export const Crosshair = ({ color = 'var(--foreground)', containerWidth, height, points, x }) => {
    if (x === null) {
        return null;
    }
    const nearRight = x > containerWidth * 0.7;
    return (_jsxs(_Fragment, { children: [_jsx("div", { className: "pointer-events-none absolute top-0 w-px", style: { background: color, height, left: x, opacity: 0.4 } }), points?.map((pt, i) => (_jsx("div", { className: "pointer-events-none absolute size-2 -translate-x-1/2 -translate-y-1/2 rounded-full", style: { background: color, left: x, top: pt.dotY } }, i))), points?.map((pt, i) => (_jsx("div", { className: "tooltip absolute -translate-y-1/2", style: {
                    left: nearRight ? undefined : x + 12,
                    right: nearRight ? containerWidth - x + 12 : undefined,
                    top: pt.dotY
                }, children: pt.tooltip }, i)))] }));
};
export const setupCrosshair = (container, data, getX, getY, yDomain, formatTooltip, onUpdate, getZ) => {
    if (!data.length) {
        return () => { };
    }
    const { marginBottom, marginLeft, marginRight, marginTop } = CHART_MARGINS;
    const seriesMap = data.reduce((m, d) => {
        const key = getZ?.(d) ?? '__single__';
        m.set(key, [...(m.get(key) ?? []), d]);
        return m;
    }, new Map());
    const sortedSeries = [...seriesMap.values()].map(s => [...s].sort((a, b) => getX(a) - getX(b)));
    const allX = data.map(getX);
    const [xMin, xMax] = [Math.min(...allX), Math.max(...allX)];
    const onMove = (e) => {
        const rect = container.getBoundingClientRect();
        const [localX, localY] = [e.clientX - rect.left, e.clientY - rect.top];
        if (localX < 0 ||
            localX > rect.width ||
            localY < 0 ||
            localY > rect.height) {
            return onUpdate({ x: null });
        }
        const [chartLeft, chartRight] = [marginLeft, rect.width - marginRight];
        const [chartTop, chartBottom] = [marginTop, rect.height - marginBottom];
        if (localX < chartLeft || localX > chartRight) {
            return onUpdate({ x: null });
        }
        const pct = (localX - chartLeft) / (chartRight - chartLeft);
        const xVal = xMin + pct * (xMax - xMin);
        const points = sortedSeries.map(sorted => {
            const idx = sorted.findIndex(d => getX(d) >= xVal);
            const [closest, yVal] = idx <= 0
                ? [sorted[0], getY(sorted[0])]
                : idx >= sorted.length
                    ? [sorted.at(-1), getY(sorted.at(-1))]
                    : (() => {
                        const [left, right] = [sorted[idx - 1], sorted[idx]];
                        const t = (xVal - getX(left)) / (getX(right) - getX(left));
                        return [
                            t < 0.5 ? left : right,
                            getY(left) + t * (getY(right) - getY(left))
                        ];
                    })();
            const yPct = (yVal - yDomain[0]) / (yDomain[1] - yDomain[0]);
            return {
                dotY: chartBottom - yPct * (chartBottom - chartTop),
                tooltip: formatTooltip(closest)
            };
        });
        onUpdate({ points, x: localX });
    };
    document.addEventListener('mousemove', onMove);
    return () => document.removeEventListener('mousemove', onMove);
};
export const withChartBlend = (Component) => {
    const Wrapped = (props) => {
        const { color } = useSmoothControls('Charts', { color: { value: '#709fea' } }, { collapsed: true });
        return _jsx(Component, { ...props, color: color });
    };
    Wrapped.displayName = `withChartBlend(${Component.displayName ?? Component.name})`;
    return Wrapped;
};
//# sourceMappingURL=utils.js.map