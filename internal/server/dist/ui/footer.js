'use client';
import { jsxs as _jsxs, jsx as _jsx } from "react/jsx-runtime";
import { useRef } from 'react';
import { useCssVarDims } from '../hooks/use-css-var-dims';
import { Cell, Grid } from './components/grid';
import { Small } from './components/typography/small';
const GROUPS = [
    { label: 'Product', links: ['Overview', 'Features', 'Pricing'] },
    { label: 'Resources', links: ['Docs', 'Blog', 'Support'] },
    { label: 'Company', links: ['About', 'Careers', 'Contact'] },
    { label: 'Legal', links: ['Privacy', 'Terms', 'License'] }
];
export function Footer({ LinkComponent = 'a' }) {
    const ref = useRef(null);
    useCssVarDims('footer', ref);
    return (_jsxs(Grid, { as: "footer", ref, children: [_jsx(Cell, { children: _jsxs(Small, { className: "opacity-50", children: ["\u00A9", new Date().getFullYear()] }) }), GROUPS.map(({ label, links }) => (_jsxs(Cell, { children: [_jsx(Small, { className: "opacity-50", children: label }), _jsx("nav", { className: "mt-3 flex flex-col gap-2", children: links.map(link => (_jsx(Small, { as: LinkComponent, className: "underline", href: `/${link.toLowerCase()}`, children: link }, link))) })] }, label)))] }));
}
//# sourceMappingURL=footer.js.map