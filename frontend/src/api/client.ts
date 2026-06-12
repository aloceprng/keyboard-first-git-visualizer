import type {
    Row,
    CommitDetail,
    RefsResponse,
    SearchResult,
    ActionRequest,
    ActionEvent,
} from "./types"

export const BASE_URL = "http://127.0.0.1:7832"

function buildURL(path: string, params: Record<string, string | undefined> = {}): string {
    const url = new URL(`${BASE_URL}${path}`)
    for (const [key, value] of Object.entries(params)) {
        if (value !== undefined && value !== "") {
            url.searchParams.set(key, value)
        }
    }
    return url.toString()
}

async function unwrapError(response: Response): Promise<never> {
    const body = await response.json().catch(() => ({ error: response.statusText }))
    throw new Error(body.error ?? `HTTP ${response.status}`)
}

async function getJSON<T>(path: string, params: Record<string, string | undefined> = {}): Promise<T> {
    const response = await fetch(buildURL(path, params))
    if (!response.ok) return unwrapError(response)
    return response.json()
}

// for mvp, no streaming when parsing
function parseNDJSON<T>(text: string): T[] {
    return text
        .split("\n")
        .map((line) => line.trim())
        .filter((line) => line.length > 0)
        .map((line) => JSON.parse(line) as T)
}

// GET /graph?limit=500 (all rows at once)
export async function fetchGraph(limit = 500, before?: string, filter?: string): Promise<Row[]> {
    const response = await fetch(buildURL("/graph", { limit: String(limit), before, filter }))
    if (!response.ok) return unwrapError(response)
    return parseNDJSON<Row>(await response.text())
}

// GET /refs
export function fetchRefs(): Promise<RefsResponse> {
    return getJSON<RefsResponse>("/refs")
}

// GET /commit/:sha
export function fetchCommit(sha: string): Promise<CommitDetail> {
    return getJSON<CommitDetail>(`/commit/${sha}`)
}

// GET /search?q=... — backend hits are {SHA, Score, Highlight}; normalise to SHA.
interface RawSearchHit {
    SHA: string
}

export async function fetchSearch(
    q: string,
    type: "commit" | "author" | "branch" | "file" = "commit",
    limit = 20,
): Promise<SearchResult[]> {
    const hits = await getJSON<RawSearchHit[]>("/search", { q, type, limit: String(limit) })
    return hits.map((h) => ({ sha: h.SHA }))
}

// POST /action — execute a git operation
export async function postAction(req: ActionRequest): Promise<ActionEvent[]> {
    const response = await fetch(`${BASE_URL}/action`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(req),
    })
    if (!response.ok) return unwrapError(response)
    return parseNDJSON<ActionEvent>(await response.text())
}
