import { type CSSProperties, type ReactNode } from 'react';
export declare function Select({ children, className, disabled, id, onValueChange, placeholder, style, value }: SelectProps): import("react/jsx-runtime").JSX.Element;
export declare function SelectOption(_props: SelectOptionProps): null;
interface SelectOptionProps {
    children: ReactNode;
    value: string;
}
interface SelectProps {
    children?: ReactNode;
    className?: string;
    disabled?: boolean;
    id?: string;
    onValueChange?: (value: string) => void;
    placeholder?: string;
    style?: CSSProperties;
    value?: string;
}
export {};
//# sourceMappingURL=select.d.ts.map