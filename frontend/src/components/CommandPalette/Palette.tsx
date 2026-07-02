import { useEffect, useMemo, useRef, useState } from "react"
import { useStore } from "../../store/store"
import { fetchSearch } from "../../api/client"
import { buildCommands, type Command, type CommandCtx, type PaletteTab, type PromptSpec } from "../../commands"
import { laneColor } from "../Graph/EdgeRenderer"
import { Kbd } from "../core/Kbd"
import "./Palette.css"

interface PaletteProps {
    reload: () => Promise<void>
}

// the three Figma tabs
const TABS: { id: PaletteTab; label: string }[] = [
    { id: "global", label: "Global" },
    { id: "commit", label: "Commit" },
    { id: "branch", label: "Branch" },
]

type Mode = "tab" | "search" | "prompt"

// run something, showing any failure inline instead of swallowing it
function attempt(fn: () => void | Promise<void>, onError: (m: string) => void) {
    Promise.resolve()
        .then(fn)
        .catch((e) => onError(e instanceof Error ? e.message : String(e)))
}

// compact relative time, e.g. "2h ago"
function timeAgo(iso: string): string {
    const then = new Date(iso).getTime()
    if (Number.isNaN(then)) return ""
    let value = Math.max(0, Math.round((Date.now() - then) / 1000))
    const steps: [string, number][] = [["s", 60], ["m", 60], ["h", 24], ["d", 30], ["mo", 12]]
    for (const [suffix, size] of steps) {
        if (value < size) return `${value}${suffix} ago`
        value = Math.floor(value / size)
    }
    return `${value}y ago`
}

interface ListItem {
    id: string
    label: string
    group?: string
    dot?: string
    danger?: boolean
    meta?: string
    keys?: string[]
    onSelect: () => void | Promise<void>
}

export function Palette({ reload }: PaletteProps) {
    const isOpen = useStore((s) => s.isPaletteOpen)
    const close = useStore((s) => s.closePalette)
    const selectedSHA = useStore((s) => s.selectedSHA)
    const rows = useStore((s) => s.rows)
    const refs = useStore((s) => s.refs)
    const selectCommit = useStore((s) => s.selectCommit)
    const searchResults = useStore((s) => s.searchResults)
    const setSearchResults = useStore((s) => s.setSearchResults)
    const requestConfirm = useStore((s) => s.requestConfirm)
    const openRepoPicker = useStore((s) => s.openRepoPicker)

    const [mode, setMode] = useState<Mode>("tab")
    const [tab, setTab] = useState<PaletteTab>("global")
    const [query, setQuery] = useState("")
    const [active, setActive] = useState(0)
    const [promptSpec, setPromptSpec] = useState<PromptSpec | null>(null)
    const [error, setError] = useState<string | null>(null)
    const inputRef = useRef<HTMLInputElement>(null)
    const activeRef = useRef<HTMLDivElement>(null)

    const selectedRow = useMemo(
        () => (selectedSHA ? rows.find((r) => r.sha === selectedSHA) ?? null : null),
        [selectedSHA, rows],
    )

    // reset when the palette opens; default to the commit tab if one is selected
    useEffect(() => {
        if (!isOpen) return
        setMode("tab")
        setTab(selectedSHA ? "commit" : "global")
        setQuery("")
        setActive(0)
        setPromptSpec(null)
        setError(null)
        requestAnimationFrame(() => inputRef.current?.focus())
    }, [isOpen]) // eslint-disable-line react-hooks/exhaustive-deps

    // clear transient state whenever the view changes
    useEffect(() => {
        setActive(0)
        setError(null)
    }, [mode, tab])

    // commit search → query the backend (debounced)
    useEffect(() => {
        if (mode !== "search") return
        const q = query.trim()
        if (!q) {
            setSearchResults([])
            return
        }
        let cancelled = false
        const t = setTimeout(() => {
            fetchSearch(q)
                .then((r) => !cancelled && setSearchResults(r))
                .catch(() => !cancelled && setError("search unavailable"))
        }, 120)
        return () => {
            cancelled = true
            clearTimeout(t)
        }
    }, [mode, query, setSearchResults])

    const ctx: CommandCtx = useMemo(
        () => ({
            selectedSHA,
            selectedRow,
            refs,
            rows,
            reload,
            selectCommit,
            close,
            confirm: (message, run) => requestConfirm({ message, run }),
            prompt: (spec) => {
                setPromptSpec(spec)
                setQuery(spec.initial ?? "")
                setMode("prompt")
            },
            enterSearch: () => {
                setQuery("")
                setSearchResults([])
                setMode("search")
            },
            openRepoPicker,
        }),
        [selectedSHA, selectedRow, refs, rows, reload, selectCommit, close, requestConfirm, openRepoPicker, setSearchResults],
    )

    // the rows to render, grouped, for the current view
    const groups: { label?: string; items: ListItem[] }[] = useMemo(() => {
        if (mode === "search") {
            const bySha = new Map(rows.map((r) => [r.sha, r]))
            const items: ListItem[] = searchResults.map((res) => {
                const row = bySha.get(res.sha)
                return {
                    id: res.sha,
                    label: row?.subject ?? res.sha.slice(0, 7),
                    meta: row?.short ?? res.sha.slice(0, 7),
                    dot: row ? laneColor(row.lane) : undefined,
                    onSelect: () => {
                        selectCommit(res.sha)
                        close()
                    },
                }
            })
            return [{ items }]
        }

        if (mode === "prompt") return []

        // tab mode — build commands, filter by query, bucket by group
        const cmds: Command[] = buildCommands(tab, ctx)
        const q = query.toLowerCase()
        const matches = cmds.filter((c) => c.label.toLowerCase().includes(q))

        const order: string[] = []
        const byGroup = new Map<string, ListItem[]>()
        for (const c of matches) {
            const g = c.group ?? "Commands"
            let bucket = byGroup.get(g)
            if (!bucket) {
                bucket = []
                byGroup.set(g, bucket)
                order.push(g)
            }
            bucket.push({
                id: c.id,
                label: c.label,
                group: g,
                dot: c.dot,
                danger: c.danger,
                keys: c.keys ?? (c.shortcut ? [c.shortcut] : undefined),
                onSelect: () => c.run(ctx),
            })
        }
        return order.map((label) => ({ label, items: byGroup.get(label)! }))
    }, [mode, tab, query, ctx, rows, searchResults, selectCommit, close])

    const flatItems = useMemo(() => groups.flatMap((g) => g.items), [groups])
    const indexById = useMemo(() => {
        const m = new Map<string, number>()
        flatItems.forEach((it, i) => m.set(it.id, i))
        return m
    }, [flatItems])

    useEffect(() => {
        setActive((a) => Math.min(a, Math.max(0, flatItems.length - 1)))
    }, [flatItems.length])
    useEffect(() => {
        activeRef.current?.scrollIntoView({ block: "nearest" })
    }, [active])

    if (!isOpen) return null

    function cycleTab(dir: number) {
        const i = TABS.findIndex((t) => t.id === tab)
        setTab(TABS[(i + dir + TABS.length) % TABS.length].id)
        setQuery("")
        setMode("tab")
    }

    function backOut() {
        if (mode === "prompt" || mode === "search") {
            setMode("tab")
            setQuery("")
            setPromptSpec(null)
            setSearchResults([])
            return
        }
        if (query) {
            setQuery("")
            return
        }
        close()
    }

    function onKeyDown(e: React.KeyboardEvent) {
        switch (e.key) {
            case "Escape":
                e.preventDefault()
                backOut()
                return
            case "Tab":
                if (mode === "tab") {
                    e.preventDefault()
                    cycleTab(e.shiftKey ? -1 : 1)
                }
                return
            case "ArrowDown":
                if (mode !== "prompt") {
                    e.preventDefault()
                    setActive((a) => Math.min(a + 1, flatItems.length - 1))
                }
                return
            case "ArrowUp":
                if (mode !== "prompt") {
                    e.preventDefault()
                    setActive((a) => Math.max(a - 1, 0))
                }
                return
            case "Enter": {
                e.preventDefault()
                if (mode === "prompt" && promptSpec) {
                    const value = query.trim()
                    if (!value && !promptSpec.allowEmpty) return
                    const spec = promptSpec
                    attempt(() => spec.run(query.trim(), ctx), setError)
                    return
                }
                const item = flatItems[active]
                if (item) attempt(item.onSelect, setError)
                return
            }
            case "/":
                // empty command filter + "/" → jump to commit search (Figma keycap)
                if (mode === "tab" && query === "") {
                    e.preventDefault()
                    ctx.enterSearch()
                }
                return
        }
    }

    const placeholder =
        mode === "search" ? "Search commits…" : mode === "prompt" ? promptSpec?.placeholder ?? "Type a value…" : "Type a command…"

    const showTabs = mode === "tab" && query === ""
    const showCommitHint = mode === "tab" && tab === "commit" && !selectedRow

    const emptyLabel =
        mode === "search"
            ? query.trim()
                ? "No commits match"
                : "Type to search commits…"
            : showCommitHint
              ? "Select a commit in the graph to act on it"
              : "No matches"

    return (
        <div className="cp__overlay" onMouseDown={close}>
            <div className="cp" onMouseDown={(e) => e.stopPropagation()}>
                {/* search / input bar */}
                <div className="cp__search">
                    <input
                        ref={inputRef}
                        className="cp__input"
                        placeholder={placeholder}
                        value={query}
                        onChange={(e) => setQuery(e.target.value)}
                        onKeyDown={onKeyDown}
                        spellCheck={false}
                        autoComplete="off"
                    />
                    <Kbd size={28}>{mode === "tab" ? "/" : "esc"}</Kbd>
                </div>

                {/* tabs (hidden while filtering, per the Figma "searching" state) */}
                {showTabs && (
                    <div className="cp__tabs">
                        {TABS.map((t) => (
                            <button
                                key={t.id}
                                className={"cp__tab" + (t.id === tab ? " cp__tab--active" : "")}
                                onMouseDown={(e) => {
                                    e.preventDefault()
                                    setTab(t.id)
                                }}
                            >
                                {t.label}
                            </button>
                        ))}
                    </div>
                )}

                {/* prompt title bar */}
                {mode === "prompt" && promptSpec && (
                    <div className="cp__promptbar">
                        <span className="cp__prompttitle">{promptSpec.title}</span>
                    </div>
                )}

                {/* selected-commit context (commit tab) */}
                {mode === "tab" && tab === "commit" && selectedRow && (
                    <div className="cp__context">
                        <span className="cp__ctxdot" style={{ background: laneColor(selectedRow.lane) }} />
                        <span className="cp__ctxtext">
                            <span className="cp__ctxtitle">{selectedRow.subject}</span>
                            <span className="cp__ctxsub">
                                <span className="cp__ctxsha">{selectedRow.short}</span>
                                &nbsp;&nbsp;{selectedRow.author.name} · {timeAgo(selectedRow.timestamp)}
                            </span>
                        </span>
                    </div>
                )}

                {error && <div className="cp__error">{error}</div>}

                {/* list */}
                {mode !== "prompt" && (
                    <div className="cp__list">
                        {flatItems.length === 0 && <div className="cp__empty">{emptyLabel}</div>}
                        {groups.map((group, gi) => (
                            <div className="cp__group" key={group.label ?? gi}>
                                {group.label && showTabs && <div className="cp__grouphead">{group.label}</div>}
                                {group.items.map((item) => {
                                    const idx = indexById.get(item.id) ?? -1
                                    const isActive = idx === active
                                    return (
                                        <div
                                            key={item.id}
                                            ref={isActive ? activeRef : undefined}
                                            className={
                                                "cp__row" +
                                                (isActive ? " cp__row--active" : "") +
                                                (item.danger ? " cp__row--danger" : "")
                                            }
                                            onMouseEnter={() => setActive(idx)}
                                            onMouseDown={(e) => {
                                                e.preventDefault()
                                                attempt(item.onSelect, setError)
                                            }}
                                        >
                                            {item.dot !== undefined && (
                                                <span className="cp__rowdot" style={{ background: item.dot }} />
                                            )}
                                            <span className="cp__label">{item.label}</span>
                                            {item.meta && <span className="cp__meta">{item.meta}</span>}
                                            {item.keys && item.keys.length > 0 && (
                                                <span className="cp__keys">
                                                    {item.keys.map((k, ki) => (
                                                        <Kbd size={28} key={ki}>
                                                            {k}
                                                        </Kbd>
                                                    ))}
                                                </span>
                                            )}
                                        </div>
                                    )
                                })}
                            </div>
                        ))}
                    </div>
                )}

                {/* footer */}
                <div className="cp__footer">
                    {mode === "prompt" ? (
                        <span className="cp__hint">
                            <Kbd size={26}>↵</Kbd> {promptSpec?.submitLabel ?? "confirm"} ·&nbsp;
                            <Kbd size={26}>esc</Kbd> cancel
                        </span>
                    ) : mode === "search" ? (
                        <span className="cp__hint">
                            <Kbd size={26}>↑</Kbd>
                            <Kbd size={26}>↓</Kbd> navigate ·&nbsp;
                            <Kbd size={26}>↵</Kbd> select ·&nbsp;
                            <Kbd size={26}>esc</Kbd> back
                        </span>
                    ) : (
                        <span className="cp__hint">
                            <Kbd size={26}>Tab</Kbd> switch tabs ·&nbsp;
                            <Kbd size={26}>↵</Kbd> run
                        </span>
                    )}
                    <span className="cp__brand">[ gitvis ]</span>
                </div>
            </div>
        </div>
    )
}
