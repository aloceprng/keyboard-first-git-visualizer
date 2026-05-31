import type { Row, CommitDetail, RefsResponse, SearchResult,  ActionRequest, ActionEvent, } from "./types"

async function* streamNDJSON<T>(response: Response): AsyncGenerator<T> {
    const reader = response.body!.getReader()
    const decoder = new TextDecoder()
    let buffer = ""

    while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })

        const lines = buffer.split("\n")
        buffer = lines.pop()!

        for (const line of lines) {
            const trimmed = line.trim()
            if (trimmed) yield JSON.parse(trimmed) as T
        }
    }

    const remaining = buffer.trim()
    if (remaining) yield JSON.parse(remaining) as T
}

export const BASE_URL = "http://127.0.0.1:7832"

function buildURL(path: string, params: Record<string, string | undefined>): string {
    const url = new URL(`${BASE_URL}${path}`)
    for (const [key, value] of Object.entries(params)) {
        if (value !== undefined && value !== "") {
            url.searchParams.set(key, value)
        }
    }
    return url.toString()
}

async function get<T>(path: string, params: Record<string, string | undefined> = {}): Promise<T> {
    const response = await fetch(buildURL(path, params))
    if (!response.ok) {
        const body = await response.json().catch(() => ({ error: response.statusText }))
        throw new Error(body.error ?? `HTTP ${response.status}`)
    }
    return response.json()
}

export async function fetchGraph(
    limit    = 500,
    cursor?: string,
    filter?: string,
    onBatch?: (rows: Row[]) => void,
): Promise<{ hasMore: boolean }> {
    const response = await fetch(
        buildURL("/graph", {
            limit:  String(limit),
            before: cursor,
            filter,
        })
    )

    if (!response.ok) {
        const body = await response.json().catch(() => ({ error: response.statusText }))
        throw new Error(body.error ?? `HTTP ${response.status}`)
    }

    const hasMore = response.headers.get("X-Has-More") === "true"

    let batch: Row[] = []
    for await (const row of streamNDJSON<Row>(response)) {
        batch.push(row)
        if (batch.length >= 50) {
            onBatch?.(batch)
            batch = []
        }
    }
    if (batch.length > 0) onBatch?.(batch)

    return { hasMore }
}

export function fetchCommit(sha: string): Promise<CommitDetail> {
    return get<CommitDetail>(`/commit/${sha}`)
}

export function fetchRefs(): Promise<RefsResponse> {
    return get<RefsResponse>("/refs")
}

export function fetchSearch(
    q:     string,
    type:  "commit" | "author" | "branch" | "file" = "commit",
    limit  = 20,
): Promise<SearchResult[]> {
    return get<SearchResult[]>("/search", {
        q,
        type,
        limit: String(limit),
    })
}

export async function* postAction(req: ActionRequest): AsyncGenerator<ActionEvent> {
    const response = await fetch(`${BASE_URL}/action`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(req),
    })

    if (response.status === 409 || !response.ok) {
        const body = await response.json().catch(() => ({ error: response.statusText }))
        throw new Error(body.error ?? `HTTP ${response.status}`)
    }

    yield* streamNDJSON<ActionEvent>(response)
}