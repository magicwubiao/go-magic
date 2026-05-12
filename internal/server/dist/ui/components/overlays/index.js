'use client';
import { jsx as _jsx, Fragment as _Fragment, jsxs as _jsxs } from "react/jsx-runtime";
import { atom } from 'nanostores';
import { useEffect } from 'react';
import { setControlValue, useSmoothControls } from '../../../hooks/use-smooth-controls';
import { colorMix } from '../../../utils/color';
import fillerBg from '../../../assets/filler-bg0.jpg';
import { Glitch } from './glitch';
import { Greys } from './greys';
import { Noise } from './noise';
import { Vignette } from './vignette';
const LAYER = 'pointer-events-none fixed inset-0';
export const BLEND_MODES = [
    'overlay',
    'multiply',
    'screen',
    'difference',
    'exclusion',
    'color-dodge',
    'color-burn',
    'hard-light',
    'soft-light',
    'darken',
    'lighten'
];
export const LENS_0 = {
    Globe: { innerColor: '#170d02', innerOpacity: 0.1, outerColor: '#FFAC02' },
    Lens: {
        bgBlend: 'difference',
        bgColor: '#041C1C',
        bgOpacity: 1,
        fgColor: '#FFFFFF',
        fgOpacity: 0,
        fillerOpacity: 0.033,
        mgColor: '#ffe6cb',
        mgOpacity: 1
    }
};
export const LENS_5I = {
    Globe: { innerColor: '#170d02', innerOpacity: 0.3, outerColor: '#FFAC02' },
    Lens: {
        bgBlend: 'multiply',
        bgColor: '#170d02',
        bgOpacity: 1,
        fgColor: '#FFFFFF',
        fgOpacity: 1,
        fillerOpacity: 0.06,
        mgColor: '#FFAC02',
        mgOpacity: 1
    }
};
export const lens0 = (l, g) => ({
    Globe: { ...LENS_0.Globe, ...g },
    Lens: { ...LENS_0.Lens, ...l }
});
// The Hermes light-mode look is produced by a fullscreen opaque-white
// `mix-blend-mode: difference` foreground layer that inverts everything.
// Colored lenses that want a "white + accent" look MUST be built from
// LENS_5I, not LENS_0 — otherwise `bgBlend: 'difference'` + an opaque
// colored bg + active fg inversion land halfway between dark and light
// mode and produce a muddy warm wash instead of a clean inversion.
export const lens5i = (l, g) => ({
    Globe: { ...LENS_5I.Globe, ...g },
    Lens: { ...LENS_5I.Lens, ...l }
});
// Accent colors are the *pre-inversion* source; after the difference FG
// layer they read as their visual complement. e.g. `#FFAC02` (orange)
// renders as blue #0053FD on screen — that's the default LENS_5I accent.
export const LENSES = [
    ['0', LENS_0],
    ['1', lens0({ bgColor: '#0A1F1F' })],
    ['2', lens0({ bgColor: '#0E0313', mgColor: '#e6cbff' })],
    ['3', lens5i({ mgColor: '#FFAC02' })],
    ['4', lens5i({ bgColor: '#0E0313', mgColor: '#FF5500' })],
    ['5', lens0({ bgColor: '#1540B1', bgOpacity: 0.7 })],
    ['5i', LENS_5I],
    ['6', lens5i({ bgColor: '#170D02', mgColor: '#00E5FF' })]
];
export const applyLens = (preset, animate = false) => Object.entries(preset).forEach(([g, v]) => Object.entries(v).forEach(([k, val]) => setControlValue(g, k, val, { animate })));
export const $lightMode = atom(true);
export const toggleLens = () => {
    const isLight = $lightMode.get();
    const next = isLight ? LENS_0 : LENS_5I;
    $lightMode.set(!isLight);
    applyLens(next, true);
};
export function Overlays({ dark, initial }) {
    // `initial` lets the host (e.g. Storybook) seed the Leva/atom state with
    // the *exact* lens preset the user selected, avoiding a one-cycle lag
    // where useSmoothControls emits old colors for the first paint (and, on
    // Storybook's fast iframe reload, sometimes never catches up because
    // useControls' ready-gate swallows the instant color writes).
    const base = initial?.Lens ?? (dark ? LENS_0.Lens : LENS_5I.Lens);
    const lens = useSmoothControls('Lens', {
        bgBlend: { options: BLEND_MODES, value: base.bgBlend },
        bgColor: { value: base.bgColor },
        bgOpacity: { max: 1, min: 0, step: 0.01, value: base.bgOpacity },
        fgBlend: { options: BLEND_MODES, value: 'difference' },
        fgColor: { value: base.fgColor },
        fgOpacity: { max: 1, min: 0, step: 0.01, value: base.fgOpacity },
        fillerBlend: { options: BLEND_MODES, value: 'difference' },
        fillerOpacity: { max: 1, min: 0, step: 0.01, value: base.fillerOpacity },
        mgColor: { value: base.mgColor },
        mgOpacity: { max: 1, min: 0, step: 0.01, value: base.mgOpacity }
    }, { collapsed: false });
    useEffect(() => {
        $lightMode.set(!dark);
    }, [dark]);
    useEffect(() => {
        const s = document.documentElement.style;
        for (const [name, color, alpha] of [
            ['foreground', lens.fgColor, lens.fgOpacity],
            ['midground', lens.mgColor, lens.mgOpacity],
            ['background', lens.bgColor, lens.bgOpacity]
        ]) {
            s.setProperty(`--${name}`, colorMix(color, alpha));
            s.setProperty(`--${name}-base`, color);
            s.setProperty(`--${name}-alpha`, `${alpha}`);
        }
    }, [lens]);
    useEffect(() => {
        const handle = (e) => e.key === 'x' && toggleLens();
        window.addEventListener('keydown', handle);
        return () => window.removeEventListener('keydown', handle);
    }, []);
    // NOTE: z-index is inlined because Tailwind's JIT sometimes doesn't emit
    // these non-default utilities (e.g. in Storybook's isolated content
    // scan), which silently collapses the overlay stack to DOM order and
    // breaks the mix-blend-mode inversion — producing a muddy warm wash
    // instead of the intended clean black/white inversion.
    return (_jsxs(_Fragment, { children: [_jsx(Noise, { className: LAYER, style: { zIndex: 101 } }), _jsx("div", { className: LAYER, style: {
                    backgroundColor: colorMix(lens.fgColor, lens.fgOpacity),
                    mixBlendMode: lens.fgBlend,
                    zIndex: 100
                } }), _jsx(Vignette, { className: LAYER, style: { zIndex: 99 } }), _jsx(Greys, { className: LAYER, style: { zIndex: 200 } }), _jsx(Glitch, { className: LAYER, style: { zIndex: 201 } }), _jsx("div", { className: LAYER, style: {
                    mixBlendMode: lens.fillerBlend,
                    opacity: lens.fillerOpacity,
                    zIndex: 2
                }, children: _jsx("img", { alt: "", className: "h-[150dvh] w-auto min-w-dvw object-cover object-top-left invert", fetchPriority: "low", src: fillerBg.src }) }), _jsx("div", { className: LAYER, style: {
                    backgroundColor: colorMix(lens.bgColor, lens.bgOpacity),
                    mixBlendMode: lens.bgBlend,
                    zIndex: 1
                } })] }));
}
//# sourceMappingURL=index.js.map