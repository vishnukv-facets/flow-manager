import { useEffect, useRef, useState, type CSSProperties, type PointerEvent } from 'react'
import { Expand, Maximize2, Minus, Shrink, X } from 'lucide-react'
import { useLocation } from 'wouter'
import { TaskTerminal } from './Terminal'
import { ProviderIcon } from './ui'
import type { FloatingWindow } from '../lib/floatingTerminals'

interface Props {
  win: FloatingWindow
  pos: { x: number; y: number }
  z: number
  hidden: boolean
  onMove: (pos: { x: number; y: number }) => void
  onFocus: () => void
  onMinimize: () => void
  onClose: () => void
}

function clampPosition(x: number, y: number): { x: number; y: number } {
  if (typeof window === 'undefined') return { x, y }
  const maxX = Math.max(12, window.innerWidth - 140)
  const maxY = Math.max(12, window.innerHeight - 72)
  return {
    x: Math.min(Math.max(12, x), maxX),
    y: Math.min(Math.max(12, y), maxY),
  }
}

const MIN_W = 360
const MIN_H = 240

// maximizedRect returns the main CONTENT area — right of the sidebar (.rail) and
// BELOW the top-nav line (.topbar) — measured live so it tracks the actual
// layout. The page content lives in `.main > .content`, whose box is exactly
// that region, so we measure it directly. The floating window is
// fixed-positioned, so these viewport coords map straight to left/top.
function maximizedRect(): { left: number; top: number; width: number; height: number } {
  if (typeof window === 'undefined') return { left: 0, top: 0, width: 0, height: 0 }
  const content = document.querySelector('.main > .content')
  if (content) {
    const r = content.getBoundingClientRect()
    return {
      left: Math.round(r.left),
      top: Math.round(r.top),
      width: Math.max(MIN_W, Math.round(r.width)),
      height: Math.max(MIN_H, Math.round(r.height)),
    }
  }
  // Fallback: right of the sidebar, below the topbar.
  const rail = document.querySelector('.rail')
  const left = rail ? Math.round(rail.getBoundingClientRect().right) : 0
  const topbar = document.querySelector('.topbar')
  const top = topbar ? Math.round(topbar.getBoundingClientRect().bottom) : 0
  return { left, top, width: Math.max(MIN_W, window.innerWidth - left), height: Math.max(MIN_H, window.innerHeight - top) }
}

function defaultSize(): { w: number; h: number } {
  if (typeof window === 'undefined') return { w: 720, h: 460 }
  return { w: Math.min(720, window.innerWidth - 24), h: Math.min(460, window.innerHeight - 96) }
}

function clampSize(w: number, h: number): { w: number; h: number } {
  if (typeof window === 'undefined') return { w, h }
  return {
    w: Math.min(Math.max(MIN_W, w), window.innerWidth - 24),
    h: Math.min(Math.max(MIN_H, h), window.innerHeight - 24),
  }
}

export function FloatingTerminalWindow({ win, pos, z, hidden, onMove, onFocus, onMinimize, onClose }: Props) {
  const [, navigate] = useLocation()
  const [status, setStatus] = useState('connecting')
  // Drag is tracked locally and committed to the owner on release, so a drag
  // doesn't re-render every other window/tray chip on each pointer move.
  const [dragPos, setDragPos] = useState<{ x: number; y: number } | null>(null)
  const dragRef = useRef<{ dx: number; dy: number } | null>(null)
  // Size is window-local: the window stays mounted across navigation, so the
  // size sticks until reload (matching the position default behavior). The
  // terminal's ResizeObserver refits xterm automatically as this changes.
  const [size, setSize] = useState(defaultSize)
  const resizeRef = useRef<{ x: number; y: number; w: number; h: number } | null>(null)
  // Expand-to-fill toggle: when on, the window covers the viewport minus the
  // sidebar (recomputed live). maxRect is recomputed on toggle and on resize.
  const [maximized, setMaximized] = useState(false)
  const [maxRect, setMaxRect] = useState(maximizedRect)

  useEffect(() => {
    const onResize = () => {
      if (maximized) setMaxRect(maximizedRect())
      else onMove(clampPosition(pos.x, pos.y))
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [pos.x, pos.y, onMove, maximized])

  const toggleMaximize = () => {
    setMaximized((m) => {
      if (!m) setMaxRect(maximizedRect())
      return !m
    })
  }

  const onDragStart = (e: PointerEvent<HTMLDivElement>) => {
    // Never start a drag from an interactive control — otherwise the header's
    // pointer capture swallows the button's click (minimize/close break). A
    // maximized window is pinned, so it doesn't drag.
    if (maximized || e.button !== 0 || (e.target as HTMLElement).closest('button')) return
    const base = dragPos ?? pos
    dragRef.current = { dx: e.clientX - base.x, dy: e.clientY - base.y }
    setDragPos(base)
    e.currentTarget.setPointerCapture(e.pointerId)
  }

  const onDragMove = (e: PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current
    if (!drag) return
    setDragPos(clampPosition(e.clientX - drag.dx, e.clientY - drag.dy))
  }

  const onDragEnd = (e: PointerEvent<HTMLDivElement>) => {
    if (dragRef.current && dragPos) onMove(dragPos)
    dragRef.current = null
    setDragPos(null)
    if (e.currentTarget.hasPointerCapture(e.pointerId)) {
      e.currentTarget.releasePointerCapture(e.pointerId)
    }
  }

  const onResizeStart = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return
    e.stopPropagation()
    onFocus()
    resizeRef.current = { x: e.clientX, y: e.clientY, w: size.w, h: size.h }
    e.currentTarget.setPointerCapture(e.pointerId)
  }
  const onResizeMove = (e: PointerEvent<HTMLDivElement>) => {
    const r = resizeRef.current
    if (!r) return
    setSize(clampSize(r.w + (e.clientX - r.x), r.h + (e.clientY - r.y)))
  }
  const onResizeEnd = (e: PointerEvent<HTMLDivElement>) => {
    resizeRef.current = null
    if (e.currentTarget.hasPointerCapture(e.pointerId)) {
      e.currentTarget.releasePointerCapture(e.pointerId)
    }
  }

  const at = dragPos ?? pos
  const style = (maximized
    ? { left: maxRect.left, top: maxRect.top, zIndex: z, width: maxRect.width, height: maxRect.height }
    : { left: at.x, top: at.y, zIndex: z, width: size.w, height: size.h }) as CSSProperties

  return (
    <div
      className={`floating-terminal${hidden ? ' hidden' : ''}${maximized ? ' maximized' : ''}`}
      style={style}
      onPointerDownCapture={onFocus}
    >
      <div
        className="floating-terminal-head"
        onPointerDown={onDragStart}
        onPointerMove={onDragMove}
        onPointerUp={onDragEnd}
        onPointerCancel={onDragEnd}
      >
        <div className="floating-terminal-title">
          <span className={`dot ${status === 'connected' ? 'running' : 'idle'}`} />
          <span className="clip">{win.title || 'Ask Flow'}</span>
        </div>
        <div className="floating-terminal-meta">
          <span className="provider-chip">
            <ProviderIcon provider={win.provider} size={13} />
            {win.provider}
          </span>
          <span className="mono">{status}</span>
        </div>
        <div className="floating-terminal-actions">
        {win.kind === 'task' && (
          <button
            type="button"
            className="btn icon sm"
            title="Open full session page"
            aria-label="Open full session page"
            onClick={() => navigate(`/session/${win.id}`)}
          >
            <Maximize2 size={14} />
          </button>
        )}
        <button
          type="button"
          className="btn icon sm"
          title={maximized ? 'Restore' : 'Expand to fill'}
          aria-label={maximized ? 'Restore window' : 'Expand window to fill'}
          onClick={toggleMaximize}
        >
          {maximized ? <Shrink size={14} /> : <Expand size={14} />}
        </button>
        <button type="button" className="btn icon sm" title="Minimize to tray" onClick={onMinimize}>
          <Minus size={15} />
        </button>
        <button
          type="button"
          className="btn icon sm"
          title={win.kind === 'task' ? 'Close window (session keeps running)' : 'End session'}
          onClick={onClose}
        >
          <X size={15} />
        </button>
        </div>
      </div>
      {!hidden && (
        <div className="floating-terminal-body">
          <TaskTerminal
            slug={win.id}
            kind={win.kind}
            provider={win.provider}
            onStatus={(kind, message) => setStatus(kind === 'open' ? 'connected' : message || kind)}
          />
        </div>
      )}
      {!maximized && (
        <div
          className="floating-terminal-resize"
          title="Drag to resize"
          onPointerDown={onResizeStart}
          onPointerMove={onResizeMove}
          onPointerUp={onResizeEnd}
          onPointerCancel={onResizeEnd}
        />
      )}
    </div>
  )
}
