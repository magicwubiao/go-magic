import { type VariantProps } from 'class-variance-authority';
import { type PolyProps } from '../../../utils';
declare const typographyVariants: (props?: ({
    compressed?: boolean | null | undefined;
    courier?: boolean | null | undefined;
    expanded?: boolean | null | undefined;
    mondwest?: boolean | null | undefined;
    mono?: boolean | null | undefined;
    sans?: boolean | null | undefined;
    variant?: "lg" | "md" | "sm" | "xl" | null | undefined;
} & import("class-variance-authority/types").ClassProp) | undefined) => string;
export declare const Typography: import("../../..").PolyComponent<"span", OwnProps>;
type OwnProps = VariantProps<typeof typographyVariants>;
export type TypographyProps<T extends React.ElementType = 'span'> = PolyProps<T, OwnProps>;
export {};
//# sourceMappingURL=index.d.ts.map