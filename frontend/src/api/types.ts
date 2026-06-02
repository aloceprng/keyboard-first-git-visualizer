// GET /graph NDJSON
export const EdgeType = {
  Straight: 0,
  MergeIn: 1,
  BranchOut: 2,
} as const;
export type EdgeType = (typeof EdgeType)[keyof typeof EdgeType];

export interface Edge {
  fromLane: number;
  toLane: number;
  toRow: number;
  type: EdgeType;
}

export interface GitPerson {
  name: string;
  email: string;
}

// one row in the graph NDJSON stream
export interface Row {
  sha: string;
  short: string;
  subject: string;
  author: GitPerson;
  timestamp: string;
  parents: string[];
  lane: number;
  row: number;
  edges: Edge[];
  passthrough: number;
  refs: string[];
  activeLanes: number;
}

// GET /commit/:sha

// +N −N in F files summary from commit.Stats()
export interface DiffStats {
  additions: number;
  deletions: number;
  files: number;
}

// full commit metadata returned by GET /commit/:sha
export interface CommitDetail {
  sha: string;
  short: string;
  subject: string;
  body: string;
  author: GitPerson;
  committer: GitPerson;
  timestamp: string;
  committedAt: string;
  parents: string[];
  refs: string[];
  diffStats?: DiffStats;
}

// GET /refs)
export interface RefEntry {
  ref: string;
  sha: string;
}

export type InProgressOperation =
  | ""
  | "merge"
  | "rebase"
  | "cherry-pick"
  | "revert"
  | "bisect";

// structured metadata for the current in-progress operation
export interface InProgressMeta {
  // for rebase: current step number (1-based)
  step?: number;
  // for rebase: total number of steps
  total?: number;
  // for cherry-pick / merge: SHA being applied
  sha?: string;
}

// full response from GET /refs
export interface RefsResponse {
  refs: Record<string, string>;
  head: string;
  headSha: string;
  inProgress: InProgressOperation;
  inProgressMeta?: InProgressMeta;
}

// GET /search)
export type SearchResultType = "commit" | "branch" | "tag" | "author";

export interface SearchResult {
  type: SearchResultType;
  sha: string;
  short: string;
  subject: string;
  author: GitPerson;
  timestamp: string;
  refs: string[];
  score: number;
}

export interface SearchParams {
  q: string;
  type?: SearchResultType;
  limit?: number;
}

// POST /action

export type ActionName =
  | "checkout"
  | "branch_create"
  | "branch_delete"
  | "branch_rename"
  | "merge"
  | "rebase"
  | "rebase_abort"
  | "rebase_continue"
  | "revert"
  | "cherry_pick"
  | "cherry_pick_abort"
  | "reset_soft"
  | "reset_mixed"
  | "reset_hard"
  | "stash"
  | "stash_pop"
  | "stash_drop"
  | "tag"
  | "fetch";

// body for POST /action
export interface ActionRequest {
  action: ActionName;
  args: Record<string, string>;
  confirm?: boolean;
}

export type ActionEventType = "stdout" | "stderr" | "conflict" | "done" | "error";

export interface ActionEvent {
  type: ActionEventType;
  data: string;
  files?: string[];
}

// /watch
export type WatchEventType = "graph_updated" | "refs_changed";

export interface MovedRef {
  ref: string;
  from: string;
  to: string;
}

/**
 * Event pushed by the server when the repo changes.
 *
 * graph_updated: new commits pushed or removed.
 *   - addedShas: prepend to graph (fetch each via /commit/:sha)
 *   - removedShas: remove from graph (force-push rewrite)
 *   - movedRefs: patch ref labels without a full reload
 *
 * refs_changed: ref labels moved without new commits.
 *   - Re-fetch /refs and call graphStore.updateMovedRefs.
 */
export interface WatchEvent {
  type: WatchEventType;
  addedShas: string[];
  removedShas: string[];
  movedRefs: MovedRef[];
  inProgress: InProgressOperation;
}

export interface GraphParams {
  limit?: number;
  before?: string;
  filter?: string;
}