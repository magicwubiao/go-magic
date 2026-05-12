'use client';
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useCallback, useState } from 'react';
import { cn } from '../../utils';
import { Small } from './typography/small';
/**
 * A "copy to clipboard" button that briefly shows a "Copied!" confirmation.
 * Designed to sit alongside a short command string, not as a general button.
 */
export function CopyButton({ children, className, copiedLabel = 'Copied!', label = 'Copy', resetDelayMs = 2000, text }) {
    const [copied, setCopied] = useState(false);
    const handleCopy = useCallback(() => {
        void navigator.clipboard.writeText(text).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), resetDelayMs);
        });
    }, [resetDelayMs, text]);
    return (_jsx("button", { className: cn('font-courier cursor-pointer border-none bg-transparent text-[0.6875rem]', 'tracking-widest uppercase', 'hover:text-midground tap-highlight-transparent transition-colors', 'flex items-center justify-center', copied ? 'text-midground' : 'text-current opacity-60', className), onClick: handleCopy, type: "button", children: children ?? (copied ? copiedLabel : label) }));
}
/**
 * A labeled, copy-able command (or code) display. Pairs `<CopyButton>` with
 * a monospace code block. Used for install/setup instructions.
 */
export function CommandBlock({ className, code, label }) {
    return (_jsxs("div", { className: cn('flex flex-col gap-1', className), children: [_jsxs("div", { className: "flex items-center justify-between", children: [_jsx(Small, { className: "opacity-50", children: label }), _jsx(CopyButton, { text: code })] }), _jsx("div", { className: cn('bg-background/40 font-courier border border-current/20', 'px-3 py-2 text-[0.6875rem] leading-relaxed lowercase'), children: _jsx("code", { className: "break-all", children: code }) })] }));
}
//# sourceMappingURL=command-block.js.map