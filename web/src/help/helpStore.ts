// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * Global UI state backing the help Drawer. Any component opens help for a
 * document id; the single HelpDrawer mounted at the application root reacts.
 * termID optionally swaps the content to a glossary term while the Drawer
 * stays open.
 */

import { create } from 'zustand'

interface HelpState {
  // docID is the page document that opened the Drawer.
  docID: string | null
  // termID, when set, replaces the shown content with a glossary term.
  termID: string | null

  // open shows the Drawer with the given page document and clears any
  // in-Drawer glossary navigation.
  open: (docID: string) => void
  // showTerm swaps the Drawer content to a glossary term.
  showTerm: (termID: string) => void
  // close hides the Drawer.
  close: () => void
}

// useHelpStore is the Zustand store for help UI state.
export const useHelpStore = create<HelpState>((set) => ({
  docID: null,
  termID: null,
  open: (docID) => set({ docID, termID: null }),
  showTerm: (termID) => set({ termID }),
  close: () => set({ docID: null, termID: null }),
}))
