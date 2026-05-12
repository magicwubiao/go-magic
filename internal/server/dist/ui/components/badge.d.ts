export declare const Badge: ({ className, style, tone, ...props }: BadgeProps) => import("react/jsx-runtime").JSX.Element;
type Tone = 'default' | 'destructive' | 'outline' | 'secondary' | 'success' | 'warning';
interface BadgeProps extends Omit<React.HTMLAttributes<HTMLSpanElement>, 'color'> {
    tone?: Tone;
}
export {};
//# sourceMappingURL=badge.d.ts.map