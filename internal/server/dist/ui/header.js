'use client';
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useRef } from 'react';
import { useCssVarDims } from '../hooks/use-css-var-dims';
import { Blink } from './components/blink';
import { Cell, Grid } from './components/grid';
import { HoverBg } from './components/hover-bg';
import { H2 } from './components/typography/h2';
import { Small } from './components/typography/small';
const NAV = [
    { href: '/projects', label: 'Projects' },
    { href: '/participants', label: 'Participants' },
    { href: '/provenance', label: 'Provenance' },
    { href: '/contribute', label: 'Contribute' }
];
export function Header({ LinkComponent = 'a' }) {
    const ref = useRef(null);
    useCssVarDims('header', ref);
    return (_jsxs(Grid, { as: "header", ref, children: [_jsx(Cell, { as: LinkComponent, href: "/", children: _jsxs("hgroup", { className: "flex flex-col gap-2", children: [_jsx(Small, { children: "Nous" }), _jsx(H2, { children: "Psyche" })] }) }), NAV.map(({ href, label }) => (_jsxs(Cell, { as: LinkComponent, className: "group relative", href: '/runs', children: [_jsxs(Small, { children: [label, _jsx(Blink, {})] }), _jsx(HoverBg, {})] }, href)))] }));
}
//# sourceMappingURL=header.js.map