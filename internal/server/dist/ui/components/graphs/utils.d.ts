import { type ComponentType, type HTMLAttributes, type RefObject } from 'react';
import { type BlendColors, type BlendModeProps } from '../blend-mode';
export declare const accessor: <T, R>(key: ((d: T) => R) | keyof T) => (d: T) => R;
export declare const CHART_MARGINS: {
    readonly marginBottom: 24;
    readonly marginLeft: 36;
    readonly marginRight: 12;
    readonly marginTop: 8;
};
export declare const CHART_STYLE: {
    readonly background: "transparent";
    readonly color: "var(--midground)";
    readonly fontFamily: "var(--font-mono), monospace";
    readonly fontSize: "11px";
    readonly overflow: "hidden";
};
export declare const stylePlot: (plot: HTMLElement) => void;
export declare const useDims: (ref: RefObject<HTMLElement | null>) => {
    h: number;
    w: number;
};
export declare const Crosshair: ({ color, containerWidth, height, points, x }: CrosshairState & {
    color?: string;
    containerWidth: number;
    height: number;
}) => import("react/jsx-runtime").JSX.Element | null;
export declare const setupCrosshair: <T extends DataPoint>(container: HTMLElement, data: T[], getX: (d: T) => number, getY: (d: T) => number, yDomain: [number, number], formatTooltip: (d: T) => string, onUpdate: (state: CrosshairState) => void, getZ?: (d: T) => unknown) => () => void;
export declare const withChartBlend: <P extends BlendModeProps>(Component: ComponentType<P>) => {
    (props: Omit<P, keyof BlendColors>): import("react/jsx-runtime").JSX.Element;
    displayName: string;
};
export type DataPoint = Record<string, unknown>;
export interface CrosshairPoint {
    dotY: number;
    tooltip: string;
}
export interface CrosshairState {
    points?: CrosshairPoint[];
    x: null | number;
}
export interface ChartProps<T = DataPoint> extends Omit<HTMLAttributes<HTMLDivElement>, 'children'> {
    data?: T[];
    formatTooltip?: (d: T) => string;
    formatX?: (v: unknown) => string;
    formatY?: (v: number) => string;
    height?: number;
    x?: ((d: T) => unknown) | keyof T;
    xTicks?: number | number[];
    y?: ((d: T) => number) | keyof T;
    yDomain?: [number, number];
    yTicks?: number | number[];
}
//# sourceMappingURL=utils.d.ts.map