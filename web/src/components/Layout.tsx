// The app shell: sidebar, top bar, search palette, shortcuts — the Pocket Calendar layout.
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Dashboard, Page, SavedFilter, Scene } from '../lib/types'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { Avatar, type Anchor, type MenuItem, Menu, MenuButton, modalOpen, rectOf, useIsMobile, useMediaQuery } from './ui'
import { AskButton } from './AskButton'
import { LockScreen, useLock } from './Lock'

const SIDEBAR_KEY = 'psp-sidebar'

/** Something was added or changed from the shell (or a page): the sidebar counts and the pages reload. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('psp:changed'))
export function useOnChange(cb: () => void) {
  const ref = useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('psp:changed', h)
    return () => window.removeEventListener('psp:changed', h)
  }, [])
}

/** The page's own name for the top bar breadcrumb (a scene's name). */
const CrumbCtx = createContext<(t: string) => void>(() => {})
export function useCrumb(title: string | undefined) {
  const set = useContext(CrumbCtx)
  useEffect(() => { set(title || ''); return () => set('') }, [title, set])
}

/** Counts and saved views, for every page that needs them (the sidebar, the list). */
const ShellCtx = createContext<{ dash: Dashboard | null; filters: SavedFilter[]; reload: () => void }>({ dash: null, filters: [], reload: () => {} })
export const useShell = () => useContext(ShellCtx)

/** The add menu: the sidebar's "Add" button and the phone's + button. */
export function createItems(navigate: (to: string) => void): MenuItem[] {
  return [
    { label: 'Download from a link', icon: 'download', hint: 'D', onSelect: () => navigate('/downloads') },
    { label: 'New performer', icon: 'user', onSelect: () => navigate('/performers?new=1') },
    { label: 'New studio', icon: 'building', onSelect: () => navigate('/studios?new=1') },
    { label: 'New tag', icon: 'tag', onSelect: () => navigate('/tags?new=1') },
    { label: 'New group', icon: 'layers', onSelect: () => navigate('/groups?new=1') },
    { label: 'Look for new files now', icon: 'refresh', onSelect: () => { api.post(`${V1}/library/scan`).then(() => setTimeout(announceChange, 2500)).catch(() => {}) } },
  ]
}

export default function Layout() {
  const navigate = useNavigate()
  const loc = useLocation()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])

  const [crumb, setCrumbRaw] = useState('')
  const setCrumb = useCallback((t: string) => setCrumbRaw(t), [])

  const { settings } = useApp()
  const { locked, unlock } = useLock()
  const [dash, setDash] = useState<Dashboard | null>(null)
  const [filters, setFilters] = useState<SavedFilter[]>([])
  const load = useCallback(() => {
    api.get<Dashboard>(`${V1}/dashboard`).then(setDash).catch(() => {})
    api.get<SavedFilter[]>(`${V1}/filters`).then(setFilters).catch(() => {})
  }, [])
  useEffect(() => { load() }, [loc.pathname, load])
  useOnChange(load)
  // the counts follow scans and downloads that finish in the background
  useEffect(() => { const t = window.setInterval(load, 60_000); return () => window.clearInterval(t) }, [load])

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [fabMenu, setFabMenu] = useState<Anchor | null>(null)

  useEffect(() => {
    const open = () => setShortcuts(true)
    window.addEventListener('psp:shortcuts', open)
    return () => window.removeEventListener('psp:shortcuts', open)
  }, [])
  // Esc twice: leave for the escape page (Settings → Privacy)
  const lastEsc = useRef(0)
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || !settings?.panic_url) return
      const now = performance.now()
      if (now - lastEsc.current < 600) { document.querySelectorAll('video').forEach(v => { v.pause(); v.removeAttribute('src') }); window.location.replace(settings.panic_url) }
      lastEsc.current = now
    }
    window.addEventListener('keydown', h, true)
    return () => window.removeEventListener('keydown', h, true)
  }, [settings?.panic_url])
  // a link pasted anywhere (not into a field) goes to Downloads
  useEffect(() => {
    const h = (e: ClipboardEvent) => {
      const t = e.target as HTMLElement
      if (t.closest('input, textarea, [contenteditable="true"]') || modalOpen()) return
      const text = e.clipboardData?.getData('text') || ''
      if (/^https?:\/\/\S+$/i.test(text.trim())) { e.preventDefault(); navigate(`/downloads?url=${encodeURIComponent(text.trim())}`) }
    }
    window.addEventListener('paste', h)
    return () => window.removeEventListener('paste', h)
  }, [navigate])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'h': go('/'); break
        case 's': if (!loc.pathname.startsWith('/scenes/')) go('/scenes'); break
        case 'v': if (!loc.pathname.startsWith('/scenes/')) go('/scenes?vr=yes'); break
        case 'p': if (!loc.pathname.startsWith('/scenes/')) go('/performers'); break
        case 'm': if (!loc.pathname.startsWith('/scenes/')) go('/markers'); break
        case 'd': go('/downloads'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'home', label: 'Home', icon: 'home', hint: 'H', run: () => navigate('/') },
    { id: 'scenes', label: 'All scenes', icon: 'film', hint: 'S', keywords: 'library videos', run: () => navigate('/scenes') },
    { id: 'vr', label: 'VR scenes', icon: 'vr', hint: 'V', keywords: 'headset 180 360', run: () => navigate('/scenes?vr=yes') },
    { id: 'continue', label: 'Continue watching', icon: 'play', run: () => navigate('/scenes?progress=1') },
    { id: 'fav', label: 'Favourites', icon: 'heart', run: () => navigate('/scenes?favorite=1') },
    { id: 'performers', label: 'Performers', icon: 'user', hint: 'P', run: () => navigate('/performers') },
    { id: 'studios', label: 'Studios', icon: 'building', run: () => navigate('/studios') },
    { id: 'tags', label: 'Tags', icon: 'tag', run: () => navigate('/tags') },
    { id: 'markers', label: 'Markers', icon: 'marker', hint: 'M', keywords: 'moments positions', run: () => navigate('/markers') },
    { id: 'groups', label: 'Groups', icon: 'layers', keywords: 'movies series', run: () => navigate('/groups') },
    { id: 'galleries', label: 'Galleries', icon: 'image', keywords: 'pictures photos images', run: () => navigate('/galleries') },
    { id: 'downloads', label: 'Downloads', icon: 'download', hint: 'D', keywords: 'link youtube yt-dlp metube', run: () => navigate('/downloads') },
    { id: 'stats', label: 'Numbers', icon: 'chart', keywords: 'stats history', run: () => navigate('/stats') },
    { id: 'duplicates', label: 'Duplicates', icon: 'copy', keywords: 'same phash', run: () => navigate('/duplicates') },
    { id: 'random', label: 'Surprise me (a random scene)', icon: 'shuffle', keywords: 'random pick', run: () => { api.get<Page<Scene>>(`${V1}/scenes?per=1&sort=random&seed=${Math.floor(Math.random() * 1e6)}`).then(p => { if (p.items.length) navigate(`/scenes/${p.items[0].id}`) }).catch(() => {}) } },
    { id: 'scan', label: 'Look for new files now', icon: 'refresh', keywords: 'scan rescan library', run: () => { api.post(`${V1}/library/scan`).then(() => setTimeout(announceChange, 2500)).catch(() => {}) } },
    { id: 'headset', label: 'Settings: headset (DeoVR, HereSphere)', icon: 'vr', keywords: 'quest oculus deovr heresphere', run: () => navigate('/settings/headset') },
    { id: 'trash', label: 'Trash', icon: 'trash', keywords: 'deleted restore', run: () => navigate('/scenes?trash=1') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  return (
    <CrumbCtx.Provider value={setCrumb}>
      <ShellCtx.Provider value={{ dash, filters, reload: load }}>
        <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
          {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
          <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
            <Sidebar onNavigate={closeDrawer} dash={dash} filters={filters} />
          </div>
          <div className="main">
            <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} crumb={crumb} />
            <main className="content"><div className="content-inner"><Outlet /></div></main>
          </div>
          {mobile && loc.pathname !== '/downloads' && !loc.pathname.startsWith('/scenes/') && <button type="button" className="fab" aria-label="New" title="New" aria-haspopup="menu" aria-expanded={!!fabMenu} onClick={e => setFabMenu(fabMenu ? null : rectOf(e.currentTarget))}><Icon name="plus" size={26} strokeWidth={2.2} /></button>}
          {fabMenu && <Menu anchor={fabMenu} align="right" items={createItems(navigate)} onClose={() => setFabMenu(null)} width={250} />}
          {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} />}
          {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
          {locked && <LockScreen onUnlock={unlock} />}
        </div>
      </ShellCtx.Provider>
    </CrumbCtx.Provider>
  )
}

const SECTION: Record<string, [string, string]> = {
  '': ['Home', '/'], scenes: ['Scenes', '/scenes'], performers: ['Performers', '/performers'], studios: ['Studios', '/studios'], tags: ['Tags', '/tags'],
  markers: ['Markers', '/markers'], groups: ['Groups', '/groups'], galleries: ['Galleries', '/galleries'], downloads: ['Downloads', '/downloads'],
  stats: ['Numbers', '/stats'], duplicates: ['Duplicates', '/duplicates'],
}

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen, crumb }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; crumb: string }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [section, sectionTo] = SECTION[loc.pathname.split('/')[1] || ''] || ['', '/']
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('psp:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {crumb ? <><button type="button" className="crumb" onClick={() => navigate(sectionTo)}>{section}</button><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="ellipsis">{crumb}</span></> : section}
      </h1>
      <span className="spacer" />
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K or /)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
