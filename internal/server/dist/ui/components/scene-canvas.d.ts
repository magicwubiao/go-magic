export declare function SceneCanvas({ camera, children, className, contained, noEvents, style }: SceneCanvasProps): import("react/jsx-runtime").JSX.Element;
interface SceneCanvasProps {
    camera?: {
        far?: number;
        near?: number;
        position?: [number, number, number];
        zoom?: number;
    };
    children: () => React.ReactNode;
    className?: string;
    contained?: boolean;
    noEvents?: boolean;
    style?: React.CSSProperties;
}
export {};
//# sourceMappingURL=scene-canvas.d.ts.map