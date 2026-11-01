package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

const bodyTop = 2 // header: title line + rule

type hitRect struct {
	x, y, w, h int
}

func (r hitRect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

type hitRow struct {
	rect  hitRect
	index int
}

type inputHit struct {
	line  int
	value string
}

type uiHits struct {
	sourceTitle hitRect
	sourceArea  hitRect
	sourceRows  []hitRow
	addSource   hitRect
	presetBlock hitRect
	mainPreset  hitRect

	dest       hitRect
	optionArea hitRect
	optionRows []hitRow
	command    hitRect

	modalBox        hitRect
	presetItemLines []int
	inputCandidates []inputHit
	inputRecents    []inputHit
	confirmLine     int
	confirmNo       hitRect
	confirmYes      hitRect
}

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		return m.handleWheel(msg)
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if m.modal.kind != modalNone {
		return m.handleModalClick(msg)
	}
	if m.screen == screenRun {
		return m, nil
	}
	return m.handleSetupClick(msg)
}

func (m *Model) handleSetupClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	x, y := msg.X, msg.Y
	if m.hits.addSource.contains(x, y) {
		m.openSourceInput()
		return m, nil
	}
	if m.hits.sourceTitle.contains(x, y) {
		m.focus = focusSources
		return m, nil
	}
	for _, row := range m.hits.sourceRows {
		if row.rect.contains(x, y) {
			if m.focus == focusSources && m.sourceCursor == row.index {
				m.openSourceEdit()
			} else {
				m.focus = focusSources
				m.sourceCursor = row.index
			}
			return m, nil
		}
	}
	if m.hits.presetBlock.contains(x, y) || m.hits.mainPreset.contains(x, y) {
		m.openPresetPicker()
		return m, nil
	}
	if m.hits.dest.contains(x, y) {
		m.openDestInput()
		return m, nil
	}
	if m.hits.command.contains(x, y) {
		m.openCommand()
		return m, nil
	}
	for _, row := range m.hits.optionRows {
		if !row.rect.contains(x, y) {
			continue
		}
		m.focus = focusOptions
		m.optionCursor = row.index
		rows := optionRows()
		if row.index < 0 || row.index >= len(rows) {
			return m, nil
		}
		opt := rows[row.index]
		if opt.kind == rowHeader {
			return m, nil
		}
		if opt.kind == rowText {
			m.openOptionEditor(opt.id)
		} else {
			m.toggleCurrentOption(false)
		}
		return m, nil
	}
	if m.hits.optionArea.contains(x, y) {
		m.focus = focusOptions
	}
	return m, nil
}

func (m *Model) handleWheel(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	up := msg.Button == tea.MouseButtonWheelUp
	delta := 3
	if !up {
		delta = -3
	}
	step := 1
	if up {
		step = -1
	}
	if m.modal.kind != modalNone {
		switch m.modal.kind {
		case modalPresets:
			m.modal.cursor = clamp(m.modal.cursor+step, 0, max(0, len(m.modal.items)-1))
		case modalHelp, modalMessage, modalCommand:
			m.modal.scroll = max(0, m.modal.scroll-delta)
		}
		return m, nil
	}
	if m.screen == screenRun {
		m.scrollLog(delta)
		return m, nil
	}
	if m.hits.optionArea.contains(msg.X, msg.Y) {
		m.focus = focusOptions
		m.moveCursor(-delta)
		return m, nil
	}
	if m.hits.sourceArea.contains(msg.X, msg.Y) {
		m.focus = focusSources
		if len(m.sources) > 0 {
			m.sourceCursor = clamp(m.sourceCursor+step, 0, len(m.sources)-1)
		}
		return m, nil
	}
	if m.focus == focusOptions {
		m.moveCursor(-delta)
	} else if len(m.sources) > 0 {
		m.sourceCursor = clamp(m.sourceCursor+step, 0, len(m.sources)-1)
	}
	return m, nil
}

func (m *Model) handleModalClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	x, y := msg.X, msg.Y
	if !m.hits.modalBox.contains(x, y) {
		switch m.modal.kind {
		case modalHelp, modalMessage, modalCommand, modalPresets, modalConfirm:
			if m.pendingPlan != nil {
				m.pendingPlan.Cleanup()
				m.pendingPlan = nil
			}
			m.modal = modalState{}
		}
		return m, nil
	}
	switch m.modal.kind {
	case modalPresets:
		for i, row := range m.hits.presetItemLines {
			rect := hitRect{x: m.hits.modalBox.x + 3, y: m.hits.modalBox.y + 2 + row, w: max(1, m.hits.modalBox.w-6), h: 2}
			if rect.contains(x, y) {
				if i >= 0 && i < len(m.modal.items) {
					m.applyPreset(m.modal.items[i])
				}
				return m, nil
			}
		}
	case modalConfirm:
		if m.hits.confirmNo.contains(x, y) {
			if m.pendingPlan != nil {
				m.pendingPlan.Cleanup()
				m.pendingPlan = nil
			}
			m.modal = modalState{}
			return m, nil
		}
		if m.hits.confirmYes.contains(x, y) {
			return m, m.executeConfirm()
		}
	case modalInput:
		for _, c := range m.hits.inputCandidates {
			rect := hitRect{x: m.hits.modalBox.x + 5, y: m.hits.modalBox.y + 2 + c.line, w: max(1, m.hits.modalBox.w-10), h: 1}
			if rect.contains(x, y) {
				m.input.SetValue(c.value)
				m.input.CursorEnd()
				return m, nil
			}
		}
		for _, c := range m.hits.inputRecents {
			rect := hitRect{x: m.hits.modalBox.x + 5, y: m.hits.modalBox.y + 2 + c.line, w: max(1, m.hits.modalBox.w-10), h: 1}
			if rect.contains(x, y) {
				m.input.SetValue(c.value)
				m.input.CursorEnd()
				return m, nil
			}
		}
	}
	return m, nil
}

func (m *Model) executeConfirm() tea.Cmd {
	action := m.modal.action
	m.modal = modalState{}
	switch action {
	case "start-plan":
		if m.pendingPlan != nil {
			return m.startPlan(m.pendingPlan)
		}
	case "cancel-run":
		m.cancelRun()
	case "clear-sources":
		m.sources = nil
		m.sourceCursor = 0
		m.setToast("源列表已清空", false)
	case "reset-options":
		m.resetOptions()
	}
	return nil
}
