import { type TypographyProps } from './typography';
export declare const Progress: ({ animate, barProps, children, className, speed, value, ...props }: ProgressProps) => import("react/jsx-runtime").JSX.Element;
interface ProgressProps extends React.ComponentProps<'div'> {
    animate?: boolean;
    barProps?: TypographyProps<'span'>;
    speed?: number;
    value: number;
}
export {};
//# sourceMappingURL=progress.d.ts.map