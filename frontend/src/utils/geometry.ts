import type { Row } from "../api/types"
import { laneToX, rowToY, yToRow, DOT_RADIUS } from "./canvas"

export interface Point {
    x: number
    y: number
}

// s-curve between branch nodes
export function bezierControlPoints(from: Point, to: Point): [Point, Point] {
    const dy = to.y - from.y
    return [
        { x: from.x, y: from.y + dy / 3 },
        { x: to.x, y: to.y - dy / 3 },
    ]
}

// map mouse y-coordinate to row index
export function hitTestRow(mouseY: number): number {
    return yToRow(mouseY)
}

// true if the mouse is within (a little slop around) the given row's dot
// used when a click should register only near the dot, not anywhere in the row
export function hitTestDot(mouseX: number, mouseY: number, row: Row): boolean {
    const cx = laneToX(row.lane)
    const cy = rowToY(row.row)
    const dx = mouseX - cx
    const dy = mouseY - cy
    const reach = DOT_RADIUS + 3
    return dx * dx + dy * dy <= reach * reach
}
