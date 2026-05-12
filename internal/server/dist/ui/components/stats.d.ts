import React, { ReactNode } from 'react';
export declare function Stats({ className, items, flip, ...props }: StatsProps): import("react/jsx-runtime").JSX.Element;
interface StatsProps extends React.ComponentProps<'div'> {
    items: {
        label: string | {
            key: string;
            node: ReactNode;
        };
        value: string | {
            key: string;
            node: ReactNode;
        };
    }[];
    flip?: boolean;
}
export {};
//# sourceMappingURL=stats.d.ts.map