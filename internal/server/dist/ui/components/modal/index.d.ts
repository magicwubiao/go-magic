export declare function Modal({ children, className, id, trigger, ...props }: ModalProps): import("react/jsx-runtime").JSX.Element;
interface ModalProps extends Omit<React.ComponentProps<'dialog'>, 'open'> {
    trigger: (controls: {
        close: () => void;
        open: () => void;
    }) => React.ReactNode;
}
export {};
//# sourceMappingURL=index.d.ts.map