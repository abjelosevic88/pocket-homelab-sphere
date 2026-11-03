// The app lock: a PIN asked when the app opens and after a while away. It
// keeps the screen to yourself on a shared device; the account itself is
// protected by signing in.
import { useEffect, useRef, useState } from 'react'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { Icon } from './icons'

const KEY = 'psp-unlocked-at'

export function useLock() {
  const { settings } = useApp()
  const enabled = !!settings?.lock_enabled
  const minutes = settings?.lock_minutes ?? 0
  const [locked, setLocked] = useState(() => enabled && !sessionStorage.getItem(KEY))
  useEffect(() => { if (enabled && !sessionStorage.getItem(KEY)) setLocked(true); if (!enabled) setLocked(false) }, [enabled])
  const last = useRef(Date.now())
  useEffect(() => {
    if (!enabled) return
    const seen = () => { last.current = Date.now() }
    const vis = () => {
      if (document.visibilityState === 'hidden') { last.current = Date.now(); return }
      if (minutes > 0 && Date.now() - last.current > minutes * 60_000) { sessionStorage.removeItem(KEY); setLocked(true) }
    }
    const tick = window.setInterval(() => { if (minutes > 0 && Date.now() - last.current > minutes * 60_000) { sessionStorage.removeItem(KEY); setLocked(true) } }, 15_000)
    const key = (e: KeyboardEvent) => { if (e.shiftKey && e.key === 'L' && !(e.target as HTMLElement).closest('input, textarea')) { sessionStorage.removeItem(KEY); setLocked(true) } }
    window.addEventListener('pointerdown', seen)
    window.addEventListener('keydown', seen)
    window.addEventListener('keydown', key)
    document.addEventListener('visibilitychange', vis)
    return () => { window.clearInterval(tick); window.removeEventListener('pointerdown', seen); window.removeEventListener('keydown', seen); window.removeEventListener('keydown', key); document.removeEventListener('visibilitychange', vis) }
  }, [enabled, minutes])
  return { locked, unlock: () => { sessionStorage.setItem(KEY, String(Date.now())); last.current = Date.now(); setLocked(false) } }
}

export function LockScreen({ onUnlock }: { onUnlock: () => void }) {
  const [pin, setPin] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => { input.current?.focus() }, [])
  useEffect(() => { document.querySelectorAll('video').forEach(v => v.pause()) }, [])
  const submit = async (e?: React.FormEvent) => {
    e?.preventDefault()
    if (pin.length < 4) return
    setBusy(true)
    try { await api.post(`${V1}/lock`, { pin }); onUnlock() } catch { setErr('That is not the PIN'); setPin('') } finally { setBusy(false); input.current?.focus() }
  }
  return (
    <div className="lock-screen" role="dialog" aria-label="Locked">
      <form className="lock-box" onSubmit={submit}>
        <img src="/favicon.svg" alt="" width={56} height={56} />
        <h1>Pocket Sphere is locked</h1>
        <p className="muted">Type your PIN to continue.</p>
        <input ref={input} className="lock-pin" type="password" inputMode="numeric" autoComplete="off" maxLength={12} value={pin} onChange={e => { setPin(e.target.value.replace(/\D/g, '')); setErr('') }} aria-label="PIN" />
        {err && <p className="bad small">{err}</p>}
        <button className="btn primary lg" disabled={busy || pin.length < 4}><Icon name="lock" size={16} />Unlock</button>
      </form>
    </div>
  )
}
