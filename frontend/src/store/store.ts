import { create } from "zustand"
import type { Row, RefsResponse, SearchResult } from "../api/types"

// A pending destructive action awaiting user confirmation.
export interface ConfirmRequest {
    message: string
    run: () => void | Promise<void>
}

interface AppState {
    rows: Row[]
    refs: RefsResponse | null
    selectedSHA: string | null
    isLoading: boolean

    isPaletteOpen: boolean
    searchResults: SearchResult[]

    isRepoPickerOpen: boolean
    confirm: ConfirmRequest | null

    setRows: (rows: Row[]) => void
    setRefs: (refs: RefsResponse) => void
    selectCommit: (sha: string | null) => void
    setLoading: (loading: boolean) => void

    openPalette: () => void
    closePalette: () => void
    setSearchResults: (results: SearchResult[]) => void

    openRepoPicker: () => void
    closeRepoPicker: () => void
    requestConfirm: (req: ConfirmRequest) => void
    clearConfirm: () => void
}

export const useStore = create<AppState>((set) => ({
    rows: [],
    refs: null,
    selectedSHA: null,
    isLoading: false,
    isPaletteOpen: false,
    searchResults: [],
    isRepoPickerOpen: false,
    confirm: null,

    setRows: (rows) => set({ rows }),
    setRefs: (refs) => set({ refs }),
    selectCommit: (selectedSHA) => set({ selectedSHA }),
    setLoading: (isLoading) => set({ isLoading }),

    openPalette: () => set({ isPaletteOpen: true }),
    closePalette: () => set({ isPaletteOpen: false, searchResults: [] }),
    setSearchResults: (searchResults) => set({ searchResults }),

    openRepoPicker: () => set({ isRepoPickerOpen: true }),
    closeRepoPicker: () => set({ isRepoPickerOpen: false }),
    requestConfirm: (confirm) => set({ confirm }),
    clearConfirm: () => set({ confirm: null }),
}))
