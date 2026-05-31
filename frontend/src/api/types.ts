export type Identity = { name: string; email: string }

export type Edge = {
    fromLane: number
    toLane: number
    toRow: number
    type: number
}

export type Row = {
    sha: string
    short: string
    subject: string
    author: Identity
    timestamp: string
    parents: string[]
    lane: number
    row: number
    edges: Edge[]
    passthrough: number
    refs: string[]
    activeLanes: number
}

export type DiffStats = {
    insertions: number
    deletions: number
    files: number
}

export type CommitDetail = {
    sha: string
    short: string
    subject: string
    body: string
    author: Identity
    committer: Identity
    timestamp: string
    parents: string[]
    refs: string[]
    stats: DiffStats
    changedFiles: string[]
}

export type RefResponse = {
    name: string
    sha: string
    type: string
    isCurrent: boolean
}

export type RefsResponse = {
    refs: RefResponse[]
    head: string
    headBranch: string
    inProgress: string
    inProgressMeta: Record<string, string>
}

export type SearchResult = {
    sha: string
    score: number
    highlight: number[]
}

export type ActionRequest = {
    action: string
    args?: Record<string, string>
    confirm?: boolean
}

export type ActionEvent = {
    type: "stdout" | "stderr" | "conflict" | "done" | "error"
    data: string
    files?: string[]
}