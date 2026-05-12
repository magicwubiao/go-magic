export declare const BLEND_MODES: React.CSSProperties["mixBlendMode"][];
export declare const LENS_0: {
    Globe: {
        innerColor: string;
        innerOpacity: number;
        outerColor: string;
    };
    Lens: {
        bgBlend: string;
        bgColor: string;
        bgOpacity: number;
        fgColor: string;
        fgOpacity: number;
        fillerOpacity: number;
        mgColor: string;
        mgOpacity: number;
    };
};
export declare const LENS_5I: {
    Globe: {
        innerColor: string;
        innerOpacity: number;
        outerColor: string;
    };
    Lens: {
        bgBlend: string;
        bgColor: string;
        bgOpacity: number;
        fgColor: string;
        fgOpacity: number;
        fillerOpacity: number;
        mgColor: string;
        mgOpacity: number;
    };
};
export declare const lens0: (l?: Partial<typeof LENS_0.Lens>, g?: Partial<typeof LENS_0.Globe>) => LensPreset;
export declare const lens5i: (l?: Partial<typeof LENS_5I.Lens>, g?: Partial<typeof LENS_5I.Globe>) => LensPreset;
export declare const LENSES: [string, LensPreset][];
export declare const applyLens: (preset: LensPreset, animate?: boolean) => void;
export declare const $lightMode: import("nanostores").PreinitializedWritableAtom<boolean> & object;
export declare const toggleLens: () => void;
export declare function Overlays({ dark, initial }: OverlaysProps): import("react/jsx-runtime").JSX.Element;
export interface LensPreset {
    Globe: typeof LENS_0.Globe;
    Lens: typeof LENS_0.Lens;
}
interface OverlaysProps {
    dark?: boolean;
    /**
     * Exact preset to seed the internal Leva controls with. When omitted the
     * component falls back to `LENS_0` / `LENS_5I` based on `dark`. Pass the
     * actual preset from a host (e.g. Storybook toolbar) to guarantee the
     * first-paint colors match the selected lens without needing a followup
     * `applyLens` that can be lost in useSmoothControls' startup window.
     */
    initial?: LensPreset;
}
export {};
//# sourceMappingURL=index.d.ts.map