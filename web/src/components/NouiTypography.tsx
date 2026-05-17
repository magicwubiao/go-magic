import { type HTMLAttributes, type ReactNode } from "react";
import { cn } from "@/lib/utils";

type TypographyProps = HTMLAttributes<HTMLElement> & {
  as?: "span" | "div" | "p" | "h1" | "h2" | "h3" | "h4" | "h5" | "h6" | "article" | "section";
  children?: ReactNode;
  compressed?: boolean;
  courier?: boolean;
  expanded?: boolean;
  mondwest?: boolean;
  mono?: boolean;
  sans?: boolean;
  variant?: "sm" | "md" | "lg" | "xl";
};

const variantClasses: Record<NonNullable<TypographyProps["variant"]>, string> = {
  sm: "leading-[1.4] text-[.9375rem] tracking-[0.1875rem]",
  md: "text-[2.625rem] leading-[1] tracking-[0.0525rem]",
  lg: "text-[2.625rem] leading-[1] tracking-[0.0525rem]",
  xl: "text-[4.5rem] leading-[1] tracking-[0.135rem]",
};

export function Typography({
  as: Tag = "span",
  className,
  compressed,
  courier,
  expanded,
  mondwest,
  mono,
  sans,
  variant,
  ...props
}: TypographyProps) {
  const hasFontVariant = compressed || courier || expanded || mondwest || mono || sans;

  return (
    <Tag
      className={cn(
        compressed && "font-compressed",
        courier && "font-courier",
        expanded && "font-expanded",
        mondwest && "font-mondwest tracking-[0.1875rem]",
        mono && "font-mono",
        (!hasFontVariant || sans) && "font-sans",
        variant && variantClasses[variant],
        className,
      )}
      {...props}
    />
  );
}

export function H2({ className, variant = "lg", ...props }: Omit<TypographyProps, "as">) {
  return <Typography as="h2" className={cn("font-bold", className)} variant={variant} {...props} />;
}
