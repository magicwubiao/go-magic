import type { AutoPlayPattern } from './image-distortion';
/**
 * Social-ready glitchy card built around the haptic-distortion image
 * component. The poster runs the sword-guy distortion on an auto-animated
 * slash pattern so it can be screen-recorded as a GIF without a human
 * moving a cursor.
 *
 * Two variants, matching actual use cases:
 * - `'vibe'` (default): full-bleed distorted image with just registration
 *   marks and a tiny "Hermes Agent" mark in the corner — mirrors the
 *   overlay on the Hermes agent website.
 * - `'dispatch'`: broadcast-card layout with sidebar copy, numbered tags,
 *   and chrome — for when the poster needs to carry information.
 */
export declare function Poster({ aspect, autoPlay, body, border, channel, children, className, cornerMarks, eyebrow, headline, layout, scale, seal, signature, src, tags, tint, tintStrength, variant, ...rest }: PosterProps): import("react/jsx-runtime").JSX.Element;
export type PosterAspect = 'landscape' | 'portrait' | 'square' | 'story' | 'wide';
export type PosterVariant = 'dispatch' | 'vibe';
export interface PosterProps {
    /** Output aspect ratio. Picks sensible defaults for common social formats. */
    aspect?: PosterAspect;
    /** Distortion choreography pattern. Default: `'slash'`. */
    autoPlay?: AutoPlayPattern;
    /** (`dispatch` only) Descriptive copy under the headline. */
    body?: React.ReactNode;
    /** Show the thin outer frame around the poster. Default `true`. */
    border?: boolean;
    /** Tiny broadcast-station label. Optional in `vibe`; shown in header in `dispatch`. */
    channel?: React.ReactNode;
    /** (`dispatch` only) Override the sidebar content (takes precedence over headline/body). */
    children?: React.ReactNode;
    className?: string;
    /** Show the small `+` die-line registration marks in the image corners. Default `true`. */
    cornerMarks?: boolean;
    /** (`dispatch` only) Small tagline above the headline. */
    eyebrow?: React.ReactNode;
    /** (`dispatch` only) Big expanded-typography headline. Pass an array of strings to stack lines. */
    headline?: string[] | string;
    /** (`dispatch` only) Force stacked vs split layout. Default inferred from `aspect`. */
    layout?: 'split' | 'stacked';
    /** Render scale. 1 = full canvas (1080px+ base width). */
    scale?: number;
    /** (`dispatch` only) Small legal / signature line at the bottom-right. */
    seal?: React.ReactNode;
    /**
     * Signature mark. In `vibe` this is the small "Hermes Agent" overlay in the
     * bottom-right. In `dispatch` this is the URL / CTA in the footer.
     */
    signature?: React.ReactNode;
    /** Override the poster image. Defaults to the Hermes "filler-bg0" asset. */
    src?: string;
    /** (`dispatch` only) Ranked list of features / pricing tiers rendered as a numbered sidebar list. */
    tags?: string[];
    /** Shader tint overlay. Great for tier-colored variants. */
    tint?: string;
    /** Active / inactive tint strength — defaults match `ImageDistortion`. */
    tintStrength?: {
        active: number;
        inactive: number;
    };
    /** Layout variant. `'vibe'` (default) is full-bleed image; `'dispatch'` is the broadcast-card with sidebar copy. */
    variant?: PosterVariant;
}
//# sourceMappingURL=poster.d.ts.map