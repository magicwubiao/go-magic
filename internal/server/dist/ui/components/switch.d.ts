import { type ButtonHTMLAttributes } from 'react';
export declare const Switch: import("react").ForwardRefExoticComponent<SwitchProps & import("react").RefAttributes<HTMLButtonElement>>;
interface SwitchProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'onChange'> {
    checked: boolean;
    onCheckedChange: (checked: boolean) => void;
}
export {};
//# sourceMappingURL=switch.d.ts.map