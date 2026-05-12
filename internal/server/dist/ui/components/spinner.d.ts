import { type CSSProperties, type HTMLAttributes } from 'react';
import { type BrailleSpinnerName } from 'unicode-animations';
/**
 * Braille unicode spinner. Renders the active frame of a `unicode-animations`
 * sequence inside an inline `<span>`, advancing on the spinner's own interval.
 *
 * Inherits font color and font size from its parent — apply Tailwind utilities
 * (e.g. `text-warning`, `text-base`) via `className` to style.
 *
 * Decorative by default. Pass `aria-label` (and optionally `role="status"`) when
 * the spinner has no surrounding loading text and screen readers need to know
 * something is loading.
 */
export declare function Spinner({ className, name, style, ...props }: SpinnerProps): import("react/jsx-runtime").JSX.Element;
interface SpinnerProps extends HTMLAttributes<HTMLSpanElement> {
    className?: string;
    name?: BrailleSpinnerName;
    style?: CSSProperties;
}
export {};
//# sourceMappingURL=spinner.d.ts.map