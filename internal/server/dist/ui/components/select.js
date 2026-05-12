'use client';
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { Children, isValidElement, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { cn } from '../../utils';
const TRIGGER_CN = 'flex h-9 w-full items-center justify-between gap-2 ' +
    'border border-midground/15 bg-background/40 px-3 py-1 ' +
    'font-courier text-sm text-left text-midground transition-colors ' +
    'hover:border-midground/25 ' +
    'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-midground/30 focus-visible:border-midground/30 ' +
    'disabled:cursor-not-allowed disabled:opacity-50 ' +
    'cursor-pointer';
const LISTBOX_CN = 'absolute z-50 mt-1 w-full max-h-60 overflow-auto ' +
    'border border-midground/15 bg-background-base text-midground shadow-lg';
export function Select({ children, className, disabled, id, onValueChange, placeholder, style, value }) {
    const [open, setOpen] = useState(false);
    const [highlightedIndex, setHighlightedIndex] = useState(-1);
    const containerRef = useRef(null);
    const listRef = useRef(null);
    const options = useMemo(() => collectOptions(children), [children]);
    const selected = options.find(o => o.value === value);
    const displayLabel = selected?.label ?? placeholder ?? value ?? '';
    const close = useCallback(() => {
        setOpen(false);
        setHighlightedIndex(-1);
    }, []);
    useEffect(() => {
        if (!open)
            return;
        const ac = new AbortController();
        document.addEventListener('mousedown', e => {
            if (!containerRef.current?.contains(e.target))
                close();
        }, { signal: ac.signal });
        return () => ac.abort();
    }, [open, close]);
    useEffect(() => {
        if (!open || highlightedIndex < 0)
            return;
        const el = listRef.current?.children[highlightedIndex];
        el?.scrollIntoView({ block: 'nearest' });
    }, [open, highlightedIndex]);
    const handleKeyDown = (e) => {
        if (disabled)
            return;
        switch (e.key) {
            case 'Enter':
            case ' ':
                e.preventDefault();
                if (!open) {
                    setOpen(true);
                    setHighlightedIndex(options.findIndex(o => o.value === value));
                }
                else if (highlightedIndex >= 0 && options[highlightedIndex]) {
                    onValueChange?.(options[highlightedIndex].value);
                    close();
                }
                break;
            case 'ArrowDown':
                e.preventDefault();
                if (!open) {
                    setOpen(true);
                    setHighlightedIndex(options.findIndex(o => o.value === value));
                }
                else {
                    setHighlightedIndex(i => Math.min(i + 1, options.length - 1));
                }
                break;
            case 'ArrowUp':
                e.preventDefault();
                if (open)
                    setHighlightedIndex(i => Math.max(i - 1, 0));
                break;
            case 'Home':
                if (open) {
                    e.preventDefault();
                    setHighlightedIndex(0);
                }
                break;
            case 'End':
                if (open) {
                    e.preventDefault();
                    setHighlightedIndex(options.length - 1);
                }
                break;
            case 'Escape':
                e.preventDefault();
                close();
                break;
        }
    };
    return (_jsxs("div", { className: cn('relative', className), id: id, ref: containerRef, style: style, children: [_jsxs("button", { "aria-expanded": open, "aria-haspopup": "listbox", className: TRIGGER_CN, disabled: disabled, onClick: () => !disabled && setOpen(o => !o), onKeyDown: handleKeyDown, role: "combobox", type: "button", children: [_jsx("span", { className: cn('truncate', !selected && 'text-midground/50'), children: displayLabel }), _jsx(ChevronDownGlyph, { className: cn('size-3 shrink-0 text-midground/60 transition-transform', open && 'rotate-180') })] }), open && (_jsx("div", { className: LISTBOX_CN, ref: listRef, role: "listbox", children: options.map((opt, i) => {
                    const isSelected = opt.value === value;
                    const isHighlighted = i === highlightedIndex;
                    return (_jsxs("div", { "aria-selected": isSelected, className: cn('flex cursor-pointer items-center gap-2 px-3 py-2', 'font-courier text-sm transition-colors', isHighlighted && 'bg-midground/10', isSelected ? 'text-midground' : 'text-midground/70'), onClick: () => {
                            onValueChange?.(opt.value);
                            close();
                        }, onMouseEnter: () => setHighlightedIndex(i), role: "option", children: [_jsx(CheckGlyph, { className: cn('size-3 shrink-0', isSelected ? 'opacity-100' : 'opacity-0') }), _jsx("span", { className: "truncate", children: opt.label })] }, opt.value));
                }) }))] }));
}
// Marker component — `Select` reads `value`/`children` from its tree.
// Renders nothing on its own.
export function SelectOption(_props) {
    return null;
}
const ChevronDownGlyph = ({ className }) => (_jsx("svg", { "aria-hidden": true, className: className, fill: "none", stroke: "currentColor", strokeLinecap: "square", strokeWidth: 1.5, viewBox: "0 0 12 12", children: _jsx("path", { d: "M2.5 4.5 6 8l3.5-3.5" }) }));
const CheckGlyph = ({ className }) => (_jsx("svg", { "aria-hidden": true, className: className, fill: "none", stroke: "currentColor", strokeLinecap: "square", strokeWidth: 1.5, viewBox: "0 0 12 12", children: _jsx("path", { d: "m2.5 6.5 2.5 2.5L9.5 3.5" }) }));
function collectOptions(children) {
    const out = [];
    Children.forEach(children, child => {
        if (!isValidElement(child))
            return;
        const el = child;
        if (el.props.value !== undefined) {
            out.push({
                label: typeof el.props.children === 'string'
                    ? el.props.children
                    : String(el.props.value),
                value: String(el.props.value)
            });
        }
        else if (el.props.children) {
            out.push(...collectOptions(el.props.children));
        }
    });
    return out;
}
//# sourceMappingURL=select.js.map