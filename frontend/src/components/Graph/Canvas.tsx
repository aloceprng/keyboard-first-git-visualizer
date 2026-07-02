import { useEffect, useRef } from "react"
import type { Row } from "../../api/types"
import { useStore } from "../../store/store"
import { ROW_HEIGHT, canvasHeight, laneToX, rowToY, yToRow } from "../../utils/canvas"
import * as render from "./EdgeRenderer"

const TEXT_GAP = 16 // px between the widest lane and the text column

function draw(
    ctx: CanvasRenderingContext2D,
    width: number,
    height: number,
    rows: Row[],
    selectedSHA: string | null,
): void {
    render.clear(ctx, width, height)
    if (rows.length === 0) return

    // 1. passthrough lines (active lanes with no commit in the row)
    for (const row of rows) {
        if (row.passthrough === 0) continue
        for (let lane = 0; lane < 32; lane++) {
            if (row.passthrough & (1 << lane)) render.drawPassthrough(ctx, lane, row.row)
        }
    }

    // 2. edges, 3. dots, 4. text — drawn in passes so dots sit above lines
    for (const row of rows) for (const edge of row.edges) render.drawEdge(ctx, row, edge)
    for (const row of rows) render.drawDot(ctx, row, row.sha === selectedSHA)

    const maxLane = rows.reduce((m, r) => Math.max(m, r.activeLanes), 1)
    const textX = laneToX(maxLane) + TEXT_GAP
    for (const row of rows) render.drawCommitText(ctx, row, textX, row.sha === selectedSHA)
}

export function Canvas() {
    const rows = useStore((s) => s.rows)
    const selectedSHA = useStore((s) => s.selectedSHA)
    const selectCommit = useStore((s) => s.selectCommit)
    const canvasRef = useRef<HTMLCanvasElement>(null)

    useEffect(() => {
        const canvas = canvasRef.current
        if (!canvas) return
        const ctx = canvas.getContext("2d")
        if (!ctx) return

        const dpr = window.devicePixelRatio || 1
        const cssWidth = canvas.clientWidth
        const cssHeight = canvasHeight(rows.length)

        // Size the backing store for the display's pixel density, then scale
        // the context so all drawing code works in CSS pixels.
        canvas.width = Math.floor(cssWidth * dpr)
        canvas.height = Math.floor(cssHeight * dpr)
        canvas.style.height = `${cssHeight}px`
        ctx.setTransform(dpr, 0, 0, dpr, 0, 0)

        const raf = requestAnimationFrame(() =>
            draw(ctx, cssWidth, cssHeight, rows, selectedSHA),
        )
        return () => cancelAnimationFrame(raf)
    }, [rows, selectedSHA])

    // scroll a freshly-selected commit into view if it's off-screen (jump / j-k)
    useEffect(() => {
        if (!selectedSHA) return
        const parent = canvasRef.current?.parentElement
        if (!parent) return
        const idx = rows.findIndex((r) => r.sha === selectedSHA)
        if (idx < 0) return
        const y = rowToY(rows[idx].row)
        const top = parent.scrollTop
        const bottom = top + parent.clientHeight
        if (y < top + ROW_HEIGHT || y > bottom - ROW_HEIGHT) {
            parent.scrollTo({ top: Math.max(0, y - parent.clientHeight / 2), behavior: "smooth" })
        }
    }, [selectedSHA, rows])

    function onClick(e: React.MouseEvent<HTMLCanvasElement>) {
        const rect = e.currentTarget.getBoundingClientRect()
        const row = yToRow(e.clientY - rect.top)
        if (row >= 0 && row < rows.length) selectCommit(rows[row].sha)
    }

    return (
        <canvas
            ref={canvasRef}
            onClick={onClick}
            style={{ width: "100%", height: rows.length * ROW_HEIGHT, display: "block" }}
        />
    )
}
