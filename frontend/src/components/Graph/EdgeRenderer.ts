// Stateless canvas drawing primitives. Every function takes a 2D context plus
// numbers/rows — none of them know about React or the store.

import type { Row, Edge } from "../../api/types"
import { EdgeType } from "../../api/types"
import { laneToX, rowToY, ROW_HEIGHT, DOT_RADIUS } from "../../utils/canvas"
import { bezierControlPoints } from "../../utils/geometry"

// Canvas can't read CSS custom properties, so these mirror the design-system
// palette in tokens/fig-tokens.css — keep them in sync if the tokens change.
// Ordered for contrast on the dark-navy canvas (brighter hues first, so lane 0
// — usually the main branch — stays legible).
const LANE_COLORS = [
    "#6db0f8", // secondary/blue
    "#17c3b2", // main/green
    "#f9c11a", // main/yellow
    "#dd1c1a", // main/red
    "#8df2e8", // secondary/green
    "#fde5a1", // secondary/yellow
    "#f4a09f", // secondary/red
    "#0852a1", // main/blue
]

const TEXT_COLOR = "#ffffff"          // greyscale/white — selected commit
const TEXT_BODY = "#fef4db"           // neutrals/cream — commit subjects
const TEXT_MUTED = "#959595"          // greyscale/grey-medium — ref pills
const PILL_BG = "#3f4464"             // neutrals/navy-light
const FONT = "13px 'Inter', system-ui, -apple-system, sans-serif"
const LINE_WIDTH = 2

export function laneColor(lane: number): string {
    return LANE_COLORS[((lane % LANE_COLORS.length) + LANE_COLORS.length) % LANE_COLORS.length]
}

export function clear(ctx: CanvasRenderingContext2D, width: number, height: number): void {
    ctx.clearRect(0, 0, width, height)
}

// Vertical line through a row for an active branch with no commit here.
export function drawPassthrough(ctx: CanvasRenderingContext2D, lane: number, row: number): void {
    const x = laneToX(lane)
    const top = row * ROW_HEIGHT
    ctx.strokeStyle = laneColor(lane)
    ctx.lineWidth = LINE_WIDTH
    ctx.beginPath()
    ctx.moveTo(x, top)
    ctx.lineTo(x, top + ROW_HEIGHT)
    ctx.stroke()
}

// One edge from this row's dot down to a target (toLane, toRow).
export function drawEdge(ctx: CanvasRenderingContext2D, row: Row, edge: Edge): void {
    const from = { x: laneToX(edge.fromLane), y: rowToY(row.row) }
    const to = { x: laneToX(edge.toLane), y: rowToY(edge.toRow) }

    // Colour by the lane the edge belongs to (merge-ins take their target lane).
    const lane = edge.type === EdgeType.MergeIn ? edge.toLane : edge.fromLane
    ctx.strokeStyle = laneColor(lane)
    ctx.lineWidth = LINE_WIDTH

    ctx.beginPath()
    ctx.moveTo(from.x, from.y)
    if (edge.type === EdgeType.Straight || edge.fromLane === edge.toLane) {
        ctx.lineTo(to.x, to.y) // cheaper than a bezier for vertical edges
    } else {
        const [c1, c2] = bezierControlPoints(from, to)
        ctx.bezierCurveTo(c1.x, c1.y, c2.x, c2.y, to.x, to.y)
    }
    ctx.stroke()
}

export function drawDot(ctx: CanvasRenderingContext2D, row: Row, selected: boolean): void {
    const x = laneToX(row.lane)
    const y = rowToY(row.row)

    if (selected) {
        ctx.strokeStyle = TEXT_COLOR
        ctx.lineWidth = LINE_WIDTH
        ctx.beginPath()
        ctx.arc(x, y, DOT_RADIUS + 3, 0, Math.PI * 2)
        ctx.stroke()
    }

    ctx.fillStyle = laneColor(row.lane)
    ctx.beginPath()
    ctx.arc(x, y, DOT_RADIUS, 0, Math.PI * 2)
    ctx.fill()
}

// A small rounded pill for a branch/tag label. Returns the x after the pill.
function drawPill(ctx: CanvasRenderingContext2D, text: string, x: number, y: number): number {
    const padX = 6
    const w = ctx.measureText(text).width + padX * 2
    const h = 16
    const top = y - h / 2

    ctx.fillStyle = PILL_BG
    ctx.strokeStyle = PILL_BG
    ctx.lineWidth = 1
    ctx.beginPath()
    // roundRect is widely supported in modern WebViews; fall back to a rect.
    if (typeof ctx.roundRect === "function") {
        ctx.roundRect(x, top, w, h, 4)
    } else {
        ctx.rect(x, top, w, h)
    }
    ctx.fill()
    ctx.stroke()

    ctx.fillStyle = TEXT_MUTED
    ctx.fillText(text, x + padX, y)
    return x + w
}

// Ref pills followed by short SHA + subject, starting at column x.
export function drawCommitText(
    ctx: CanvasRenderingContext2D,
    row: Row,
    x: number,
    selected: boolean,
): void {
    const y = rowToY(row.row)
    ctx.font = FONT
    ctx.textBaseline = "middle"

    let cursor = x
    for (const ref of row.refs) {
        if (ref === "HEAD") continue
        const name = ref
            .replace("refs/heads/", "")
            .replace("refs/remotes/", "")
            .replace("refs/tags/", "")
        cursor = drawPill(ctx, name, cursor, y) + 6
    }

    ctx.fillStyle = selected ? TEXT_COLOR : TEXT_BODY
    ctx.fillText(`${row.short}  ${row.subject}`, cursor, y)
}
