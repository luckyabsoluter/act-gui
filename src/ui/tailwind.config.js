/** @type {import('tailwindcss').Config} */
module.exports = {
  prefix: 'tw-',
  content: [
    "./index.html",
    "./src/**/*.{vue,js,ts,jsx,tsx}"
  ],
  theme: {
    extend: {
      colors: {
        green: 'var(--color-green)',
        yellow: 'var(--color-yellow)',
        red: 'var(--color-red)',
        teal: 'var(--color-teal)',
        purple: 'var(--color-purple)',
        secondary: 'var(--color-secondary)',
        text: 'var(--color-text)',
        'text-light': 'var(--color-text-light)',
        'text-light-2': 'var(--color-text-light-2)',
        'grey-light': 'var(--color-grey-light)',
      },
      fontSize: {
        12: '12px',
      },
    },
  },
  plugins: [],
}
