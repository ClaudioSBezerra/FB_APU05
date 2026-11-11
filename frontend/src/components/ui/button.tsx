import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium leading-5 transition-colors duration-150 ease-in-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-0 disabled:cursor-not-allowed disabled:opacity-40 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        // Sólido verde (--green-150 → hover --green-50 → pressed --green-250)
        default: "bg-cta text-white enabled:hover:bg-cta-hover enabled:active:bg-cta-pressed",
        destructive: "bg-brand-red text-white enabled:hover:bg-brand-red-strong",
        // Contornado verde (borda --green-l550, texto --green-250, wash no hover, tint no press)
        outline: "border border-cta-outline bg-card text-cta-pressed enabled:hover:bg-[rgba(195,227,170,.15)] enabled:active:bg-cta-soft",
        // Soft azul — família secundária
        secondary: "bg-secondary text-secondary-foreground enabled:hover:bg-[var(--blue-l750)]",
        // Plain verde (sem borda; hover --green-l850)
        ghost: "text-cta-pressed enabled:hover:bg-cta-soft",
        link: "text-link underline-offset-4 hover:underline",
      },
      size: {
        default: "h-10 px-4 py-2",
        sm: "h-8 rounded-md px-3 text-xs leading-4",
        lg: "h-11 rounded-md px-8",
        icon: "h-10 w-10",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return <Comp className={cn(buttonVariants({ variant, size, className }))} ref={ref} {...props} />;
  },
);
Button.displayName = "Button";

export { Button, buttonVariants };
