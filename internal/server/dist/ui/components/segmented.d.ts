import { type ReactNode } from 'react';
export declare function Segmented<T extends string>({ className, onChange, options, size, value }: SegmentedProps<T>): import("react/jsx-runtime").JSX.Element;
export declare function FilterGroup({ children, className, label }: FilterGroupProps): import("react/jsx-runtime").JSX.Element;
interface FilterGroupProps {
    children: ReactNode;
    className?: string;
    label: string;
}
interface SegmentedOption<T extends string> {
    label: string;
    value: T;
}
interface SegmentedProps<T extends string> {
    className?: string;
    onChange: (value: T) => void;
    options: SegmentedOption<T>[];
    size?: 'md' | 'sm';
    value: T;
}
export {};
//# sourceMappingURL=segmented.d.ts.map