import { useEffect, useRef, useState } from 'react'
import { Loader2, SendHorizontal, Sparkles } from 'lucide-react'
import { useAction, useUiData } from '../lib/query'
import { useFloatingTerminals } from '../lib/floatingTerminals'
import { AgentPicker } from './pickers'

// Ask Flow — a global, icon-only entry point that lives in the topbar beside
// "New task". Clicking it opens an anchored popover with a prompt input, the
// Claude/Codex picker, and Open; submitting spins up an adhoc floating-terminal
// session via the same `overview-chat` action the old banner used.
//
// It is deliberately NOT a solid primary button: that role belongs to "New
// task". Instead it's a "liquid glass" chip with a calm flow-accent pulse, so
// it reads as an ambient assistant trigger rather than a second primary CTA.
// Outside-click / Escape close it (mirrors the app's menu idiom); the input
// autofocuses on open.
export function AskFlow() {
  const [open, setOpen] = useState(false)
  const [prompt, setPrompt] = useState('')
  const [provider, setProvider] = useState('claude')
  const { data: ui } = useUiData()
  const { open: openFloatingTerminal } = useFloatingTerminals()
  const action = useAction()
  const wrapRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  const providers = ui?.CAPABILITIES.providers ?? []
  // Fall back to the first available provider when the stored pick is missing
  // or uninstalled, so Open never fires against an unavailable agent.
  const available = providers.filter((p) => p.available !== false)
  const effectiveProvider = available.length
    ? available.some((p) => p.id === provider)
      ? provider
      : available[0].id
    : provider

  useEffect(() => {
    if (!open) return
    inputRef.current?.focus()
    const onDown = (e: globalThis.MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const submit = () => {
    const text = prompt.trim()
    if (!text || action.isPending) return
    action.mutate(
      { kind: 'overview-chat', prompt: text, provider: effectiveProvider },
      {
        onSuccess: (resp) => {
          setPrompt('')
          setOpen(false)
          if (resp.floating_terminal) openFloatingTerminal(resp.floating_terminal)
        },
      },
    )
  }

  return (
    <div className="ask-flow-menu" ref={wrapRef}>
      <button
        type="button"
        className={`ask-flow-trigger${open ? ' open' : ''}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label="Ask Flow"
        title="Ask Flow"
        onClick={() => setOpen((o) => !o)}
      >
        <Sparkles size={16} />
      </button>
      {open && (
        <div className="ask-flow-pop" role="dialog" aria-label="Ask Flow">
          <div className="eyebrow">Ask Flow</div>
          <input
            ref={inputRef}
            className="input ask-flow-input"
            aria-label="Ask Flow prompt"
            value={prompt}
            disabled={action.isPending}
            placeholder="Triage my day, inspect stalled sessions, or route work into tasks…"
            onChange={(e) => setPrompt(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                submit()
              }
            }}
          />
          <div className="ask-flow-pop-actions">
            <AgentPicker value={effectiveProvider} onChange={setProvider} providers={providers} />
            <button type="button" className="btn primary" disabled={!prompt.trim() || action.isPending} onClick={submit}>
              {action.isPending ? <Loader2 size={15} className="spin" /> : <SendHorizontal size={15} />}
              Open
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
