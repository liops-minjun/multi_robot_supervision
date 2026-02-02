/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        // Theme-aware colors using CSS variables
        theme: {
          // Backgrounds
          'bg-base': 'var(--color-bg-base)',
          'bg-surface': 'var(--color-bg-surface)',
          'bg-elevated': 'var(--color-bg-elevated)',
          'bg-sunken': 'var(--color-bg-sunken)',
          // Borders
          'border': 'var(--color-border-primary)',
          'border-secondary': 'var(--color-border-secondary)',
          // Text
          'text': 'var(--color-text-primary)',
          'text-secondary': 'var(--color-text-secondary)',
          'text-muted': 'var(--color-text-muted)',
          // Accents
          'accent-blue': 'var(--color-accent-blue)',
          'accent-green': 'var(--color-accent-green)',
          'accent-red': 'var(--color-accent-red)',
          'accent-yellow': 'var(--color-accent-yellow)',
          'accent-purple': 'var(--color-accent-purple)',
          'accent-cyan': 'var(--color-accent-cyan)',
          'accent-orange': 'var(--color-accent-orange)',
        },
      },
      backgroundColor: {
        base: 'var(--color-bg-base)',
        surface: 'var(--color-bg-surface)',
        elevated: 'var(--color-bg-elevated)',
        sunken: 'var(--color-bg-sunken)',
      },
      borderColor: {
        primary: 'var(--color-border-primary)',
        secondary: 'var(--color-border-secondary)',
      },
      textColor: {
        primary: 'var(--color-text-primary)',
        secondary: 'var(--color-text-secondary)',
        muted: 'var(--color-text-muted)',
      },
    },
  },
  plugins: [],
}
