'use client';
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import * as Plot from '@observablehq/plot';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { cn } from '../../../utils';
import { accessor, CHART_MARGINS, CHART_STYLE, Crosshair, setupCrosshair, stylePlot, useDims, withChartBlend } from './utils';
export const BarChart = withChartBlend(({ backgroundColor: _, className, color: fillColor, data = [], formatTooltip, formatX: formatXProp, formatY: formatYProp, x = 'label', xDomain, xTicks = [0, 50000, 100000], y = 'value', yDomain = [0, 10], yTicks = [10, 8, 4, 2], ...props }) => {
    const ref = useRef(null);
    const plotRef = useRef(null);
    const [crosshair, setCrosshair] = useState({ x: null });
    const dims = useDims(ref);
    const formatX = useCallback((v) => formatXProp?.(v) ??
        (typeof v === 'number' ? v.toLocaleString('en-US') : String(v)), [formatXProp]);
    const formatY = useCallback((v) => formatYProp?.(v) ?? String(v), [formatYProp]);
    const getX = useMemo(() => accessor(x), [x]);
    const getY = useMemo(() => accessor(y), [y]);
    useEffect(() => {
        if (!ref.current ||
            !plotRef.current ||
            !data.length ||
            !dims.h ||
            !dims.w) {
            return;
        }
        plotRef.current.innerHTML = '';
        const [xMin, xMax] = [
            xDomain?.[0] ?? 0,
            xDomain?.[1] ?? Math.max(...data.map(d => getX(d)))
        ];
        const plot = Plot.plot({
            ...CHART_MARGINS,
            height: dims.h,
            marks: [
                Plot.rectY(data, {
                    fill: fillColor ?? 'currentColor',
                    fillOpacity: 0.3,
                    interval: (xMax - xMin) / data.length,
                    x: getX,
                    y: getY
                }),
                Plot.axisX({ tickFormat: formatX, ticks: xTicks })
            ],
            style: CHART_STYLE,
            width: dims.w,
            x: { domain: [xMin, xMax], label: null, type: 'linear' },
            y: {
                domain: yDomain,
                grid: true,
                label: null,
                tickFormat: formatY,
                ticks: yTicks
            }
        });
        stylePlot(plot);
        plotRef.current.appendChild(plot);
        const cleanup = setupCrosshair(ref.current, data, d => getX(d), getY, yDomain, d => formatTooltip?.(d) ?? `${formatX(getX(d))}: ${formatY(getY(d))}`, setCrosshair);
        return cleanup;
    }, [
        data,
        dims.h,
        dims.w,
        fillColor,
        formatTooltip,
        formatX,
        formatY,
        getX,
        getY,
        xDomain,
        xTicks,
        yDomain,
        yTicks
    ]);
    return (_jsxs("div", { className: cn('relative aspect-4/1 w-full overflow-clip', className), ref: ref, ...props, children: [_jsx("div", { className: "absolute inset-0", ref: plotRef }), _jsx(Crosshair, { color: fillColor, containerWidth: dims.w, height: dims.h, ...crosshair })] }));
});
//# sourceMappingURL=bar-chart.js.map