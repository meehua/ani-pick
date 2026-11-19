/// <reference types="vite/client" />

// 构建时由 vite.config.ts 注入的版本标识（version-commit），与后端 /api/meta 的
// build 字段同源比对（见 components/build-status.tsx）。
declare const __APP_BUILD__: string
