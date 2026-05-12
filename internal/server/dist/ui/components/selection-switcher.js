'use client';
import { useEffect } from 'react';
const colors = [
    'oklch(85% 0.12 330)',
    'oklch(85% 0.12 300)',
    'oklch(85% 0.12 270)',
    'oklch(85% 0.12 230)',
    'oklch(85% 0.12 180)',
    'oklch(85% 0.12 150)',
    'oklch(85% 0.12 120)',
    'oklch(85% 0.12 90)',
    'oklch(85% 0.12 60)',
    'oklch(85% 0.12 30)',
    'oklch(88% 0.10 80)'
];
export function SelectionSwitcher() {
    useEffect(() => {
        const ac = new AbortController();
        const { signal } = ac;
        let idx = 0;
        const cycle = () => requestAnimationFrame(() => document.documentElement.style.setProperty('--selection-bg', colors[(idx = (idx + 1) % colors.length)]));
        const onKey = (e) => e.key.toLowerCase() === 'a' && (e.metaKey || e.ctrlKey) && cycle();
        document.addEventListener('selectstart', cycle, { signal });
        document.addEventListener('keydown', onKey, { signal });
        return () => ac.abort();
    }, []);
    return null;
}
//# sourceMappingURL=selection-switcher.js.map