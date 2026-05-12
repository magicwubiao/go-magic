import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { Cell, Grid } from './components/grid';
import { Progress } from './components/progress';
import { H1 } from './components/typography/h1';
import { Small } from './components/typography/small';
export function BasicPage({ children, subtitle, title }) {
    return (_jsxs(_Fragment, { children: [_jsx(Grid, { children: _jsx(Cell, { children: _jsx(Progress, { value: 0 }) }) }), _jsxs(Grid, { className: "lg:grid-cols-[max-content_1fr]", children: [_jsx(Cell, { className: "-order-1", children: _jsxs("div", { className: "sticky top-4 flex flex-col gap-4", children: [title ? _jsx(H1, { className: "-mb-2 pr-10 opacity-90", children: title }) : null, subtitle ? _jsx(Small, { className: "opacity-60", children: subtitle }) : null] }) }), _jsx(Cell, { className: "post bg-current/3", children: children })] })] }));
}
//# sourceMappingURL=basic-page.js.map