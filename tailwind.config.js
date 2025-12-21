/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    "./internal/analytics/templates/**/*.html",
  ],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        twitch: {
          DEFAULT: '#9146ff',
          dark: '#772ce8',
          bg: '#0e0e10',
          card: '#18181b',
          border: '#303033',
          text: '#efeff1',
          muted: '#adadb8',
          live: '#eb0400',
          success: '#36b535',
        },
      },
      fontFamily: {
        sans: ['system-ui', '-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'Roboto', 'Oxygen', 'Ubuntu', 'Cantarell', 'sans-serif'],
      },
    },
  },
  plugins: [],
}
