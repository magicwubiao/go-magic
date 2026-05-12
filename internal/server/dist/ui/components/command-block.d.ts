/**
 * A "copy to clipboard" button that briefly shows a "Copied!" confirmation.
 * Designed to sit alongside a short command string, not as a general button.
 */
export declare function CopyButton({ children, className, copiedLabel, label, resetDelayMs, text }: CopyButtonProps): import("react/jsx-runtime").JSX.Element;
/**
 * A labeled, copy-able command (or code) display. Pairs `<CopyButton>` with
 * a monospace code block. Used for install/setup instructions.
 */
export declare function CommandBlock({ className, code, label }: CommandBlockProps): import("react/jsx-runtime").JSX.Element;
interface CommandBlockProps {
    className?: string;
    code: string;
    label: string;
}
interface CopyButtonProps {
    children?: React.ReactNode;
    className?: string;
    copiedLabel?: string;
    label?: string;
    resetDelayMs?: number;
    text: string;
}
export {};
//# sourceMappingURL=command-block.d.ts.map