// 首屏防闪烁：与 ThemeProvider 共用 localStorage 键与 .dark 类（跟随系统）
try {
  var s = localStorage.getItem('anipick-theme')
  var d =
    s === 'dark' ||
    ((s === null || s === undefined || s === 'system') &&
      window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', d)
  document.documentElement.style.colorScheme = d ? 'dark' : 'light'
} catch (e) {}
