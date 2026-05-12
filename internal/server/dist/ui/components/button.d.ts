import { type VariantProps } from 'class-variance-authority';
declare const buttonVariants: (props?: ({
    destructive?: boolean | null | undefined;
    ghost?: boolean | null | undefined;
    invert?: boolean | null | undefined;
    outlined?: boolean | null | undefined;
    size?: "default" | "icon" | "sm" | "xs" | null | undefined;
} & import("class-variance-authority/types").ClassProp) | undefined) => string;
export declare const Button: ({ children, className, destructive, ghost, invert, outlined, prefix, size, suffix, ...props }: ButtonProps) => import("react/jsx-runtime").JSX.Element;
interface ButtonProps extends Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, 'prefix' | 'suffix'>, VariantProps<typeof buttonVariants> {
    prefix?: React.ReactNode;
    suffix?: React.ReactNode;
}
export {};
//# sourceMappingURL=button.d.ts.map