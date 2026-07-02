import { useCallback, useEffect } from "react"
import { fetchGraph, fetchRefs } from "./api/client"
import { useStore } from "./store/store"
import { Canvas } from "./components/Graph/Canvas"
import { Palette } from "./components/CommandPalette/Palette"
import { Panel } from "./components/CommitPanel/Panel"
import { StatusBar } from "./components/StatusBar/StatusBar"
import { RepoPicker } from "./components/RepoPicker/RepoPicker"
import { ConfirmDialog } from "./components/ConfirmDialog/ConfirmDialog"
import "./App.css"

function App() {
    const isLoading = useStore((s) => s.isLoading)
    const rows = useStore((s) => s.rows)
    const refs = useStore((s) => s.refs)
    const selectedSHA = useStore((s) => s.selectedSHA)
    const isPaletteOpen = useStore((s) => s.isPaletteOpen)
    const isRepoPickerOpen = useStore((s) => s.isRepoPickerOpen)
    const isConfirmOpen = useStore((s) => s.confirm !== null)
    const setRows = useStore((s) => s.setRows)
    const setRefs = useStore((s) => s.setRefs)
    const setLoading = useStore((s) => s.setLoading)
    const selectCommit = useStore((s) => s.selectCommit)
    const openPalette = useStore((s) => s.openPalette)
    const closePalette = useStore((s) => s.closePalette)

    const load = useCallback(async () => {
        setLoading(true)
        try {
            const [graph, refsResp] = await Promise.all([fetchGraph(), fetchRefs()])
            setRows(graph)
            setRefs(refsResp)
        } catch (err) {
            console.error("failed to load graph:", err)
        } finally {
            setLoading(false)
        }
    }, [setRows, setRefs, setLoading])

    // initial load
    useEffect(() => {
        void load()
    }, [load])

    // move selection up/down the row list
    const moveSelection = useCallback(
        (delta: number) => {
            if (rows.length === 0) return
            const idx = rows.findIndex((r) => r.sha === selectedSHA)
            const next = idx < 0 ? 0 : Math.min(Math.max(idx + delta, 0), rows.length - 1)
            selectCommit(rows[next].sha)
        },
        [rows, selectedSHA, selectCommit],
    )

    // global keyboard shortcuts
    useEffect(() => {
        function onKey(e: KeyboardEvent) {
            // modal dialogs own the keyboard while open
            if (isRepoPickerOpen || isConfirmOpen) return

            // ⌘K / Ctrl+K toggles the palette regardless of focus
            if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
                e.preventDefault()
                if (isPaletteOpen) closePalette()
                else openPalette()
                return
            }
            // while the palette is open it owns the keyboard
            if (isPaletteOpen) return
            // don't hijack typing in inputs
            const t = e.target
            if (t instanceof HTMLInputElement || t instanceof HTMLTextAreaElement) return

            switch (e.key) {
                case "j":
                case "ArrowDown":
                    e.preventDefault()
                    moveSelection(1)
                    break
                case "k":
                case "ArrowUp":
                    e.preventDefault()
                    moveSelection(-1)
                    break
                case "g":
                    if (refs?.head) selectCommit(refs.head)
                    break
                case "r":
                    void load()
                    break
                case "y":
                    if (selectedSHA) void navigator.clipboard.writeText(selectedSHA)
                    break
            }
        }
        window.addEventListener("keydown", onKey)
        return () => window.removeEventListener("keydown", onKey)
    }, [isPaletteOpen, isRepoPickerOpen, isConfirmOpen, openPalette, closePalette, moveSelection, refs, selectedSHA, selectCommit, load])

    return (
        <div className="app">
            <header className="app__header">
                <span className="app__brand">keyboard-first git visualizer</span>
                <span className="app__hint">
                    {isLoading ? "loading…" : `${rows.length} commits · ⌘K commands · j/k navigate · R refresh`}
                </span>
            </header>
            <main className="app__graph">
                <Canvas />
            </main>
            <StatusBar reload={load} />
            <Panel />
            <Palette reload={load} />
            <RepoPicker reload={load} />
            <ConfirmDialog />
        </div>
    )
}

export default App
