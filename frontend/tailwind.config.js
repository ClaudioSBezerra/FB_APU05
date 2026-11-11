/** @type {import('tailwindcss').Config} */
export default {
  darkMode: ["class"],
  content: [
    "./index.html",
    "./src/**/*.{ts,tsx}",
  ],
  theme: {
    container: {
      center: true,
      padding: "2rem",
      screens: {
        "2xl": "1400px",
      },
    },
    extend: {
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
        // Tokens diretos do Catálogo 2.0 (sem modificador de opacidade).
        ws: "var(--ws-current)",
        brand: {
          red: "var(--brand-red)",
          "red-strong": "var(--brand-red-strong)",
          "red-tint": "var(--brand-red-tint)",
        },
        cta: {
          DEFAULT: "var(--green-150)",
          hover: "var(--green-50)",
          pressed: "var(--green-250)",
          soft: "var(--green-l850)",
          "soft-hover": "var(--green-l700)",
          outline: "var(--green-l550)",
        },
        info: { bg: "var(--info-bg)", border: "var(--info-br)", text: "var(--info-tx)" },
        success: { bg: "var(--suc-bg)", border: "var(--suc-br)", text: "var(--suc-tx)" },
        warning: { bg: "var(--warn-bg)", border: "var(--warn-br)", text: "var(--warn-tx)" },
        danger: { bg: "var(--err-bg)", border: "var(--err-br)", text: "var(--err-tx)" },
        fg: {
          secondary: "var(--fg-secondary)",
          helper: "var(--fg-helper)",
          table: "var(--fg-table)",
          muted: "var(--fg-muted)",
        },
        canvas: "var(--bg-canvas)",
        link: "var(--blue-d150)",
      },
      // Catálogo 2.0: 8px em cards/modais (lg), 4px em tudo que encosta em
      // texto — botão, input, badge, alerta (md/sm). Ver --r-* em catalogo-tokens.css.
      borderRadius: {
        lg: "var(--r-card)",
        md: "var(--r-button)",
        sm: "var(--r-button)",
        pill: "var(--r-pill)",
        avatar: "var(--r-avatar)",
      },
      fontFamily: {
        sans: ["var(--font-family)"],
        mono: ["var(--font-mono)"],
      },
      boxShadow: {
        card: "var(--shadow-card)",
        modal: "var(--shadow-modal)",
        popover: "var(--shadow-popover)",
        tooltip: "var(--shadow-tooltip)",
        focus: "var(--focus-ring)",
      },
      keyframes: {
        "accordion-down": {
          from: { height: "0" },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: "0" },
        },
      },
      animation: {
        "accordion-down": "accordion-down 0.2s ease-out",
        "accordion-up": "accordion-up 0.2s ease-out",
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
}
