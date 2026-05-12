export declare function ImageDistortion({ active, autoPlay, className, fallbackClassName, src, style, tint, tintStrength }: ImageDistortionProps): import("react/jsx-runtime").JSX.Element;
export type AutoPlayPattern = 'aggressive' | 'gentle' | 'slash';
interface ImageDistortionProps {
    active?: boolean;
    /**
     * Drive the distortion with a choreographed motion pattern instead of
     * waiting for a real pointer. Useful for posters, social clips, and any
     * context where the image needs to feel alive on its own.
     */
    autoPlay?: AutoPlayPattern;
    className?: string;
    fallbackClassName?: string;
    src: string;
    style?: React.CSSProperties;
    tint?: string;
    tintStrength?: {
        active: number;
        inactive: number;
    };
}
export {};
//# sourceMappingURL=image-distortion.d.ts.map