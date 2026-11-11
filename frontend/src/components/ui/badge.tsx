import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center rounded-sm border px-1.5 py-[3px] text-[10px] font-medium uppercase leading-[10px] tracking-[.04em] whitespace-nowrap focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-0",
  {
    variants: {
      variant: {
        // Nomes shadcn mantidos (usados nas páginas); cores = --badge-* do Catálogo 2.0
        default: "border-[var(--badge-green-br)] bg-[var(--badge-green-bg)] text-[var(--badge-green-tx)]",
        secondary: "border-[var(--badge-gray-br)] bg-[var(--badge-gray-bg)] text-[var(--badge-gray-tx)]",
        destructive: "border-[var(--badge-red-br)] bg-[var(--badge-red-bg)] text-[var(--badge-red-tx)]",
        outline: "border-[var(--badge-gray-br)] bg-card text-[var(--badge-gray-tx)]",
        info: "border-[var(--badge-blue-br)] bg-[var(--badge-blue-bg)] text-[var(--badge-blue-tx)]",
        warning: "border-[var(--badge-yellow-br)] bg-[var(--badge-yellow-bg)] text-[var(--badge-yellow-tx)]",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

export interface BadgeProps extends React.HTMLAttributes<HTMLDivElement>, VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return <div className={cn(badgeVariants({ variant }), className)} {...props} />;
}

export { Badge, badgeVariants };
