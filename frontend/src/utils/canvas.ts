export const LANE_WIDTH = 22
export const ROW_HEIGHT = 28
export const LEFT_MARGIN = 12
export const DOT_RADIUS = 5

export function laneToX(lane: number): number { return lane * LANE_WIDTH + LEFT_MARGIN }
export function rowToY(row: number): number { return row * ROW_HEIGHT + ROW_HEIGHT / 2 }
export function canvasHeight(rowCount: number): number { return rowCount * ROW_HEIGHT }
export function yToRow(y: number): number { return Math.floor(y / ROW_HEIGHT) }
export function xToLane(x: number): number { return Math.round((x - LEFT_MARGIN) / LANE_WIDTH) }
