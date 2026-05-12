export declare function Watchlist({ className, counter, items, scramble, ...props }: WatchlistProps): import("react/jsx-runtime").JSX.Element;
interface WatchlistProps extends React.ComponentProps<'div'> {
    counter?: boolean;
    items: {
        label?: React.ReactNode;
        right?: React.ReactNode;
        url?: string;
    }[];
    scramble?: boolean;
}
export {};
//# sourceMappingURL=watchlist.d.ts.map