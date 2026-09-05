// PostCSS runs Tailwind and Autoprefixer while Next.js builds the stylesheet.
// Tailwind expands utility classes; Autoprefixer adds browser compatibility.
const config = {
  plugins: {
    tailwindcss: {},
    autoprefixer: {}
  }
};

export default config;
