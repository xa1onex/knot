import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

export type Lang = 'en' | 'ru'
export type Theme = 'light' | 'dark'

type Prefs = {
  lang: Lang
  theme: Theme
  setLang: (lang: Lang) => void
  setTheme: (theme: Theme) => void
  t: (key: string, vars?: Record<string, string | number>) => string
}

const KEY_LANG = 'knot_lang'
const KEY_THEME = 'knot_theme'

const dict: Record<Lang, Record<string, string>> = {
  en: {
    brand: 'Node',
    nav_overview: 'Overview',
    nav_files: 'Files',
    nav_computers: 'Computers',
    nav_updates: 'Updates',
    nav_sync: 'Folder sync',
    nav_sites: 'Websites',
    nav_hardware: 'Hardware',
    nav_keys: 'API keys',
    nav_history: 'History',
    logout: 'Log out',
    theme_light: 'Light',
    theme_dark: 'Dark',
    live: 'Live',
    waiting: 'Waiting',
    overview_title: 'Your network',
    overview_lead: 'Who is connected, what moved, and whether Node needs an update.',
    open_files: 'Open files',
    computers: 'Computers',
    connected_now: 'Connected now',
    transfers: 'Transfers',
    updates: 'Updates',
    online: 'online',
    away: 'away',
    all_here: 'all here',
    in_progress: 'in progress',
    idle: 'idle',
    ready_to_install: 'ready to install',
    up_to_date: 'up to date',
    none: 'None',
    activity: 'Activity',
    activity_chart: 'Actions on this panel, last 13 hours',
    transfers_chart: 'File sends started in the last 13 hours',
    recent_activity: 'Recent activity',
    next: 'What to do next',
    add_computer: 'Add a computer',
    check_updates: 'Check updates',
    files_hint: 'Drag a file from one computer onto the other to copy it there.',
    this_folder: 'This folder',
    upload: 'Upload',
    new_folder: 'New folder',
    empty_folder: 'This folder is empty',
    drop_here: 'Drop to copy here',
    pick_computer: 'Pick a computer',
    offline: 'Offline — wake it to see files',
    no_computers: 'No computers yet',
    no_computers_lead: 'Add a Mac or PC, then you can drag files between them.',
    folder_name: 'New folder name',
    sent: 'Sent {name} → {dest}',
    uploaded: 'Uploaded {name}',
    transfer_done: 'Copied',
    transfers_dock: 'Transfers',
    transfers_empty: 'Drag a file from the left PC onto the right PC — or the other way.',
    login_title: 'Sign in',
    login_lead: 'Opens the panel for your computers and files.',
    email: 'Email',
    password: 'Password',
    open_node: 'Open Node',
    login_error: 'Could not sign in. Use the email and password from install.',
    computers_title: 'Computers',
    computers_lead: 'Each machine keeps its own files. Add another one, then copy by dragging in Files.',
    join_title: 'Join a Mac or PC',
    join_step_1: 'Optional nickname, then create a join code.',
    join_step_2: 'On that computer run the installer and choose Device Node.',
    join_step_3: 'Paste this panel URL and the code. It appears here when connected.',
    create_code: 'Create join code',
    nickname: 'Nickname, e.g. Home Mac',
    connected: 'Connected now',
    not_connected: 'Not connected',
    refresh: 'Refresh',
    name: 'Name',
    size: 'Size',
    modified: 'Modified',
    up: 'Up',
  },
  ru: {
    brand: 'Node',
    nav_overview: 'Обзор',
    nav_files: 'Файлы',
    nav_computers: 'Компьютеры',
    nav_updates: 'Обновления',
    nav_sync: 'Синхронизация',
    nav_sites: 'Сайты',
    nav_hardware: 'Железо',
    nav_keys: 'Ключи API',
    nav_history: 'История',
    logout: 'Выйти',
    theme_light: 'Светлая',
    theme_dark: 'Тёмная',
    live: 'В сети',
    waiting: 'Ожидание',
    overview_title: 'Сеть целиком',
    overview_lead: 'Кто подключён, что недавно передавалось, и нужно ли обновить Node.',
    open_files: 'Открыть файлы',
    computers: 'Компьютеры',
    connected_now: 'Сейчас в сети',
    transfers: 'Передачи',
    updates: 'Обновления',
    online: 'в сети',
    away: 'не в сети',
    all_here: 'все на месте',
    in_progress: 'идёт сейчас',
    idle: 'тихо',
    ready_to_install: 'можно поставить',
    up_to_date: 'актуально',
    none: 'Нет',
    activity: 'Активность',
    activity_chart: 'Действия в панели за 13 часов',
    transfers_chart: 'Отправки файлов за 13 часов',
    recent_activity: 'Недавние действия',
    next: 'Что сделать',
    add_computer: 'Добавить компьютер',
    check_updates: 'Проверить обновления',
    files_hint: 'Перетащи файл с одного компьютера на другой — он скопируется в открытую папку.',
    this_folder: 'Эта папка',
    upload: 'Загрузить',
    new_folder: 'Новая папка',
    empty_folder: 'Папка пустая',
    drop_here: 'Отпусти, чтобы скопировать сюда',
    pick_computer: 'Выбери компьютер',
    offline: 'Не в сети — разбуди, чтобы увидеть файлы',
    no_computers: 'Пока нет компьютеров',
    no_computers_lead: 'Добавь Mac или ПК — потом файлы можно таскать между ними.',
    folder_name: 'Имя новой папки',
    sent: 'Отправлено {name} → {dest}',
    uploaded: 'Загружено {name}',
    transfer_done: 'Скопировано',
    transfers_dock: 'Передачи',
    transfers_empty: 'Перетащи файл с левого ПК на правый — или наоборот.',
    login_title: 'Вход',
    login_lead: 'Открывает панель с твоими компьютерами и файлами.',
    email: 'Почта',
    password: 'Пароль',
    open_node: 'Открыть Node',
    login_error: 'Не вошли. Почта и пароль — те, что задал при установке.',
    computers_title: 'Компьютеры',
    computers_lead: 'У каждой машины свои файлы. Добавь вторую — и в «Файлах» можно таскать между ними.',
    join_title: 'Подключить Mac или ПК',
    join_step_1: 'По желанию имя, затем создай код.',
    join_step_2: 'На том компьютере запусти установщик и выбери Device Node.',
    join_step_3: 'Вставь URL панели и код. Когда подключится — появится здесь.',
    create_code: 'Создать код',
    nickname: 'Имя, например Домашний Mac',
    connected: 'Сейчас в сети',
    not_connected: 'Не подключён',
    refresh: 'Обновить',
    name: 'Имя',
    size: 'Размер',
    modified: 'Изменён',
    up: 'Наверх',
  },
}

const PrefsContext = createContext<Prefs | null>(null)

function applyTheme(theme: Theme) {
  document.documentElement.classList.toggle('dark', theme === 'dark')
}

export function PrefsProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(() => {
    const v = localStorage.getItem(KEY_LANG)
    return v === 'ru' || v === 'en' ? v : navigator.language.startsWith('ru') ? 'ru' : 'en'
  })
  const [theme, setThemeState] = useState<Theme>(() => {
    const v = localStorage.getItem(KEY_THEME)
    if (v === 'light' || v === 'dark') return v
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  })

  useEffect(() => {
    applyTheme(theme)
  }, [theme])

  const value = useMemo<Prefs>(() => {
    function t(key: string, vars?: Record<string, string | number>) {
      let out = dict[lang][key] || dict.en[key] || key
      if (vars) {
        for (const [k, v] of Object.entries(vars)) out = out.replaceAll(`{${k}}`, String(v))
      }
      return out
    }
    return {
      lang,
      theme,
      setLang: (next) => {
        setLangState(next)
        localStorage.setItem(KEY_LANG, next)
      },
      setTheme: (next) => {
        setThemeState(next)
        localStorage.setItem(KEY_THEME, next)
        applyTheme(next)
      },
      t,
    }
  }, [lang, theme])

  return <PrefsContext.Provider value={value}>{children}</PrefsContext.Provider>
}

export function usePrefs() {
  const ctx = useContext(PrefsContext)
  if (!ctx) throw new Error('usePrefs')
  return ctx
}
