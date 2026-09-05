import type { Config } from "tailwindcss";

// Tailwind scans these folders for utility classes and generates only the CSS
// that the dashboard actually uses.
const config: Config = {
  content: ["./src/pages/**/*.{js,ts,jsx,tsx,mdx}", "./src/components/**/*.{js,ts,jsx,tsx,mdx}", "./src/app/**/*.{js,ts,jsx,tsx,mdx}"],
  theme: {
    extend: {
      colors: {
        ink: "#17202a",
        paper: "#f7f9fb",
        growth: "#0f766e"
      }
    }
  },
  plugins: []
};

export default config;
