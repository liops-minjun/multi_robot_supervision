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
        // Theme-aware button backgrounds
        'btn-blue': 'var(--color-btn-blue-bg)',
        'btn-green': 'var(--color-btn-green-bg)',
        'btn-red': 'var(--color-btn-red-bg)',
        'btn-yellow': 'var(--color-btn-yellow-bg)',
        'btn-purple': 'var(--color-btn-purple-bg)',
        'btn-cyan': 'var(--color-btn-cyan-bg)',
      },
      borderColor: {
        primary: 'var(--color-border-primary)',
        secondary: 'var(--color-border-secondary)',
        // Theme-aware button borders
        'btn-blue': 'var(--color-btn-blue-border)',
        'btn-green': 'var(--color-btn-green-border)',
        'btn-red': 'var(--color-btn-red-border)',
        'btn-yellow': 'var(--color-btn-yellow-border)',
        'btn-purple': 'var(--color-btn-purple-border)',
        'btn-cyan': 'var(--color-btn-cyan-border)',
      },
      textColor: {
        primary: 'var(--color-text-primary)',
        secondary: 'var(--color-text-secondary)',
        muted: 'var(--color-text-muted)',
        // Theme-aware button text
        'btn-blue': 'var(--color-btn-blue-text)',
        'btn-green': 'var(--color-btn-green-text)',
        'btn-red': 'var(--color-btn-red-text)',
        'btn-yellow': 'var(--color-btn-yellow-text)',
        'btn-purple': 'var(--color-btn-purple-text)',
        'btn-cyan': 'var(--color-btn-cyan-text)',
      },
    },
  },
  plugins: [],
}
