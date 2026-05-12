type Direction = 'down' | 'up' | 'left' | 'right';
export declare function DropdownMenu<T extends string>({ className, direction, onChange, options, value }: {
    className?: string;
    direction?: Direction;
    onChange: (value: T) => void;
    options: {
        label: string;
        value: T;
    }[];
    value: T;
}): import("react/jsx-runtime").JSX.Element;
export {};
//# sourceMappingURL=dropdown-menu.d.ts.map