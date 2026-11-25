const c = (n) => `hsl(var(--${n}) / <alpha-value>)`;
/** @type {import('tailwindcss').Config} */
export default {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        background: c('background'), foreground: c('foreground'),
        card: c('card'), muted: c('muted'), 'muted-foreground': c('muted-foreground'),
        border: c('border'), primary: c('primary'), 'primary-foreground': c('primary-foreground'),
        success: c('success'), warning: c('warning'), danger: c('danger'), ring: c('ring'),
      },
      fontFamily: {
        sans: ['system-ui', '-apple-system', 'Segoe UI', 'Roboto', 'Helvetica Neue', 'Arial', 'sans-serif'],
      },
    },
  },
  plugins: [],
};
