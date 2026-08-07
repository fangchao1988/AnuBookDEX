import type { Config } from 'tailwindcss'

// 色板 token 对齐 static/prototype_aleo.html 的 :root CSS 变量（见 docs/前端实现方案.md §3）
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        bg: {
          primary: '#0b0e11',
          secondary: '#1e2329',
          tertiary: '#2a2f36',
          hover: '#252a30',
        },
        line: { DEFAULT: '#2b3139', light: '#373d45' },
        text: {
          primary: '#eaecef',
          secondary: '#848e9c',
          muted: '#5e6673',
        },
        up: '#0ecb81',
        down: '#f6465d',
        blue: '#1e80ff',
        purple: '#a371f7',
        orange: '#f0b90b',
        cyan: '#39d2b5',
        'up-bg': 'rgba(14,203,129,0.1)',
        'down-bg': 'rgba(246,70,93,0.1)',
        'ai-glow': 'rgba(163,113,247,0.15)',
        'blue-bg': 'rgba(30,128,255,0.1)',
        'orange-bg': 'rgba(240,185,11,0.12)',
      },
      fontFamily: {
        sans: [
          '-apple-system',
          'BlinkMacSystemFont',
          '"Segoe UI"',
          'Roboto',
          '"Helvetica Neue"',
          'Arial',
          'sans-serif',
        ],
        mono: ['"SF Mono"', '"Fira Code"', 'Menlo', 'Consolas', 'monospace'],
      },
      boxShadow: {
        dropdown: '0 8px 24px rgba(0,0,0,0.4)',
      },
    },
  },
  plugins: [],
} satisfies Config
