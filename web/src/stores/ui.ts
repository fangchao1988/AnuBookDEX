import { create } from 'zustand'

export type ModalId = 'ai' | 'settings' | 'announce' | 'leverage'

interface UiState {
  modal: ModalId | null
  openModal: (m: ModalId) => void
  closeModal: () => void
}

export const useUi = create<UiState>()((set) => ({
  modal: null,
  openModal: (modal) => set({ modal }),
  closeModal: () => set({ modal: null }),
}))
