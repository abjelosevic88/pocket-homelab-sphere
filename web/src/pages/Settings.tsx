import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, postFile, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, AIStatus, Headset, LibraryStatus, Settings as SettingsT, ToolStatus, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, ago, fmtBytes, fmtDateTime, setDateFormats, type DateFormat, type TimeFormat } from '../lib/format'
import { SORTS } from '../lib/sphere'
import { Avatar, Confirm, CopyField, CoreHint, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast, errMsg } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { PocketModelSelect } from '../components/PocketModelSelect'
import { announceChange } from '../components/Layout'

type TabId = 'general' | 'player' | 'downloads' | 'lookups' | 'headset' | 'library' | 'notifications' | 'ai' | 'profile' | 'people' | 'tokens' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'general', label: 'General', icon: 'settings', sub: 'How lists look, privacy on a shared screen, date formats and the app title.' },
  { id: 'player', label: 'Player', icon: 'play', sub: 'Starting, resuming, the next scene, sound, VR and when a sitting counts as watched.' },
  { id: 'downloads', label: 'Downloads', icon: 'download', sub: 'Where videos from links go, in what quality, what is filled in — and cookies for sites that need a sign-in.' },
  { id: 'lookups', label: 'Lookups', icon: 'search', sub: 'StashDB and TPDB find what a scene is from its fingerprint or name, with performers, studio, tags and cover.' },
  { id: 'headset', label: 'Headset', icon: 'vr', sub: 'Watch on a Quest or another headset: in its browser, or in DeoVR or HereSphere.' },
  { id: 'library', label: 'Library', icon: 'folder', sub: 'The library folder, the pictures and previews made in the background, finding names in files — and bringing Stash over.' },
  { id: 'notifications', label: 'Notifications', icon: 'bell', sub: 'A push when a download finishes or fails.' },
  { id: 'ai', label: 'AI', icon: 'sparkles', sub: 'The AI behind the Ask button: Pocket AI or your own OpenAI key.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'tokens', label: 'API & shortcuts', icon: 'key', sub: 'Personal tokens for widgets, phone shortcuts and scripts.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Export your ratings, favourites, O\'s and history, import them again, download the database.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]
// the old ?tab= names
const ALIAS: Record<string, TabId> = { users: 'people', api: 'tokens', integrations: 'ai', apps: 'ai', stashdb: 'lookups', vr: 'headset' }

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const legacy = new URLSearchParams(window.location.search).get('tab') || ''
  const want = raw || legacy
  const tab: TabId = TABS.find(t => t.id === (ALIAS[want] || want))?.id || 'general'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })

  useEffect(() => { if (!raw && legacy) navigate(`/settings/${tab}`, { replace: true }) }, [raw, legacy, tab, navigate])
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])

  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back to your library" title="Back to your library (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span style={{ flex: 1 }} />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title">
              <h2>{current.label}</h2>
              <p>{current.sub}</p>
            </div>
            {tab === 'general' && <General />}
            {tab === 'player' && <PlayerTab />}
            {tab === 'downloads' && <DownloadsTab />}
            {tab === 'lookups' && <Lookups />}
            {tab === 'headset' && <HeadsetTab />}
            {tab === 'library' && <LibraryTab />}
            {tab === 'notifications' && <Notifications />}
            {tab === 'ai' && <AISettings />}
            {tab === 'profile' && <Profile />}
            {tab === 'people' && <Users />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

/** A settings card: title, explanation, actions on the right — Pocket Calendar's section. */
function Section({ title, sub, icon, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (patch?: Partial<SettingsT>) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...patch }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

/** "Test connection" results per integration. */
function useTester() {
  const [test, setTest] = useState<Record<string, string>>({})
  const run = async (which: string, url: string, key: string, extra: Record<string, string> = {}) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key, ...extra }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${(e as Error).message}` })) }
  }
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  return { run, status }
}

function SaveRow({ save, busy }: { save: () => void; busy: boolean }) {
  return <div className="row wrap"><button type="button" className="btn primary" onClick={save} disabled={busy}>Save changes</button></div>
}

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  const { toast } = useToast()
  const { setSettings, settings } = useApp()
  const [pin, setPin] = useState<'set' | 'change' | 'remove' | null>(null)
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Lists" icon="grid">
        <SettingRow title="Picture size"><Segmented size="sm" value={f.grid_size} onChange={v => setF({ ...f, grid_size: v })} options={[{ value: 's', label: 'Small' }, { value: 'm', label: 'Medium' }, { value: 'l', label: 'Large' }]} /></SettingRow>
        <SettingRow title="Order of scenes" help="When a list does not say otherwise."><Select ariaLabel="Order" value={f.default_sort} onChange={v => setF({ ...f, default_sort: v })} options={SORTS.map(s => ({ value: s.value, label: s.label }))} /></SettingRow>
        <SettingRow title="Preview when pointing at a scene" help="A few silent seconds from across the video."><Switch checked={f.hover_preview} onChange={v => setF({ ...f, hover_preview: v })} label="Preview when pointing" /></SettingRow>
        <SettingRow title="Dates"><Select ariaLabel="Date format" value={f.date_format} onChange={v => { setF({ ...f, date_format: v }); setDateFormats(v as DateFormat, f.time_format as TimeFormat) }} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        <SettingRow title="Times"><Select ariaLabel="Time format" value={f.time_format} onChange={v => { setF({ ...f, time_format: v }); setDateFormats(f.date_format as DateFormat, v as TimeFormat) }} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        {isAdmin && <SettingRow title="App title" help="For everyone on this box."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} /></SettingRow>}
      </Section>
      <Section title="Privacy" icon="shield" sub="For a screen others can see. The tab is always called “Pocket Sphere”; nothing here shows outside the app.">
        <SettingRow title="Blur pictures" help="Covers, photos and previews stay blurred until you point at them."><Switch checked={f.discreet} onChange={v => setF({ ...f, discreet: v })} label="Blur pictures" /></SettingRow>
        <SettingRow title="Escape page" help="Press Esc twice to leave at once for this page (videos stop first)." stack><input value={f.panic_url} onChange={e => setF({ ...f, panic_url: e.target.value })} placeholder="https://www.wikipedia.org" /></SettingRow>
        <SettingRow title="Lock with a PIN" help={f.lock_enabled ? 'On: the app asks for your PIN when it opens on this device.' : 'Asks for a PIN when the app opens, and after a while away.'}>
          {f.lock_enabled
            ? <span className="row"><button type="button" className="btn sm" onClick={() => setPin('change')}>Change PIN</button><button type="button" className="btn sm danger-text" onClick={() => setPin('remove')}>Turn off</button></span>
            : <button type="button" className="btn sm" onClick={() => setPin('set')}><Icon name="lock" size={14} />Set a PIN</button>}
        </SettingRow>
        {f.lock_enabled && <SettingRow title="Lock again after" help="Minutes without touching it (or with the tab hidden). 0 = only when the app opens."><input type="number" min={0} max={1440} value={f.lock_minutes} onChange={e => setF({ ...f, lock_minutes: Number(e.target.value) })} className="set-time" /></SettingRow>}
      </Section>
      <SaveRow save={() => save()} busy={busy} />
      {pin && <PinModal mode={pin} onClose={() => setPin(null)} onDone={on => { setPin(null); if (settings) { setSettings({ ...settings, lock_enabled: on }); setF({ ...f, lock_enabled: on }) } toast(on ? 'PIN set' : 'The lock is off') }} />}
    </div>
  )
}

function PinModal({ mode, onClose, onDone }: { mode: 'set' | 'change' | 'remove'; onClose: () => void; onDone: (on: boolean) => void }) {
  const { toast } = useToast()
  const [cur, setCur] = useState('')
  const [pin, setPin] = useState('')
  const [pin2, setPin2] = useState('')
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (mode !== 'remove' && pin !== pin2) { toast('The two PINs are not the same', 'err'); return }
    try {
      if (mode === 'remove') { await fetch(`${V1}/lock`, { method: 'DELETE', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ current: cur }) }).then(async r => { if (!r.ok) throw new Error((await r.json()).error) }); onDone(false) }
      else { await api.put(`${V1}/lock`, { pin, current: cur }); sessionStorage.setItem('psp-unlocked-at', String(Date.now())); onDone(true) }
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const digits = (v: string) => v.replace(/\D/g, '').slice(0, 12)
  return (
    <Modal title={mode === 'remove' ? 'Turn the lock off' : mode === 'change' ? 'Change the PIN' : 'Set a PIN'} onClose={onClose} size="sm">
      <form onSubmit={submit} className="grid" style={{ gap: 12 }}>
        {mode !== 'set' && <Field label="Current PIN"><input type="password" inputMode="numeric" value={cur} onChange={e => setCur(digits(e.target.value))} autoFocus /></Field>}
        {mode !== 'remove' && <>
          <Field label="New PIN" help="4 to 12 digits."><input type="password" inputMode="numeric" value={pin} onChange={e => setPin(digits(e.target.value))} autoFocus={mode === 'set'} /></Field>
          <Field label="Once more"><input type="password" inputMode="numeric" value={pin2} onChange={e => setPin2(digits(e.target.value))} /></Field>
        </>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary">{mode === 'remove' ? 'Turn off' : 'Save'}</button></div>
      </form>
    </Modal>
  )
}

function PlayerTab() {
  const { f, setF, save, busy } = useSettingsForm()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Watching" icon="play">
        <SettingRow title="Start playing when a scene opens"><Switch checked={f.auto_play} onChange={v => setF({ ...f, auto_play: v })} label="Start playing" /></SettingRow>
        <SettingRow title="Continue where you stopped"><Switch checked={f.resume_play} onChange={v => setF({ ...f, resume_play: v })} label="Continue where you stopped" /></SettingRow>
        <SettingRow title="Play the next one in the list" help="At the end of a scene opened from a list."><Switch checked={f.continue_queue} onChange={v => setF({ ...f, continue_queue: v })} label="Play the next one" /></SettingRow>
        <SettingRow title="Jump" help="Seconds for ← and →."><input type="number" min={1} max={120} value={f.skip_seconds} onChange={e => setF({ ...f, skip_seconds: Number(e.target.value) })} className="set-time" /></SettingRow>
        <SettingRow title="Counts as watched after" help="Seconds of watching in one sitting."><input type="number" min={5} max={3600} value={f.count_after} onChange={e => setF({ ...f, count_after: Number(e.target.value) })} className="set-time" /></SettingRow>
      </Section>
      <Section title="Quality" icon="gauge" sub="Files the browser can play as they are always play as they are. The rest are converted while you watch — to this size.">
        <SettingRow title="Converted videos"><Select ariaLabel="Converted quality" value={f.stream_quality} onChange={v => setF({ ...f, stream_quality: v })} options={[{ value: 'original', label: 'As big as the file' }, { value: '2160', label: '2160p' }, { value: '1440', label: '1440p' }, { value: '1080', label: '1080p (good on Wi-Fi)' }, { value: '720', label: '720p (good on mobile data)' }, { value: '480', label: '480p' }]} /></SettingRow>
      </Section>
      <Section title="VR scenes" icon="vr">
        <SettingRow title="Open VR scenes" help="Look around: drag to turn the view. One eye: one side of the picture, flat, for a normal screen. Whole frame: both sides as the file has them."><Segmented size="sm" value={f.vr_default_mode} onChange={v => setF({ ...f, vr_default_mode: v })} options={[{ value: 'view', label: 'Look around' }, { value: 'eye', label: 'One eye (2D)' }, { value: 'flat', label: 'Whole frame' }]} /></SettingRow>
      </Section>
      <SaveRow save={() => save()} busy={busy} />
    </div>
  )
}

function DownloadsTab() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  const { toast } = useToast()
  const { data: tool, reload } = useAsync(() => api.get<ToolStatus>(`${V1}/downloads/tool`), [])
  const [updating, setUpdating] = useState(false)
  if (!f) return <Loading />
  const yt = tool?.ytdlp
  return (
    <div className="stack loose">
      <Section title="New downloads" icon="download" sub="The Downloads page starts with these; each download can choose differently.">
        <SettingRow title="Folder in the library" help="Empty = the library folder itself."><input value={f.download_folder} onChange={e => setF({ ...f, download_folder: e.target.value })} placeholder="e.g. Downloads" /></SettingRow>
        <SettingRow title="Quality"><Select ariaLabel="Quality" value={f.download_quality} onChange={v => setF({ ...f, download_quality: v })} options={[{ value: 'best', label: 'Best' }, { value: '2160', label: '2160p' }, { value: '1440', label: '1440p' }, { value: '1080', label: '1080p' }, { value: '720', label: '720p' }, { value: '480', label: '480p' }]} /></SettingRow>
        <SettingRow title="Format"><Select ariaLabel="Format" value={f.download_format} onChange={v => setF({ ...f, download_format: v })} options={[{ value: 'auto', label: 'Auto (MP4 when it can)' }, { value: 'mp4', label: 'MP4' }, { value: 'mkv', label: 'MKV' }, { value: 'webm', label: 'WebM' }]} /></SettingRow>
        <SettingRow title="One folder per video" help="The video with its picture and info file, like the rest of the library."><Switch checked={f.own_folder} onChange={v => setF({ ...f, own_folder: v })} label="One folder per video" /></SettingRow>
      </Section>
      <Section title="Filled in from the site" icon="sparkles">
        <SettingRow title="The channel or uploader becomes the studio"><Switch checked={f.uploader_studio} onChange={v => setF({ ...f, uploader_studio: v })} label="Uploader as studio" /></SettingRow>
        <SettingRow title="Take the site’s tags" help="Up to 20."><Switch checked={f.site_tags} onChange={v => setF({ ...f, site_tags: v })} label="Site tags" /></SettingRow>
      </Section>
      <SaveRow save={() => save()} busy={busy} />
      <Section title="Cookies" icon="key" sub="For sites that only show a video when you are signed in: export the site's cookies from your browser as cookies.txt (Netscape format, e.g. with the “Get cookies.txt LOCALLY” add-on). They are yours only and used for your downloads." actions={<span className={`badge ${tool?.cookies ? 'ok' : ''}`}>{tool?.cookies ? 'added' : 'none'}</span>}>
        <div className="row wrap">
          <label className="btn sm"><Icon name="upload" size={14} />{tool?.cookies ? 'Replace…' : 'Add cookies.txt…'}<input type="file" accept=".txt,text/plain" hidden onChange={async e => { const file = e.target.files?.[0]; e.target.value = ''; if (!file) return; try { await fetch(`${V1}/downloads/cookies`, { method: 'PUT', body: file }).then(async r => { if (!r.ok) throw new Error((await r.json()).error) }); toast('Cookies saved'); reload() } catch (err) { toast(errMsg(err), 'err') } }} /></label>
          {tool?.cookies && <button type="button" className="btn sm danger-text" onClick={async () => { await api.del(`${V1}/downloads/cookies`); toast('Cookies removed'); reload() }}>Remove</button>}
        </div>
      </Section>
      <Section title="yt-dlp" icon="download" sub="The downloader behind it. Sites change often, so the app keeps its own copy and updates it every day." actions={<span className={`badge ${yt?.error ? '' : 'ok'}`}>{yt?.version || '—'}</span>}>
        {yt?.error && <div className="callout bad"><Icon name="alert" size={15} /><span>{yt.error}</span></div>}
        <p className="muted small">{yt?.own ? `Updated ${yt.updated_at ? ago(yt.updated_at) : 'when the app started'}.` : 'Managed outside the app (YTDLP).'} At most {tool?.max ?? 2} downloads run at the same time.{tool && !tool.members ? ' Only admins may download.' : ''}</p>
        {isAdmin && yt?.own && <button type="button" className="btn sm" disabled={updating} onClick={async () => { setUpdating(true); try { const r = await api.post<{ version: string }>(`${V1}/downloads/tool/update`); toast(`yt-dlp ${r.version}`); reload() } catch (e) { toast(errMsg(e), 'err') } finally { setUpdating(false) } }}><Icon name="refresh" size={14} />{updating ? 'Updating…' : 'Update now'}</button>}
      </Section>
    </div>
  )
}

function Lookups() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="StashDB" icon="search" sub={<>Free community database of scenes, matched by the file's fingerprint. Get a key at <a href="https://stashdb.org" target="_blank" rel="noreferrer">stashdb.org</a> (sign up by invite) → your profile → API key.</>}>
        <SettingRow title="API key" stack><input value={f.stashdb_key} onChange={e => setF({ ...f, stashdb_key: e.target.value })} autoComplete="off" placeholder="eyJ…" /></SettingRow>
        <SettingRow title="Another stash-box (optional)" help="e.g. https://fansdb.cc/graphql — empty = StashDB." stack><input value={f.stashbox_url} onChange={e => setF({ ...f, stashbox_url: e.target.value })} placeholder="https://stashdb.org/graphql" /></SettingRow>
        <div className="row"><button type="button" className="btn sm" onClick={() => run('stashdb', f.stashbox_url, f.stashdb_key)} disabled={!f.stashdb_key}>Test</button>{status('stashdb')}</div>
      </Section>
      <Section title="TPDB" icon="search" sub="Finds scenes by file name and hash. Free key on its website → profile → API tokens.">
        <SettingRow title="API key" stack><input value={f.tpdb_key} onChange={e => setF({ ...f, tpdb_key: e.target.value })} autoComplete="off" /></SettingRow>
        <div className="row"><button type="button" className="btn sm" onClick={() => run('tpdb', '', f.tpdb_key)} disabled={!f.tpdb_key}>Test</button>{status('tpdb')}</div>
      </Section>
      <Section title="By themselves" icon="wand">
        <SettingRow title="Look up new videos" help="When a new file's fingerprint matches exactly one scene, it is filled in (title, date, studio, performers with photos, tags, cover)."><Switch checked={f.auto_identify} onChange={v => setF({ ...f, auto_identify: v })} label="Look up new videos" /></SettingRow>
      </Section>
      <SaveRow save={() => save()} busy={busy} />
    </div>
  )
}

function HeadsetTab() {
  const { toast } = useToast()
  const { data, reload } = useAsync(() => api.get<Headset>(`${V1}/settings/headset`), [])
  const [confirm, setConfirm] = useState<'new' | 'off' | null>(null)
  const act = async (action: 'new' | 'off') => { try { await api.post(`${V1}/settings/headset`, { action }); reload(); toast(action === 'new' ? 'New address made' : 'Headset address turned off') } catch (e) { toast(errMsg(e), 'err') } }
  if (!data) return <Loading />
  return (
    <div className="stack loose">
      <Section title="In the headset's browser" icon="globe" sub="Open this app in the Quest Browser (or any browser with WebXR), start a VR scene and press the headset button next to the VR button. Your sign-in, ratings and history work as on the computer." />
      <Section title="DeoVR and HereSphere" icon="vr" sub="These players cannot sign in, so they get their own address with a secret in it. Anyone with the address sees your library — keep it to yourself, and make a new one if it leaks." actions={data.enabled && <span className="badge ok">on</span>}>
        {!data.available && <div className="callout warn"><Icon name="info" size={15} /><span>The admin turned the headset feeds off (HEADSET_APIS=false).</span></div>}
        {data.enabled ? <div className="stack">
          <Field label="DeoVR" help="In DeoVR: the browser tab → type this address."><CopyField value={data.deovr || ''} /></Field>
          <Field label="HereSphere" help="In HereSphere: the web browser → this address. Ratings and favourites you set in HereSphere come back here, and so does where you stopped."><CopyField value={data.heresphere || ''} /></Field>
          <div className="row"><button type="button" className="btn sm" onClick={() => setConfirm('new')}><Icon name="refresh" size={14} />Make a new address</button><button type="button" className="btn sm danger-text" onClick={() => setConfirm('off')}>Turn off</button></div>
        </div> : <button type="button" className="btn primary" onClick={() => act('new')} disabled={!data.available}><Icon name="vr" size={15} />Make my headset address</button>}
      </Section>
      {confirm && <Confirm title={confirm === 'new' ? 'Make a new address?' : 'Turn the headset address off?'} confirmLabel={confirm === 'new' ? 'Make a new one' : 'Turn off'} message="The old address stops working at once; headsets using it need the new one." onCancel={() => setConfirm(null)} onConfirm={() => { const c = confirm; setConfirm(null); act(c) }} />}
    </div>
  )
}

function LibraryTab() {
  const { user } = useApp()
  const { toast } = useToast()
  const isAdmin = user?.role === 'admin'
  const { data, reload } = useAsync(() => api.get<LibraryStatus>(`${V1}/library`), [])
  useEffect(() => { const t = window.setInterval(reload, 5000); return () => window.clearInterval(t) }, [reload])
  const [blobs, setBlobs] = useState('')
  const [importing, setImporting] = useState(false)
  const [res, setRes] = useState<Record<string, number> | null>(null)
  const [regen, setRegen] = useState<'all' | 'missing' | null>(null)
  const sys = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const importStash = async (file: File) => {
    setImporting(true); setRes(null)
    try { const r = await postFile<Record<string, number>>(`${V1}/import/stash${blobs ? `?blobs=${encodeURIComponent(blobs)}` : ''}`, file); setRes(r); announceChange(); toast('Stash library brought over') } catch (e) { toast(errMsg(e), 'err') } finally { setImporting(false) }
  }
  const autotag = async () => { const r = await api.post<{ changed: number }>(`${V1}/library/autotag`, { performers: true, studios: true, tags: true }); toast(`${r.changed} scenes changed`); announceChange() }
  if (!data) return <Loading />
  const working = Object.values(data.working)
  return (
    <div className="stack loose">
      <Section title="The library folder" icon="folder" sub={<>Read every few minutes; new files show up by themselves. Renamed or moved files keep everything (they are known by their fingerprint). Deleted scenes wait in <code>.trash</code> before they are gone.</>}
        actions={<button type="button" className="btn sm" onClick={async () => { await api.post(`${V1}/library/scan`); toast('Looking now'); setTimeout(() => { reload(); announceChange() }, 2500) }}><Icon name="refresh" size={14} />Look now</button>}>
        <dl className="set-dl">
          {isAdmin && sys.data?.library_dir != null && <div><dt>Folder</dt><dd><code>{String(sys.data.library_dir)}</code>{data.free_bytes >= 0 && ` · ${fmtBytes(data.free_bytes)} free`}</dd></div>}
          <div><dt>Last look</dt><dd>{data.scan.at ? `${fmtDateTime(data.scan.at)} · ${data.scan.scenes} videos${data.scan.added ? ` · ${data.scan.added} new` : ''}${data.scan.moved ? ` · ${data.scan.moved} moved` : ''}${data.scan.missing ? ` · ${data.scan.missing} gone` : ''} · ${data.scan.ms} ms` : 'not yet'}</dd></div>
          {data.scan.error && <div><dt>Problem</dt><dd className="bad">{data.scan.error}</dd></div>}
        </dl>
      </Section>
      <Section title="Made in the background" icon="image" sub="For every video: a cover (when it has none), the pictures under the player, a short preview and the fingerprint (to find duplicates and look it up). VR videos get flat pictures, not the doubled fisheye frame.">
        {!data.ffmpeg && <div className="callout bad"><Icon name="alert" size={15} /><span>ffmpeg is missing: {data.ffmpeg_error}. The container image has it; for a bare binary install ffmpeg.</span></div>}
        <dl className="set-dl">
          <div><dt>Waiting</dt><dd>{data.to_read} to read · {data.to_make} to make · {data.to_hash} to fingerprint{working.length ? ` · now: ${working.join(', ')}` : ''}</dd></div>
          <div><dt>ffmpeg</dt><dd>{data.ffmpeg_version || '—'}{data.hwaccel ? ` · converting on the GPU (${data.hwaccel})` : ' · converting on the CPU'}{data.streams ? ` · ${data.streams} streams now` : ''}</dd></div>
        </dl>
        {isAdmin && <div className="row wrap"><button type="button" className="btn sm" onClick={() => setRegen('missing')}>Make the missing ones</button><button type="button" className="btn sm" onClick={() => setRegen('all')}>Make all again</button><Link to="/duplicates" className="btn sm"><Icon name="copy" size={14} />Find duplicates</Link></div>}
      </Section>
      <Section title="Names in file names" icon="wand" sub="Puts performers, studios and tags on every scene whose file path mentions them (by name or other name). Add a performer, then run this.">
        <button type="button" className="btn sm" onClick={autotag}><Icon name="wand" size={14} />Find them now</button>
      </Section>
      {isAdmin && <Section title="Bring Stash over" icon="upload" sub={<>Upload a copy of Stash's database (<code>stash-go.sqlite</code>, in Stash's config folder). Scenes are matched to the files here by Stash's own fingerprints; titles, details, links, studios, performers, tags, groups, markers and StashDB ids come over — and for you: ratings, favourites, O's, play history and where you stopped. Running it twice changes nothing new.</>}>
        <Field label="Stash's blobs folder (optional)" help="Where Stash keeps pictures (its blobs folder), as this server sees it. Without it, performer and studio pictures are left out unless Stash kept them in the database."><input value={blobs} onChange={e => setBlobs(e.target.value)} placeholder="/stash/blobs" /></Field>
        <label className="btn"><Icon name="upload" size={15} />{importing ? 'Bringing it over…' : 'Choose stash-go.sqlite…'}<input type="file" hidden onChange={e => { const file = e.target.files?.[0]; e.target.value = ''; if (file) importStash(file) }} disabled={importing} /></label>
        {res && <p className="muted small mt-sm">{res.scenes ?? 0} scenes matched{res.not_found ? ` (${res.not_found} files not in this library)` : ''} · {res.performers ?? 0} new performers · {res.studios ?? 0} studios · {res.tags ?? 0} tags · {res.groups ?? 0} groups · {res.markers ?? 0} markers · {res.plays ?? 0} plays · {res.os ?? 0} O's · {res.covers ?? 0} covers</p>}
      </Section>}
      {regen && <Confirm title={regen === 'all' ? 'Make every picture and preview again?' : 'Make the missing pictures and previews?'} danger={false} confirmLabel="Start" message={regen === 'all' ? 'This reads every video again and can take hours for a big library. Covers you chose stay.' : 'Only scenes whose pictures or preview are missing or failed.'} onCancel={() => setRegen(null)} onConfirm={async () => { const m = regen; setRegen(null); await api.post(`${V1}/library/regenerate`, { missing: m === 'missing' }); toast('Started'); reload() }} />}
    </div>
  )
}

function Notifications() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Push" icon="bell" actions={<span className={`badge ${f.notify_configured ? 'ok' : ''}`}>{f.notify_configured ? 'on' : 'not set'}</span>}
        sub={<>Apprise gateway (<code>…/notify/&lt;key&gt;</code>), an ntfy topic URL, or any webhook that takes JSON. On a box with Pocket Alerts use its Apprise address.</>}>
        <SettingRow title="URL" stack><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://apprise:8000/notify/sphere" /></SettingRow>
        <SettingRow title="Format"><Segmented value={f.notify_format} onChange={v => setF({ ...f, notify_format: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'json', label: 'JSON' }]} size="sm" /></SettingRow>
      </Section>
      <Section title="Tell me when" icon="inbox">
        <SettingRow title="A download finishes or fails"><Switch checked={f.notify_downloads} onChange={v => setF({ ...f, notify_downloads: v })} label="Downloads" /></SettingRow>
        <SettingRow title="Without titles" help="“A new video is in your library” instead of its title — for notifications on a lock screen."><Switch checked={f.notify_quiet} onChange={v => setF({ ...f, notify_quiet: v })} label="Without titles" /></SettingRow>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        <button type="button" className="btn" onClick={() => run('notify', f.notify_url, '', { format: f.notify_format })} disabled={!f.notify_url}><Icon name="mail" size={15} />Send a test</button>
        {status('notify')}
      </div>
    </div>
  )
}

function useAIStatus() {
  const [st, setSt] = useState<AIStatus | null>(null)
  useEffect(() => { api.get<AIStatus>(`${V1}/ai/status`).then(setSt).catch(() => {}) }, [])
  return st
}

function AISettings() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const ai = useAIStatus()
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const shown = !coreOn && f.ai_provider === 'pocket' ? 'openai' : (f.ai_provider || 'auto')
  return (
    <div className="stack loose">
      <Section title="What the AI sees" icon="sparkles" sub="Only what you ask it in the Ask panel, with the page you are on. Nothing is sent by itself; no other Pocket app can read your library unless you switch its link on in Pocket Core.">
        {ai && (ai.ready ? <p className="muted small row" style={{ gap: 6 }}><Icon name="sparkles" size={13} />{ai.provider === 'pocket' ? 'Pocket AI' : 'Your OpenAI key'} answers.</p> : <div className="callout info"><Icon name="info" size={15} /><div>No AI is connected{ai.detail ? ` (${ai.detail})` : ''}.{!f.core_configured && <CoreHint />}</div></div>)}
      </Section>
      <Section title="AI" icon="sparkles">
        <div className="grid" style={{ gap: 12 }}>
          <Field group label="Which AI" help="Automatic prefers your enabled Pocket AI connection; without one it uses your OpenAI settings.">
            <Select ariaLabel="AI provider" value={shown} onChange={v => setF({ ...f, ai_provider: v as SettingsT['ai_provider'] })} options={[{ value: 'auto', label: coreOn ? 'Automatic — prefer Pocket AI' : 'Automatic' }, { value: 'pocket', label: 'Pocket AI', disabled: !coreOn, hint: coreOn ? undefined : 'needs Pocket Core' }, { value: 'openai', label: 'OpenAI / compatible API' }]} />
          </Field>
          {!coreOn && <CoreHint />}
          {coreOn && shown !== 'openai' && <PocketModelSelect value={f.ai_pocket_model || ''} onChange={v => setF({ ...f, ai_pocket_model: v })} />}
          {(shown === 'openai' || (!coreOn && shown === 'auto')) && <>
            <Field label="OpenAI API key" help="platform.openai.com → API keys. Stored on the server; shown masked."><input value={f.openai_api_key} onChange={e => setF({ ...f, openai_api_key: e.target.value })} autoComplete="off" placeholder="sk-…" /></Field>
            <div className="two-col tight">
              <Field label="Model" help="Default gpt-4o-mini."><input value={f.openai_model} onChange={e => setF({ ...f, openai_model: e.target.value })} placeholder="gpt-4o-mini" /></Field>
              <Field label="Base URL (optional)" help="Ollama, LM Studio, OpenRouter…"><input value={f.openai_base_url} onChange={e => setF({ ...f, openai_base_url: e.target.value })} placeholder="https://api.openai.com/v1" /></Field>
            </div>
          </>}
        </div>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        {(shown === 'openai' || (!coreOn && shown === 'auto')) && <><button type="button" className="btn" onClick={() => run('openai', f.openai_base_url, f.openai_api_key)} disabled={!f.openai_api_key}>Test the OpenAI key</button>{status('openai')}</>}
      </div>
    </div>
  )
}

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}
      sub="Everyone signs in with their own account and has their own ratings, favourites, O's, history, downloads and settings; the library is shared. Admins manage accounts; they do not see what anyone else watches.">
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar name={u.name} email={u.email} size={34} />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}{u.role === 'admin' && <span className="badge accent">admin</span>}</div>
              <div className="muted small ellipsis">{u.email}</div>
            </div>
            {isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with their ratings, history and downloads list. Videos they added stay." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <div className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={56} />
        <div className="grow"><h3>{user?.name || user?.email}</h3><p className="muted small">{user?.email} · {user?.role}</p></div>
      </div>
      <Section title="You">
        <div className="stack">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="Leave empty to keep the current one."><div className="stack">
        <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
        <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
      </div></Section>}
      <div className="row"><button className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const base = settings?.base_url || window.location.origin
  return (
    <div className="stack loose">
      <Section title="Personal tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can read and change everything, feed a Homepage widget (<code>GET /api/v1/dashboard</code>) or send links from a phone shortcut. See <code>docs/api.md</code>.</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Homepage widget)" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="Handy endpoints" icon="smartphone" sub={<>At <code>{base}/api/v1</code>, with <code>Authorization: Bearer &lt;token&gt;</code>.</>}>
        <dl className="set-dl">
          <div><dt><code>POST /downloads</code></dt><dd className="muted"><code>{'{"url":"https://…"}'}</code> — download a video (an iOS Shortcut from the share sheet).</dd></div>
          <div><dt><code>GET /dashboard</code></dt><dd className="muted">Counts — for a Homepage widget.</dd></div>
          <div><dt><code>GET /scenes?q=…</code></dt><dd className="muted">Search the library (also <code>vr</code>, <code>favorite</code>, <code>performers</code>, <code>tags</code>, <code>sort</code>).</dd></div>
          <div><dt><code>POST /library/scan</code></dt><dd className="muted">Look for new files now.</dd></div>
        </dl>
        <p className="muted small">Headset players (DeoVR, HereSphere) use their own address — see Settings → Headset.</p>
      </Section>
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user } = useApp()
  const [busy, setBusy] = useState(false)
  const [res, setRes] = useState<Record<string, number> | null>(null)
  const send = async (file: File) => {
    setBusy(true); setRes(null)
    try { const r = await postFile<Record<string, number>>(`${V1}/import`, file); setRes(r); announceChange(); toast('Imported') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack loose">
      <Section title="Export" icon="download" sub="Your ratings, favourites, O's, play history, where you stopped, favourite performers and studios, saved views and followed channels, as one JSON file. Scenes are named by fingerprint and path, so it works on another box with the same files.">
        <a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />Export JSON</a>
      </Section>
      <Section title="Import" icon="upload" sub="A Pocket Sphere export. Running it twice changes nothing the second time. Coming from Stash? Use Settings → Library.">
        <label className="btn"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Choose a file…'}<input type="file" accept=".json,application/json" hidden onChange={e => { const file = e.target.files?.[0]; e.target.value = ''; if (file) send(file) }} /></label>
        {res && <p className="muted small mt-sm">{res.scenes} scenes · {res.plays} plays · {res.os} O's · {res.marks} favourites · {res.filters} views · {res.subscriptions} channels{res.not_found ? ` · ${res.not_found} scenes not in this library` : ''}</p>}
      </Section>
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="The whole box, every account — admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Covers and photos live next to it under <code>files/</code>; <code>generated/</code> (scrubber pictures, previews) can always be made again; the videos are in the library folder. Restore by stopping the app and replacing <code>pocket-sphere.db</code> in the data directory.</p>
        <a className="btn primary" href={`${V1}/backup`}><Icon name="download" size={15} />Download database</a>
      </Section>}
    </div>
  )
}
function About() {
  const { data, error } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const [keys, setKeys] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const counts = (data.counts || {}) as Record<string, number>
  const scan = (data.scan || {}) as Record<string, number>
  const tiles: [string, number | undefined, string][] = [['Videos', scan.scenes, 'film'], ['Your plays', counts.plays, 'play'], ['Your O\'s', counts.o_events, 'droplet'], ['Your downloads', counts.downloads, 'download']]
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Database', <span key="db">{data.db_path ? <><code>{String(data.db_path)}</code> · </> : null}{fmtBytes(Number(data.db_bytes))} · covers and photos {fmtBytes(Number(data.files_bytes))} · made files {fmtBytes(Number(data.generated_bytes))}</span>],
    ['Library', <span key="lib">{data.library_dir ? <><code>{String(data.library_dir)}</code> · </> : null}{Number(data.free_bytes) >= 0 ? `${fmtBytes(Number(data.free_bytes))} free` : ''}</span>],
    ['Converting', data.hwaccel ? `on the GPU (${String(data.hwaccel)})` : 'on the CPU'],
    ['People', `${counts.users ?? 0} accounts on this box`],
  ]
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Sphere</h3>
          <p className="muted small">Your video library with a VR player: scenes, performers, studios, tags and markers, previews when you point at a scene, fingerprints that find what a file is, downloads from a link and DeoVR/HereSphere on the headset. One Go binary, one SQLite file, one volume.</p>
        </div>
      </div>
      <div className="set-stats">
        {tiles.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{n ?? 0}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>
      <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setKeys(true)}><Icon name="keyboard" size={15} />Keyboard shortcuts</button>
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-sphere" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
      <p className="muted small">Open source (MIT). Part of the Pocket apps; it tells the others nothing about what you watch unless you switch a link on in Pocket Core. Downloads use <a href="https://github.com/yt-dlp/yt-dlp" target="_blank" rel="noreferrer">yt-dlp</a>, pictures and conversions <a href="https://ffmpeg.org" target="_blank" rel="noreferrer">ffmpeg</a>; lookups use StashDB and TPDB with your own keys.</p>
      {keys && <ShortcutsSheet onClose={() => setKeys(false)} />}
    </div>
  )
}
