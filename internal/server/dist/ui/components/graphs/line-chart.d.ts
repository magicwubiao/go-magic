import { type ChartProps, type DataPoint } from './utils';
export declare const LineChart: {
    (props: Omit<LineChartProps<DataPoint> & {
        backgroundColor?: string;
        color?: string;
    }, keyof import("../blend-mode").BlendColors>): import("react/jsx-runtime").JSX.Element;
    displayName: string;
};
interface LineChartProps<T extends DataPoint> extends ChartProps<T> {
    curve?: 'basis' | 'catmull-rom' | 'linear' | 'natural' | 'step';
    series?: keyof T;
    showArea?: boolean;
}
export {};
//# sourceMappingURL=line-chart.d.ts.map