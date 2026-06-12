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
  insertions: number;
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
  parents: string[];
  refs: string[];
  stats: DiffStats;
  changedFiles: string[];
}

// GET /refs
export type RefType = "branch" | "remote" | "tag" | "stash";

export interface RefEntry {
  name: string;
  sha: string;
  type: RefType;
  isCurrent: boolean;
}

export type InProgressOperation =
  | ""
  | "merge"
  | "rebase"
  | "cherry-pick"
  | "revert"
  | "bisect";

// full response from GET /refs
export interface RefsResponse {
  refs: RefEntry[];
  head: string;        // HEAD commit SHA
  headBranch: string;  // branch HEAD points to, "" if detached
  inProgress: InProgressOperation;
  inProgressMeta?: Record<string, string>;
}

// GET /search
export type SearchResultType = "commit" | "branch" | "tag" | "author";

export interface SearchResult {
  sha: string;
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
  | "tag_delete"
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

