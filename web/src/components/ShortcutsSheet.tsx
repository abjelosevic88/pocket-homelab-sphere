import { Modal } from './ui'

const GROUPS: { title: string; keys: [string[], string][] }[] = [
  { title: 'Go to', keys: [[['h'], 'Home'], [['s'], 'Scenes'], [['v'], 'VR'], [['p'], 'Performers'], [['m'], 'Markers'], [['d'], 'Downloads'], [[','], 'Settings'], [['/'], 'Search & commands'], [['⌘', 'K'], 'Search & commands']] },
  { title: 'Player', keys: [[['space'], 'Play / pause'], [['←', '→'], 'Back / forward'], [['⇧', '←'], 'One minute back'], [['0–9'], 'Jump to 0–90 %'], [['f'], 'Full screen'], [['m'], 'Mute'], [['[', ']'], 'Slower / faster'], [[',', '.'], 'One frame (paused)'], [['o'], 'Add an O'], [['v'], 'VR: look around → one eye (2D) → whole frame'], [['r'], 'Look straight ahead (VR)'], [['n', 'p'], 'Next / previous in the list']] },
  { title: 'Privacy', keys: [[['esc', 'esc'], 'Leave quickly (when an escape page is set)'], [['⇧', 'L'], 'Lock now (when a PIN is set)']] },
  { title: 'Window', keys: [[['['], 'Show or hide the sidebar'], [['esc'], 'Close a dialog or menu'], [['?'], 'This sheet']] },
]

export function ShortcutsSheet({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose} size="lg">
      <div className="shortcuts">
        {GROUPS.map(g => (
          <section key={g.title}>
            <h4 className="sc-h">{g.title}</h4>
            {g.keys.map(([keys, label]) => (
              <div key={label + keys.join('')} className="sc-row"><span>{label}</span><span className="sc-keys">{keys.map((k, i) => <kbd key={k + i}>{k}</kbd>)}</span></div>
            ))}
          </section>
        ))}
      </div>
    </Modal>
  )
}
