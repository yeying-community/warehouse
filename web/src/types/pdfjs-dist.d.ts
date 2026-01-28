declare module 'pdfjs-dist/legacy/build/pdf.min.mjs' {
  export const GlobalWorkerOptions: { workerSrc: string }
  export const getDocument: (src: any) => { promise: Promise<any> }
}

declare module 'pdfjs-dist/legacy/build/pdf.worker.min.mjs?url' {
  const workerSrc: string
  export default workerSrc
}
