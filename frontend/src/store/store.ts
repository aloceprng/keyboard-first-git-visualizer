import { create } from "zustand"
import type { Row, RefsResponse, SearchResult } from "../api/types"

interface AppState {
    rows: Row[]
    refs: RefsResponse | null
    selectedSHA: string | null
    isLoading: boolean

    isPaletteOpen: boolean
    searchResults: SearchResult[]

    setRows: (rows: Row[]) => void
    setRefs: (refs: RefsResponse) => void
    selectCommit: (sha: string | null) => void
    setLoading: (loading: boolean) => void

    openPalette: () => void
    closePalette: () => void
    setSearchResults: (results: SearchResult[]) => void
}

export const useStore = create<AppState>((set) => ({
    rows: [],
    refs: null,
    selectedSHA: null,
    isLoading: false,
    isPaletteOpen: false,
    searchResults: [],

    setRows: (rows) => set({ rows }),
    setRefs: (refs) => set({ refs }),
    selectCommit: (selectedSHA) => set({ selectedSHA }),
    setLoading: (isLoading) => set({ isLoading }),

    openPalette: () => set({ isPaletteOpen: true }),
    closePalette: () => set({ isPaletteOpen: false, searchResults: [] }),
    setSearchResults: (searchResults) => set({ searchResults }),
}))
