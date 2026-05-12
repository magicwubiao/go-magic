'use client';
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import * as Plot from '@observablehq/plot';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { cn } from '../../../utils';
import { accessor, CHART_MARGINS, CHART_STYLE, Crosshair, setupCrosshair, stylePlot, useDims, withChartBlend } from './utils';
export const LineChart = withChartBlend(({ backgroundColor: _, className, color: strokeColor, curve = 'natural', data = [], formatTooltip, formatX: formatXProp, formatY: formatYProp, series = 'series', showArea = false, x = 'label', xTicks, y = 'value', yDomain = [0, 0.5], yTicks = 4, ...props }) => {
    const ref = useRef(null);
    const plotRef = useRef(null);
    const [hovered, setHovered] = useState(null);
    const [crosshair, setCrosshair] = useState({ x: null });
    const dims = useDims(ref);
    const formatX = useCallback((v) => formatXProp?.(v) ??
        (v >= 1e3 ? `${v / 1e3}k` : `${v}`), [formatXProp]);
    const formatY = useCallback((v) => formatYProp?.(v) ?? `${Math.round(v * 100)}%`, [formatYProp]);
    const getX = useMemo(() => accessor(x), [x]);
    const getY = useMemo(() => accessor(y), [y]);
    const getZ = useCallback((d) => d[series], [series]);
    useEffect(() => {
        if (!ref.current ||
            !plotRef.current ||
            !data.length ||
            !dims.h ||
            !dims.w) {
            return;
        }
        plotRef.current.innerHTML = '';
        const hasSeries = data.some(d => d[series] !== undefined);
        const seriesIdx = hasSeries
            ? data.reduce((acc, d, i) => { var _a; return ((acc[_a = d[series]] ?? (acc[_a] = i)), acc); }, {})
            : {};
        const n = Object.keys(seriesIdx).length;
        const opacity = (d) => {
            if (!hasSeries) {
                return 1;
            }
            if (hovered) {
                return d[series] === hovered[series] ? 1 : 0.2;
            }
            return 1 - (seriesIdx[d[series]] / Math.max(n - 1, 1)) * 0.2;
        };
        const lineOpts = {
            curve,
            x: getX,
            y: getY,
            ...(hasSeries && { z: getZ })
        };
        const plot = Plot.plot({
            ...CHART_MARGINS,
            height: dims.h,
            marks: [
                ...(showArea
                    ? [
                        Plot.areaY(data, {
                            ...lineOpts,
                            fill: strokeColor,
                            fillOpacity: 0.15,
                            y1: yDomain[0]
                        })
                    ]
                    : []),
                Plot.lineY(data, {
                    ...lineOpts,
                    stroke: 'transparent',
                    strokeWidth: 16
                }),
                Plot.lineY(data, {
                    ...lineOpts,
                    stroke: strokeColor,
                    strokeOpacity: opacity,
                    strokeWidth: 1.5
                })
            ],
            style: { ...CHART_STYLE, fontStretch: 'expanded' },
            width: dims.w,
            x: { label: null, tickFormat: formatX, ticks: xTicks },
            y: {
                domain: yDomain,
                grid: true,
                label: null,
                tickFormat: formatY,
                ticks: yTicks
            }
        });
        plot.addEventListener('input', () => setHovered(plot.value));
        stylePlot(plot);
        plot.querySelectorAll('g[aria-label="line"] path').forEach(el => Object.assign(el.style, {
            transition: 'stroke-opacity 0.2s'
        }));
        plotRef.current.appendChild(plot);
        const cleanup = setupCrosshair(ref.current, data, d => getX(d), getY, yDomain, d => formatTooltip?.(d) ?? `${formatX(getX(d))}: ${formatY(getY(d))}`, setCrosshair, hasSeries ? d => getZ(d) : undefined);
        return () => {
            cleanup();
            plot.parentNode && plot.remove();
        };
    }, [
        curve,
        data,
        dims.h,
        dims.w,
        formatTooltip,
        formatX,
        formatY,
        getX,
        getY,
        getZ,
        hovered,
        series,
        showArea,
        strokeColor,
        xTicks,
        yDomain,
        yTicks
    ]);
    return (_jsxs("div", { className: cn('relative aspect-4/1 w-full overflow-clip', className), ref: ref, ...props, children: [_jsx("div", { className: "absolute inset-0", ref: plotRef }), _jsx(Crosshair, { color: strokeColor, containerWidth: dims.w, height: dims.h, ...crosshair })] }));
});
//# sourceMappingURL=line-chart.js.map