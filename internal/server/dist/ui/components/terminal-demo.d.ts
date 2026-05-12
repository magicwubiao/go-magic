export declare function TerminalDemo({ ariaLabel, className, height, label, loopDelayMs, outputLineDelayMs, sequence }: TerminalDemoProps): import("react/jsx-runtime").JSX.Element;
interface ClearStep {
    type: 'clear';
}
interface OutputStep {
    lines: string[];
    type: 'output';
}
interface PauseStep {
    ms: number;
    type: 'pause';
}
interface PromptStep {
    text: string;
    type: 'prompt';
}
interface TerminalDemoProps {
    ariaLabel?: string;
    className?: string;
    height?: number | string;
    label?: string;
    loopDelayMs?: number;
    outputLineDelayMs?: number;
    sequence: TerminalDemoStep[];
}
export type TerminalDemoStep = ClearStep | OutputStep | PauseStep | PromptStep | TypeStep;
interface TypeStep {
    delay?: number;
    text: string;
    type: 'type';
}
export {};
//# sourceMappingURL=terminal-demo.d.ts.map