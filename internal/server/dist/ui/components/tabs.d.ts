import { type ButtonHTMLAttributes, type HTMLAttributes, type ReactNode } from 'react';
export declare function Tabs({ children, className, defaultValue }: TabsProps): import("react/jsx-runtime").JSX.Element;
export declare function TabsList({ className, ...props }: HTMLAttributes<HTMLDivElement>): import("react/jsx-runtime").JSX.Element;
export declare function TabsTrigger({ active, className, value: _value, ...props }: TabsTriggerProps): import("react/jsx-runtime").JSX.Element;
interface TabsProps {
    children: (active: string, setActive: (value: string) => void) => ReactNode;
    className?: string;
    defaultValue: string;
}
interface TabsTriggerProps extends ButtonHTMLAttributes<HTMLButtonElement> {
    active: boolean;
    value: string;
}
export {};
//# sourceMappingURL=tabs.d.ts.map