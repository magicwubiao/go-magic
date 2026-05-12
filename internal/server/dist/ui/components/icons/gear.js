import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { isValidElement } from 'react';
const VIEWBOX = '0 0 34 38';
const GEAR_PATH = 'M10.1249 3.37446h5.0624v3.37446h-5.0624zM23.6262 3.37446h-5.0625v3.37446h5.0625zM18.5637 0v3.37446h-3.3764L15.1877 0zM3.3762 6.74879l6.7487.00013.0003 3.37588h-6.749zM30.3748 6.74879l-6.7486.00013-.0003 3.37588h6.7489zM0 26.9988v-16.874h3.3762l-.00019 16.874zM33.7505 26.9988v-16.874h-3.3757l-.0003 16.874zM10.1248 30.3751H3.37586l.00015-3.3763 6.74879.0003zM23.6262 30.3751h6.749l-.0007-3.3763-6.7483.0003zM15.1873 33.7495h-5.0624l-.0001-3.3744 5.0625-.0001zM18.5637 33.7495h5.0625v-3.3744l-5.0625-.0001zM15.1874 37.1245l-.0001-3.375h3.3764l-.0003 3.375z';
export function GearIcon({ children, innerScale = 0.55, ...props }) {
    const isSvg = isValidElement(children) && children.type === 'svg';
    const viewBox = isSvg
        ? (children.props.viewBox ?? VIEWBOX)
        : VIEWBOX;
    const inner = isSvg
        ? children.props.children
        : children;
    return (_jsxs("svg", { fill: "none", viewBox: VIEWBOX, ...props, children: [_jsx("g", { clipRule: "evenodd", fill: "currentColor", fillRule: "evenodd", children: _jsx("path", { d: GEAR_PATH }) }), children && (_jsx("svg", { height: 26 * innerScale, width: 26 * innerScale, x: 17 - (26 * innerScale) / 2, y: 19 - (26 * innerScale) / 2, viewBox, children: inner }))] }));
}
//# sourceMappingURL=gear.js.map