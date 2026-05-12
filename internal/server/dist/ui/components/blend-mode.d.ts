import { type PolyProps } from '../../utils';
declare const LAYER_KEYS: {
    readonly bg: "bgColor";
    readonly fg: "fgColor";
    readonly mg: "mgColor";
};
type Layer = keyof typeof LAYER_KEYS;
type LayerSpec = `${Layer}/${number}` | Layer;
export declare const useBlendMode: (opts?: BlendModeOpts) => BlendColors;
export declare const withBlendMode: <P extends BlendColors>(Component: React.ComponentType<P>, opts?: BlendModeOpts) => {
    (props: Omit<P, keyof BlendColors> & Partial<BlendModeOpts>): import("react/jsx-runtime").JSX.Element;
    displayName: string;
};
export declare const BlendMode: import("../..").PolyComponent<"div", BlendModeOwnProps>;
interface BlendModeOwnProps extends BlendModeOpts {
    children?: ((colors: BlendColors) => React.ReactNode) | React.ReactNode;
}
export interface BlendColors {
    backgroundColor?: string;
    color?: string;
}
interface BlendModeOpts {
    against?: Layer;
    background?: LayerSpec | string;
    color?: LayerSpec | string;
}
export type BlendModeProps<T extends React.ElementType = 'div'> = PolyProps<T, BlendModeOwnProps>;
export {};
//# sourceMappingURL=blend-mode.d.ts.map