import { type ChartProps, type DataPoint } from './utils';
export declare const BarChart: {
    (props: Omit<BarChartProps<DataPoint> & {
        backgroundColor?: string;
        color?: string;
    }, keyof import("../blend-mode").BlendColors>): import("react/jsx-runtime").JSX.Element;
    displayName: string;
};
interface BarChartProps<T extends DataPoint> extends ChartProps<T> {
    xDomain?: [number, number];
}
export {};
//# sourceMappingURL=bar-chart.d.ts.map