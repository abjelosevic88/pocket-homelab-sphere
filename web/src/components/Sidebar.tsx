// Left column: the add button, sections with counts and saved views — the Pocket Calendar sidebar.
import { type ReactNode, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import type { Dashboard, SavedFilter } from '../lib/types'
import { Icon } from './icons'
import { createItems } from './Layout'
import { type Anchor, Menu, rectOf } from './ui'

export function Sidebar({ onNavigate, dash, filters }: { onNavigate: () => void; dash: Dashboard | null; filters: SavedFilter[] }) {
  const { status, settings } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const title = settings?.title || status?.title || 'Pocket Sphere'
  const q = new URLSearchParams(loc.search)
  const only = (key: string, val: string) => loc.pathname === '/scenes' && q.get(key) === val && [...q.keys()].filter(k => !['sort', 'dir', 'seed'].includes(k)).length === 1
  const all = loc.pathname === '/scenes' && ![...q.keys()].some(k => !['sort', 'dir', 'seed'].includes(k))
  const nav: { to: string; label: string; icon: string; active: boolean; count?: number }[] = [
    { to: '/', label: 'Home', icon: 'home', active: loc.pathname === '/' },
    { to: '/scenes', label: 'Scenes', icon: 'film', active: all, count: dash?.scenes },
    { to: '/scenes?vr=yes', label: 'VR', icon: 'vr', active: only('vr', 'yes'), count: dash?.vr },
    { to: '/scenes?progress=1', label: 'Continue watching', icon: 'play', active: only('progress', '1'), count: dash?.in_progress },
    { to: '/scenes?favorite=1', label: 'Favourites', icon: 'heart', active: only('favorite', '1'), count: dash?.favorites },
    { to: '/scenes?unwatched=1', label: 'Not watched yet', icon: 'eye-off', active: only('unwatched', '1'), count: dash?.unwatched },
    { to: '/downloads', label: 'Downloads', icon: 'download', active: loc.pathname === '/downloads', count: dash?.downloads },
  ]
  const browse: { to: string; label: string; icon: string; count?: number }[] = [
    { to: '/performers', label: 'Performers', icon: 'user', count: dash?.performers },
    { to: '/studios', label: 'Studios', icon: 'building', count: dash?.studios },
    { to: '/tags', label: 'Tags', icon: 'tag', count: dash?.tags },
    { to: '/markers', label: 'Markers', icon: 'marker', count: dash?.markers },
    { to: '/groups', label: 'Groups', icon: 'layers', count: dash?.groups },
    { to: '/galleries', label: 'Galleries', icon: 'image', count: dash?.galleries },
    { to: '/stats', label: 'Numbers', icon: 'chart' },
  ]
  const pinned = filters.filter(f => f.pinned && f.mode === 'scenes')
  return (
    <aside className="sidebar" aria-label="Pocket Sphere">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Sphere' ? title : <>Pocket <b>Sphere</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}>
        <Icon name="plus" size={17} strokeWidth={2.2} />Add<Icon name="chevron-down" size={15} className="create-caret" />
      </button>
      {menu && <Menu anchor={menu} items={createItems(to => { onNavigate(); navigate(to) })} onClose={() => setMenu(null)} width={248} />}

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} onClick={onNavigate} className={() => `sb-nav-item ${n.active ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.count && <span className="sb-count">{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      <Group title="Browse">
        <ul className="cal-list">{browse.map(b => (
          <li key={b.to} className="cal-row sb-row">
            <NavLink to={b.to} onClick={onNavigate} className={() => `cal-check ${loc.pathname.startsWith(b.to) ? 'is-active' : ''}`}>
              <span className="sb-ico"><Icon name={b.icon} size={13} /></span>
              <span className="cal-name ellipsis">{b.label}</span>
              {!!b.count && <span className="sb-count">{b.count}</span>}
            </NavLink>
          </li>
        ))}</ul>
      </Group>

      <Group title="Saved views">
        {pinned.length === 0
          ? <p className="sb-empty">Save a filtered list of scenes (“4K VR, newest first”) and pin it here.</p>
          : <ul className="cal-list">{pinned.map(f => {
            const to = `/scenes?${f.query}`
            return (
              <li key={f.id} className="cal-row sb-row">
                <NavLink to={to} onClick={onNavigate} className={() => `cal-check ${loc.pathname + loc.search === to ? 'is-active' : ''}`} title={f.name}>
                  <span className="sb-ico"><Icon name="filter" size={13} /></span>
                  <span className="cal-name ellipsis">{f.name}</span>
                </NavLink>
              </li>
            )
          })}</ul>}
      </Group>

      <div className="sb-foot">
        {!!dash?.trash && <Link to="/scenes?trash=1" className="sb-foot-link" onClick={onNavigate}><Icon name="trash" size={15} />Trash<span className="sb-count">{dash.trash}</span></Link>}
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('psp:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}

function Group({ title, children }: { title: string; children: ReactNode }) {
  const key = `psp-sb-${title}`
  const [open, setOpen] = useState(() => localStorage.getItem(key) !== '0')
  const toggle = () => setOpen(o => { localStorage.setItem(key, o ? '0' : '1'); return !o })
  return (
    <section className="sb-sec">
      <h4 className="sb-h">
        <button type="button" className="sb-h-btn" aria-expanded={open} onClick={toggle}><Icon name={open ? 'chevron-down' : 'chevron-right'} size={13} />{title}</button>
      </h4>
      {open && children}
    </section>
  )
}
