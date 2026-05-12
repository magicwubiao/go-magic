'use client';
import { jsx as _jsx, Fragment as _Fragment } from "react/jsx-runtime";
import { useStore } from '@nanostores/react';
import { createElement, useMemo } from 'react';
import { getControlAtom } from '../../hooks/use-smooth-controls';
import { cn, polyRef } from '../../utils';
import { colorDodge, colorMix } from '../../utils/color';
const LAYER_KEYS = { bg: 'bgColor', fg: 'fgColor', mg: 'mgColor' };
const parseSpec = (spec) => {
    const [layer, alpha] = spec.split('/');
    return [layer, alpha ? parseFloat(alpha) : undefined];
};
const useControlColor = (key, fallback) => {
    const atom = getControlAtom('Lens', key);
    // eslint-disable-next-line react-hooks/rules-of-hooks
    return (atom ? useStore(atom) : undefined) ?? fallback;
};
const useBlend = (against, spec) => {
    const layerKey = spec?.split('/')[0];
    const isLayerSpec = layerKey && layerKey in LAYER_KEYS;
    const [target, alpha] = isLayerSpec
        ? parseSpec(spec)
        : [undefined, undefined];
    const againstColor = useControlColor(LAYER_KEYS[against], '#041c1c');
    const fgColor = useControlColor(LAYER_KEYS.fg, '#ffe6cb');
    const mgColor = useControlColor(LAYER_KEYS.mg, '#ffe6cb');
    const bgColor = useControlColor(LAYER_KEYS.bg, '#ffe6cb');
    const targetColor = target
        ? target === 'fg'
            ? fgColor
            : target === 'mg'
                ? mgColor
                : bgColor
        : spec;
    return useMemo(() => {
        if (!spec || !targetColor) {
            return undefined;
        }
        const result = colorDodge(againstColor, targetColor);
        return alpha != null ? colorMix(result, alpha) : result;
    }, [spec, againstColor, targetColor, alpha]);
};
export const useBlendMode = (opts = {}) => {
    const { against = 'bg', background, color } = opts;
    return {
        backgroundColor: useBlend(against, background),
        color: useBlend(against, color)
    };
};
export const withBlendMode = (Component, opts) => {
    const Wrapped = (props) => {
        const { against, background, color, ...rest } = props;
        const colors = useBlendMode({
            against: against ?? opts?.against,
            background: background ?? opts?.background,
            color: color ?? opts?.color
        });
        return _jsx(Component, { ...rest, ...colors });
    };
    Wrapped.displayName = `withBlendMode(${Component.displayName ?? Component.name ?? 'Component'})`;
    return Wrapped;
};
export const BlendMode = polyRef(({ against, as, background, children, className, color, style, ...rest }, ref) => {
    const colors = useBlendMode({ against, background, color });
    if (typeof children === 'function') {
        return _jsx(_Fragment, { children: children(colors) });
    }
    return createElement((as ?? 'div'), {
        ...rest,
        children,
        className: cn(className),
        ref,
        style: { ...colors, ...style }
    });
});
//# sourceMappingURL=blend-mode.js.map