'use client';
import { jsx as _jsx, Fragment as _Fragment, jsxs as _jsxs } from "react/jsx-runtime";
import { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { cn } from '../../../utils';
export function Modal({ children, className, id, trigger, ...props }) {
    const ref = useRef(null);
    const [mounted, setMounted] = useState(false);
    useEffect(() => setMounted(true), []);
    const open = useCallback(() => ref.current?.showModal(), []);
    const close = useCallback(() => ref.current?.close(), []);
    return (_jsxs(_Fragment, { children: [trigger({ close, open }), mounted &&
                createPortal(_jsx("dialog", { className: cn('modal', className), onClick: e => e.target === e.currentTarget && close(), id, ref, ...props, children: _jsx("div", { className: "modal-body post", children: children }) }), document.body)] }));
}
//# sourceMappingURL=index.js.map