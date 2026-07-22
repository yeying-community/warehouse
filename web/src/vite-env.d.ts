/// <reference types="vite/client" />
declare module '*.vue'
declare module '*.md?raw' {
  const content: string
  export default content
}
