/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

interface NodeDesktopBridge {
  platform: string
  isDesktop: true
  getApiUrl: () => Promise<string>
  setApiUrl: (url: string) => Promise<string>
  pickUploadFiles: () => Promise<Array<{ name: string; size: number; path: string; dataBase64: string }>>
  showAbout: () => Promise<void>
  onMenuUpload: (cb: () => void) => () => void
}

interface Window {
  nodeDesktop?: NodeDesktopBridge
}
