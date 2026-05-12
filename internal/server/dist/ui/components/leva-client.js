'use client';
import { jsx as _jsx } from "react/jsx-runtime";
import { Leva } from 'leva';
import { useEffect, useState } from 'react';
export function LevaClient() {
    const [hidden, setHidden] = useState(true);
    useEffect(() => {
        setHidden(!new URLSearchParams(window.location.search).has('dev'));
    }, []);
    return _jsx(Leva, { hidden });
}
//# sourceMappingURL=leva-client.js.map